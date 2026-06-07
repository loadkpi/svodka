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
