// Package config loads svodka configuration from two sources:
//
//   - secrets and runtime flags come from the environment via ardanlabs/conf
//     (env names are prefixed with SVODKA_, e.g. SVODKA_TELEGRAM_API_ID);
//   - user content settings come from config.yml via gopkg.in/yaml.v3.
//
// The split is deliberate: ardanlabs/conf's yaml parser silently drops zero
// values (false/0/""), which would make settings like target_chat impossible to
// override to an empty/zero value. Reading the file with yaml.v3 avoids that,
// while conf still gives us env handling, masking and --help for secrets.
package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/ardanlabs/conf/v3"
	yaml "gopkg.in/yaml.v3"
)

// ErrHelp is returned by Load/LoadSecrets when --help or --version was requested.
// Callers should print the help text and exit 0.
var ErrHelp = errors.New("help requested")

// Route is one fan-out target: a set of source chats summarized into a single
// digest posted to one target chat. Other settings (window, language, model,
// backlinks, ...) stay global across all routes (ADR-19).
type Route struct {
	SourceChats []string `yaml:"source_chats"`
	TargetChat  string   `yaml:"target_chat"`
}

// Settings holds non-secret, user-editable configuration from config.yml.
type Settings struct {
	SourceChats []string `yaml:"source_chats"`
	TargetChat  string   `yaml:"target_chat"`
	// Routes, when non-empty, runs one digest per route (different source chats
	// to different target chats in a single run). When empty, the top-level
	// source_chats/target_chat form a single implicit route (ADR-19).
	Routes      []Route `yaml:"routes"`
	WindowHours int     `yaml:"window_hours"`
	OutputLang  string  `yaml:"output_lang"`
	Timezone    string  `yaml:"timezone"`
	// LLMProvider selects the Provider implementation: "anthropic" (default) or
	// "openrouter" (ADR-20). Model and MaxOutputTokens are shared across
	// providers; only their meaning (model id format, key used) changes.
	LLMProvider     string `yaml:"llm_provider"`
	Model           string `yaml:"model"`
	MaxOutputTokens int    `yaml:"max_output_tokens"`
	// ExtraInstructions is optional free-text guidance appended to the digest
	// prompt (tone, structure, what to emphasize). Empty = default behavior.
	ExtraInstructions string `yaml:"extra_instructions"`
	// Backlinks toggles t.me/c source links on digest items (default true). A
	// pointer distinguishes an absent key (-> default true) from an explicit
	// `backlinks: false` (ADR-3/ADR-15).
	Backlinks *bool `yaml:"backlinks"`
}

// Secrets holds credentials and runtime flags sourced from the environment.
type Secrets struct {
	conf.Version
	ConfigPath string `conf:"env:CONFIG,flag:config,default:config.yml,help:path to config.yml"`
	Telegram   struct {
		APIID   int    `conf:"env:TELEGRAM_API_ID,required,help:api_id from my.telegram.org"`
		APIHash string `conf:"env:TELEGRAM_API_HASH,required,mask,help:api_hash from my.telegram.org"`
		Session string `conf:"env:TELEGRAM_SESSION,mask,help:base64 session from the login command"`
	}
	Anthropic struct {
		Key string `conf:"env:ANTHROPIC_KEY,mask,help:Anthropic API key"`
	}
	OpenRouter struct {
		Key string `conf:"env:OPENROUTER_KEY,mask,help:OpenRouter API key"`
	}
}

// Overrides are optional CLI flags that take precedence over config.yml for a
// single run (flag > yaml > default). Pointer fields stay nil when the flag is
// absent, so an unset flag changes nothing while an explicit zero/empty value
// is still applied (see ADR-3/ADR-14). Parsed by conf alongside Secrets on the
// svodka command only; the login command parses Secrets without them.
type Overrides struct {
	SourceChats       *[]string `conf:"flag:source-chats,help:override source_chats (comma-separated)"`
	TargetChat        *string   `conf:"flag:target-chat,help:override target_chat"`
	WindowHours       *int      `conf:"flag:window-hours,help:override window_hours"`
	OutputLang        *string   `conf:"flag:output-lang,help:override output_lang"`
	Timezone          *string   `conf:"flag:timezone,help:override timezone"`
	LLMProvider       *string   `conf:"flag:llm-provider,help:override llm_provider (anthropic|openrouter)"`
	Model             *string   `conf:"flag:model,help:override model"`
	MaxOutputTokens   *int      `conf:"flag:max-output-tokens,help:override max_output_tokens"`
	ExtraInstructions *string   `conf:"flag:extra-instructions,help:override extra_instructions"`
	Backlinks         *bool     `conf:"flag:backlinks,help:override backlinks"`
}

