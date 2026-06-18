package config

import (
	"reflect"
	"testing"
)

func ptr[T any](v T) *T { return &v }

// baseline mirrors a config.yml after applyDefaults: a known, non-zero state so
// tests can assert that only the overridden fields change.
func baseline() Settings {
	return Settings{
		SourceChats:       []string{"@yaml"},
		TargetChat:        "@group",
		WindowHours:       24,
		OutputLang:        "ru",
		Timezone:          "Europe/Belgrade",
		Model:             "claude-sonnet-4-6",
		MaxOutputTokens:   2000,
		ExtraInstructions: "from yaml",
		Backlinks:         ptr(true),
	}
}

func TestApplyOverrides_NoneLeavesSettingsUntouched(t *testing.T) {
	got := baseline()
	applyOverrides(&got, Overrides{}) // all nil
	if want := baseline(); !reflect.DeepEqual(got, want) {
		t.Errorf("nil overrides changed settings\n got: %+v\nwant: %+v", got, want)
	}
}

func TestApplyOverrides_EachFieldApplied(t *testing.T) {
	got := baseline()
	applyOverrides(&got, Overrides{
		SourceChats:       &[]string{"@a", "@b"},
		TargetChat:        ptr("me"),
		WindowHours:       ptr(72),
		OutputLang:        ptr("en"),
		Timezone:          ptr("UTC"),
		Model:             ptr("claude-haiku-4-5"),
		MaxOutputTokens:   ptr(500),
		ExtraInstructions: ptr("be terse"),
		Backlinks:         ptr(false),
	})
	want := Settings{
		SourceChats:       []string{"@a", "@b"},
		TargetChat:        "me",
		WindowHours:       72,
		OutputLang:        "en",
		Timezone:          "UTC",
		Model:             "claude-haiku-4-5",
		MaxOutputTokens:   500,
		ExtraInstructions: "be terse",
		Backlinks:         ptr(false),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("overrides not applied\n got: %+v\nwant: %+v", got, want)
	}
}

// An explicit zero/empty value is a valid override (ADR-3): it must win over the
// yaml/default, distinct from "flag not passed".
func TestApplyOverrides_ExplicitZeroIsHonored(t *testing.T) {
	got := baseline()
	applyOverrides(&got, Overrides{
		WindowHours:       ptr(0),
		MaxOutputTokens:   ptr(0),
		ExtraInstructions: ptr(""),
	})
	if got.WindowHours != 0 {
		t.Errorf("WindowHours: got %d, want 0", got.WindowHours)
	}
	if got.MaxOutputTokens != 0 {
		t.Errorf("MaxOutputTokens: got %d, want 0", got.MaxOutputTokens)
	}
	if got.ExtraInstructions != "" {
		t.Errorf("ExtraInstructions: got %q, want empty", got.ExtraInstructions)
	}
	// Untouched fields keep their baseline values.
	if got.TargetChat != "@group" {
		t.Errorf("TargetChat changed unexpectedly: %q", got.TargetChat)
	}
}

// Backlinks defaults to true when the key is absent, but an explicit
// `backlinks: false` in config.yml must survive applyDefaults (ADR-3/ADR-15).
func TestApplyDefaults_Backlinks(t *testing.T) {
	t.Run("absent defaults to true", func(t *testing.T) {
		s := Settings{} // Backlinks nil
		applyDefaults(&s)
		if s.Backlinks == nil || !*s.Backlinks {
			t.Fatalf("Backlinks = %v, want true", s.Backlinks)
		}
	})
	t.Run("explicit false preserved", func(t *testing.T) {
		s := Settings{Backlinks: ptr(false)}
		applyDefaults(&s)
		if s.Backlinks == nil || *s.Backlinks {
			t.Fatalf("Backlinks = %v, want false", s.Backlinks)
		}
	})
}

func TestEffectiveRoutes(t *testing.T) {
	t.Run("no routes synthesizes single from top-level", func(t *testing.T) {
		s := Settings{SourceChats: []string{"@a", "@b"}, TargetChat: "@group"}
		got := s.EffectiveRoutes()
		want := []Route{{SourceChats: []string{"@a", "@b"}, TargetChat: "@group"}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("EffectiveRoutes() = %#v, want %#v", got, want)
		}
	})
	t.Run("routes win over top-level", func(t *testing.T) {
		s := Settings{
			SourceChats: []string{"@ignored"},
			TargetChat:  "@ignored",
			Routes: []Route{
				{SourceChats: []string{"@a"}, TargetChat: "@team1"},
				{SourceChats: []string{"@b"}, TargetChat: "@team2"},
			},
		}
		got := s.EffectiveRoutes()
		want := []Route{
			{SourceChats: []string{"@a"}, TargetChat: "@team1"},
			{SourceChats: []string{"@b"}, TargetChat: "@team2"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("EffectiveRoutes() = %#v, want %#v", got, want)
		}
	})
	t.Run("empty route target defaults to me", func(t *testing.T) {
		s := Settings{Routes: []Route{{SourceChats: []string{"@a"}}}}
		got := s.EffectiveRoutes()
		if got[0].TargetChat != "me" {
			t.Errorf("TargetChat = %q, want %q", got[0].TargetChat, "me")
		}
	})
}

func TestValidateRoutes(t *testing.T) {
	// A Config that is valid apart from the source-chat shape under test.
	newCfg := func(s Settings) *Config {
		c := &Config{Settings: s}
		c.Telegram.Session = "sess"
		c.Anthropic.Key = "key"
		return c
	}

	t.Run("single route from top-level ok", func(t *testing.T) {
		if err := newCfg(Settings{SourceChats: []string{"@a"}}).validate(); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
	t.Run("empty top-level source_chats errors", func(t *testing.T) {
		if err := newCfg(Settings{}).validate(); err == nil {
			t.Error("want error for empty source_chats, got nil")
		}
	})
	t.Run("routes with sources ok despite empty top-level", func(t *testing.T) {
		s := Settings{Routes: []Route{{SourceChats: []string{"@a"}, TargetChat: "@t"}}}
		if err := newCfg(s).validate(); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
	t.Run("route with empty source_chats errors", func(t *testing.T) {
		s := Settings{Routes: []Route{
			{SourceChats: []string{"@a"}, TargetChat: "@t1"},
			{TargetChat: "@t2"},
		}}
		if err := newCfg(s).validate(); err == nil {
			t.Error("want error for route with empty source_chats, got nil")
		}
	})
}

func TestSplitChats(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{"single csv arg", []string{"@a,@b,@c"}, []string{"@a", "@b", "@c"}},
		{"single chat", []string{"@solo"}, []string{"@solo"}},
		{"trims spaces", []string{" @a , @b "}, []string{"@a", "@b"}},
		{"drops blanks", []string{"@a,,@b,"}, []string{"@a", "@b"}},
		{"multiple args flattened", []string{"@a,@b", "@c"}, []string{"@a", "@b", "@c"}},
		{"all blank yields nil", []string{" , "}, nil},
		{"empty input", nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := splitChats(tt.in); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("splitChats(%q) = %#v, want %#v", tt.in, got, tt.want)
			}
		})
	}
}
