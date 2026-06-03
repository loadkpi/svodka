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

// Config is the full configuration used by the svodka job.
type Config struct {
	Secrets
	Settings
}

// Help is the prefix passed to conf so generated help text is recognizable.
const prefix = "SVODKA"

// LoadSecrets parses only the env/flag secrets. Used by the login command, which
// needs api_id/api_hash but not the content settings.
func LoadSecrets() (Secrets, error) {
	var s Secrets
	help, err := conf.Parse(prefix, &s)
	if err != nil {
		if errors.Is(err, conf.ErrHelpWanted) {
			fmt.Println(help)
			return Secrets{}, ErrHelp
		}
		return Secrets{}, fmt.Errorf("parsing secrets: %w", err)
	}
	return s, nil
}

// Load parses secrets from env and content settings from config.yml, applies
// defaults to unset settings and validates the result for the svodka job.
func Load() (*Config, error) {
	secrets, err := LoadSecrets()
	if err != nil {
		return nil, err
	}

	settings, err := loadSettings(secrets.ConfigPath)
	if err != nil {
		return nil, err
	}
	applyDefaults(&settings)

	cfg := &Config{Secrets: secrets, Settings: settings}
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