// Config is the full configuration used by the svodka job.
type Config struct {
	Secrets
	Settings
}

// Help is the prefix passed to conf so generated help text is recognizable.
const prefix = "SVODKA"

// parseConf runs conf over target, translating the help sentinel into ErrHelp
// (printing the generated usage) so callers can exit 0.
func parseConf(target any) error {
	help, err := conf.Parse(prefix, target)
	if err != nil {
		if errors.Is(err, conf.ErrHelpWanted) {
			fmt.Println(help)
			return ErrHelp
		}
		return err
	}
	return nil
}

// LoadSecrets parses only the env/flag secrets. Used by the login command, which
// needs api_id/api_hash but not the content settings or overrides.
func LoadSecrets() (Secrets, error) {
	var s Secrets
	if err := parseConf(&s); err != nil {
		if errors.Is(err, ErrHelp) {
			return Secrets{}, ErrHelp
		}
		return Secrets{}, fmt.Errorf("parsing secrets: %w", err)
	}
	return s, nil
}

// Load parses secrets and CLI overrides from env/flags, content settings from
// config.yml, applies defaults to unset settings, then overrides on top, and
// validates the result for the svodka job.
func Load() (*Config, error) {
	cfg, err := load()
	if err != nil {
		return nil, err
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// LoadForCapture parses and assembles the config exactly like Load, but skips
// validateLLM: eval's capture subcommand only resolves and fetches chats (it
// never calls an LLM), so requiring an Anthropic/OpenRouter key here would
// force users to hold a provider key just to snapshot messages (ADR-21).
func LoadForCapture() (*Config, error) {
	cfg, err := load()
	if err != nil {
		return nil, err
	}
	if err := cfg.validateTelegram(); err != nil {
		return nil, err
	}
	if err := cfg.validateRoutes(); err != nil {
		return nil, err
	}
	if err := cfg.validateWindow(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// load parses secrets and CLI overrides from env/flags, content settings from
// config.yml, applies defaults to unset settings, then overrides on top. It
// does not validate the result: callers pick which validation applies to
// their command (Load validates everything; LoadForCapture skips the LLM
// check).
func load() (*Config, error) {
	// Secrets and Overrides share one conf parse so a single pass over os.Args
	// handles both and --help lists every flag (no second flag parser to clash).
	var args struct {
		Secrets
		Overrides
	}
	if err := parseConf(&args); err != nil {
		// conf already prefixes its errors with "parsing config:"; don't double it.
		return nil, err
	}

	settings, err := loadSettings(args.ConfigPath)
	if err != nil {
		return nil, err
	}
	applyDefaults(&settings)
	applyOverrides(&settings, args.Overrides)

	return &Config{Secrets: args.Secrets, Settings: settings}, nil
}

func loadSettings(path string) (Settings, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Settings{}, fmt.Errorf("config file %q not found; copy config.example.yml to config.yml and edit it", path)
		}
		return Settings{}, fmt.Errorf("reading %q: %w", path, err)
	}

	var s Settings
	if err := yaml.Unmarshal(data, &s); err != nil {
		return Settings{}, fmt.Errorf("parsing %q: %w", path, err)
	}
	return s, nil
}

func applyDefaults(s *Settings) {
	if s.TargetChat == "" {
		s.TargetChat = "me"
	}
	if s.WindowHours == 0 {
		s.WindowHours = 24
	}
	if s.OutputLang == "" {
		s.OutputLang = "ru"
	}
	if s.Timezone == "" {
		s.Timezone = "UTC"
	}
	if s.LLMProvider == "" {
		s.LLMProvider = "anthropic"
	}
	// The built-in default model is an Anthropic id; openrouter has no sane
	// default (models are "vendor/model") so validate() requires it explicit.
	if s.Model == "" && s.LLMProvider == "anthropic" {
		s.Model = "claude-sonnet-4-6"
	}
	if s.MaxOutputTokens == 0 {
		s.MaxOutputTokens = 2000
	}
	if s.Backlinks == nil {
		t := true
		s.Backlinks = &t
	}
}

// applyOverrides applies CLI flag overrides on top of yaml+defaults. Only
// non-nil fields are touched, so an unset flag leaves the setting as-is while an
// explicit zero/empty value (e.g. --max-output-tokens 0) is honored (ADR-3).
func applyOverrides(s *Settings, ov Overrides) {
	if ov.SourceChats != nil {
		s.SourceChats = splitChats(*ov.SourceChats)
	}
	if ov.TargetChat != nil {
		s.TargetChat = *ov.TargetChat
	}
	if ov.WindowHours != nil {
		s.WindowHours = *ov.WindowHours
	}
	if ov.OutputLang != nil {
		s.OutputLang = *ov.OutputLang
	}
	if ov.Timezone != nil {
		s.Timezone = *ov.Timezone
	}
	if ov.LLMProvider != nil {
		s.LLMProvider = *ov.LLMProvider
	}
	if ov.Model != nil {
		s.Model = *ov.Model
	}
	if ov.MaxOutputTokens != nil {
		s.MaxOutputTokens = *ov.MaxOutputTokens
	}
	if ov.ExtraInstructions != nil {
		s.ExtraInstructions = *ov.ExtraInstructions
	}
	if ov.Backlinks != nil {
		s.Backlinks = ov.Backlinks
	}
}

// splitChats flattens conf's flag slice into individual chat refs. conf does not
// split a flag value on commas (it yields a single element), so we split here to
// support --source-chats "@a,@b". Blank entries are dropped.
func splitChats(raw []string) []string {
	var out []string
	for _, part := range raw {
		for c := range strings.SplitSeq(part, ",") {
			if c = strings.TrimSpace(c); c != "" {
				out = append(out, c)
			}
		}
	}
	return out
}

// EffectiveRoutes returns the routes to run. If `routes` is set it wins (and the
// top-level source_chats/target_chat are ignored); otherwise a single implicit
// route is synthesized from the top-level fields. A route's empty TargetChat
// defaults to "me" — the same safe default as the single-route path (ADR-4/ADR-19).
func (s Settings) EffectiveRoutes() []Route {
	if len(s.Routes) == 0 {
		return []Route{{SourceChats: s.SourceChats, TargetChat: s.TargetChat}}
	}
	out := make([]Route, len(s.Routes))
	for i, r := range s.Routes {
		if r.TargetChat == "" {
			r.TargetChat = "me"
		}
		out[i] = r
	}
	return out
}

// validate runs every check required for the svodka job, in the same order
// the checks used to run before being split out (so error precedence, and
// therefore which message a given invalid config surfaces, is unchanged).
func (c *Config) validate() error {
	if err := c.validateTelegram(); err != nil {
		return err
	}
	if err := c.validateLLM(); err != nil {
		return err
	}
	if err := c.validateRoutes(); err != nil {
		return err
	}
	if err := c.validateWindow(); err != nil {
		return err
	}
	return nil
}

// validateTelegram requires a Telegram session. Needed by every command that
// talks to Telegram, including eval capture.
func (c *Config) validateTelegram() error {
	if c.Telegram.Session == "" {
		return errors.New("no Telegram session: run `go run ./api/cmd/login` (e.g. in a Codespace) to create SVODKA_TELEGRAM_SESSION")
	}
	return nil
}

// validateLLM requires a key for the selected provider (and, for openrouter,
// an explicit model). Skipped by LoadForCapture: capture never calls an LLM.
func (c *Config) validateLLM() error {
	switch c.LLMProvider {
	case "anthropic":
		if c.Anthropic.Key == "" {
			return errors.New("SVODKA_ANTHROPIC_KEY is not set")
		}
	case "openrouter":
		if c.OpenRouter.Key == "" {
			return errors.New("SVODKA_OPENROUTER_KEY is not set")
		}
		if c.Model == "" {
			return errors.New(`model is required in config.yml when llm_provider is "openrouter" (format "vendor/model", e.g. "openai/gpt-5")`)
		}
	default:
		return fmt.Errorf(`llm_provider must be "anthropic" or "openrouter", got %q`, c.LLMProvider)
	}
	return nil
}

// validateRoutes requires every effective route to have at least one source
// chat.
func (c *Config) validateRoutes() error {
	for i, r := range c.EffectiveRoutes() {
		if len(r.SourceChats) == 0 {
			if len(c.Routes) == 0 {
				return errors.New("source_chats is empty in config.yml")
			}
			return fmt.Errorf("routes[%d] has empty source_chats in config.yml", i)
		}
	}
	return nil
}

// validateWindow requires a non-negative window.
func (c *Config) validateWindow() error {
	if c.WindowHours < 0 {
		return fmt.Errorf("window_hours must be >= 0, got %d", c.WindowHours)
	}
	return nil
}
