package digest

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"svodka/business/llm"
	"svodka/business/telegram"
)

// fakeProvider records every call and returns a scripted reply, so Build can be
// tested without touching the network (ADR-7).
type fakeProvider struct {
	calls []llm.Input
	reply func(llm.Input) (string, error)
}

func (f *fakeProvider) Summarize(_ context.Context, in llm.Input) (string, error) {
	f.calls = append(f.calls, in)
	if f.reply != nil {
		return f.reply(in)
	}
	return "ok", nil
}

func msg(author, text string, t time.Time) telegram.Message {
	return telegram.Message{Author: author, Time: t, Text: text}
}

func TestSerializeChat(t *testing.T) {
	loc := time.UTC
	base := time.Date(2026, 6, 6, 14, 32, 0, 0, time.UTC)

	chat := telegram.ChatMessages{
		Title: "Team",
		Messages: []telegram.Message{
			msg("Alice", "hello", base),
			msg("", "channel post", base.Add(time.Minute)), // no author
		},
	}

	got := serializeChat(chat, loc)
	want := "## Team\n[14:32] Alice: hello\n[14:33] channel post\n"
	if got != want {
		t.Fatalf("serializeChat\n got: %q\nwant: %q", got, want)
	}
}

func TestSerializeChatRespectsLocation(t *testing.T) {
	// 14:32 UTC is 16:32 in Europe/Belgrade (UTC+2 in June).
	loc, err := time.LoadLocation("Europe/Belgrade")
	if err != nil {
		t.Skipf("tz data unavailable: %v", err)
	}
	base := time.Date(2026, 6, 6, 14, 32, 0, 0, time.UTC)
	chat := telegram.ChatMessages{Title: "T", Messages: []telegram.Message{msg("A", "x", base)}}

	got := serializeChat(chat, loc)
	if !strings.Contains(got, "[16:32]") {
		t.Fatalf("expected local time 16:32, got %q", got)
	}
}

func TestSerializeAll(t *testing.T) {
	loc := time.UTC
	base := time.Date(2026, 6, 6, 9, 0, 0, 0, time.UTC)
	chats := []telegram.ChatMessages{
		{Title: "A", Messages: []telegram.Message{msg("u", "one", base)}},
		{Title: "B", Messages: []telegram.Message{msg("v", "two", base)}},
	}

	got := serializeAll(chats, loc)
	want := "## A\n[09:00] u: one\n\n## B\n[09:00] v: two\n"
	if got != want {
		t.Fatalf("serializeAll\n got: %q\nwant: %q", got, want)
	}
}

func TestUseMapReduce(t *testing.T) {
	tests := []struct {
		name      string
		text      string
		threshold int
		want      bool
	}{
		{"below", "abc", 4, false},
		{"equal", "abcd", 4, true},
		{"above", "abcde", 4, true},
		{"multibyte counts runes", strings.Repeat("я", 3), 4, false}, // 3 runes, 6 bytes
		{"multibyte at threshold", strings.Repeat("я", 4), 4, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := useMapReduce(tt.text, tt.threshold); got != tt.want {
				t.Fatalf("useMapReduce(%q, %d) = %v, want %v", tt.text, tt.threshold, got, tt.want)
			}
		})
	}
}

func smallChats() []telegram.ChatMessages {
	base := time.Date(2026, 6, 6, 10, 0, 0, 0, time.UTC)
	return []telegram.ChatMessages{
		{Title: "A", Messages: []telegram.Message{msg("u", "hi", base)}},
		{Title: "B", Messages: []telegram.Message{msg("v", "yo", base)}},
	}
}

func TestBuildSinglePass(t *testing.T) {
	fp := &fakeProvider{reply: func(llm.Input) (string, error) { return "  digest  ", nil }}
	opts := Options{OutputLang: "ru", Location: time.UTC, Model: "m", MaxTokens: 100}

	out, err := Build(context.Background(), fp, smallChats(), opts)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if out != "digest" {
		t.Fatalf("output not trimmed: %q", out)
	}
	if len(fp.calls) != 1 {
		t.Fatalf("expected exactly 1 call, got %d", len(fp.calls))
	}
	if fp.calls[0].System != singleSystem("ru") {
		t.Fatalf("single-pass should use the single system prompt")
	}
	if fp.calls[0].Model != "m" || fp.calls[0].MaxTokens != 100 {
		t.Fatalf("model/max-tokens not propagated: %+v", fp.calls[0])
	}
}

func TestBuildMapReduce(t *testing.T) {
	// Force map-reduce regardless of payload size.
	fp := &fakeProvider{}
	opts := Options{OutputLang: "en", Location: time.UTC, Model: "m", MaxTokens: 100, ThresholdChars: 1}

	chats := smallChats()
	if _, err := Build(context.Background(), fp, chats, opts); err != nil {
		t.Fatalf("Build: %v", err)
	}

	// 2 map calls + 1 reduce.
	if len(fp.calls) != 3 {
		t.Fatalf("expected 3 calls (2 map + 1 reduce), got %d", len(fp.calls))
	}

	mapSys := mapSystem("en")
	// First two are map calls with a byte-identical system (prompt-cache invariant).
	for i := range 2 {
		if fp.calls[i].System != mapSys {
			t.Fatalf("map call %d system mismatch: caching needs identical system", i)
		}
	}
	if fp.calls[2].System != reduceSystem("en") {
		t.Fatalf("last call should be the reduce step")
	}
	// Reduce input must carry both chat titles assembled from map outputs.
	if !strings.Contains(fp.calls[2].User, "## A") || !strings.Contains(fp.calls[2].User, "## B") {
		t.Fatalf("reduce input missing chat titles: %q", fp.calls[2].User)
	}
}

func TestBuildMapReduceSkipsEmptyMapOutput(t *testing.T) {
	// A map step that returns blank for chat B should be dropped from reduce input.
	fp := &fakeProvider{reply: func(in llm.Input) (string, error) {
		if in.System == mapSystem("en") && strings.Contains(in.User, "## B") {
			return "   ", nil
		}
		return "bullets", nil
	}}
	opts := Options{OutputLang: "en", Location: time.UTC, ThresholdChars: 1}

	if _, err := Build(context.Background(), fp, smallChats(), opts); err != nil {
		t.Fatalf("Build: %v", err)
	}
	reduce := fp.calls[len(fp.calls)-1]
	if strings.Contains(reduce.User, "## B") {
		t.Fatalf("empty map output for B should not reach reduce: %q", reduce.User)
	}
}

func TestBuildEmptyInput(t *testing.T) {
	fp := &fakeProvider{}
	// Chats present but all empty -> nothing to summarize, no provider calls.
	chats := []telegram.ChatMessages{{Title: "A"}, {Title: "B"}}

	out, err := Build(context.Background(), fp, chats, Options{OutputLang: "ru"})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if out != "" {
		t.Fatalf("expected empty digest, got %q", out)
	}
	if len(fp.calls) != 0 {
		t.Fatalf("expected no provider calls, got %d", len(fp.calls))
	}
}

func TestBuildPropagatesError(t *testing.T) {
	wantErr := errors.New("boom")
	fp := &fakeProvider{reply: func(llm.Input) (string, error) { return "", wantErr }}

	_, err := Build(context.Background(), fp, smallChats(), Options{OutputLang: "ru", Location: time.UTC})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped provider error, got %v", err)
	}
}
