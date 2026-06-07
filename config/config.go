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

// Settings holds non-secret, user-editable configuration from config.yml.
type Settings struct {
	SourceChats     []string `yaml:"source_chats"`
	TargetChat      string   `yaml:"target_chat"`
	WindowHours     int      `yaml:"window_hours"`
	OutputLang      string   `yaml:"output_lang"`
	Timezone        string   `yaml:"timezone"`
	Model           string   `yaml:"model"`
	MaxOutputTokens int      `yaml:"max_output_tokens"`
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

	cfg := &Config{Secrets: args.Secrets, Settings: settings}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
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
	if s.Model == "" {
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

func (c *Config) validate() error {
	if c.Telegram.Session == "" {
		return errors.New("no Telegram session: run `go run ./api/cmd/login` (e.g. in a Codespace) to create SVODKA_TELEGRAM_SESSION")
	}
	if c.Anthropic.Key == "" {
		return errors.New("SVODKA_ANTHROPIC_KEY is not set")
	}
	if len(c.SourceChats) == 0 {
		return errors.New("source_chats is empty in config.yml")
	}
	if c.WindowHours < 0 {
		return fmt.Errorf("window_hours must be >= 0, got %d", c.WindowHours)
	}
	return nil
}
