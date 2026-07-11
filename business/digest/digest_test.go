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
	reply func(llm.Input) (llm.Result, error)
}

func (f *fakeProvider) Summarize(_ context.Context, in llm.Input) (llm.Result, error) {
	f.calls = append(f.calls, in)
	if f.reply != nil {
		return f.reply(in)
	}
	return llm.Result{Text: "ok"}, nil
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

	got := serializeChat(chat, loc, false)
	want := "## Team\n[14:32] Alice: hello\n[14:33] channel post\n"
	if got != want {
		t.Fatalf("serializeChat\n got: %q\nwant: %q", got, want)
	}
}

func TestLinkFor(t *testing.T) {
	tests := []struct {
		name      string
		channelID int64
		username  string
		msgID     int
		want      string
	}{
		{"channel and msg, no username -> private link", 123, "", 456, "https://t.me/c/123/456"},
		{"channel with username -> public link", 123, "netology", 456, "https://t.me/netology/456"},
		{"no channel (user/basic group), no username", 0, "", 456, ""},
		{"user with username but no channel id -> still no link", 0, "durov", 456, ""},
		{"no msg id", 123, "", 0, ""},
		{"negative channel", -1, "", 456, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := linkFor(tt.channelID, tt.username, tt.msgID); got != tt.want {
				t.Errorf("linkFor(%d, %q, %d) = %q, want %q", tt.channelID, tt.username, tt.msgID, got, tt.want)
			}
		})
	}
}

func TestSerializeChatBacklinks(t *testing.T) {
	loc := time.UTC
	base := time.Date(2026, 6, 6, 14, 32, 0, 0, time.UTC)
	m := telegram.Message{Author: "Alice", Time: base, Text: "hello", ID: 42}

	// Channel chat with backlinks on: each line ends with a t.me/c link.
	channel := telegram.ChatMessages{Title: "Team", ChannelID: 123, Messages: []telegram.Message{m}}
	if got, want := serializeChat(channel, loc, true), "## Team\n[14:32] Alice: hello https://t.me/c/123/42\n"; got != want {
		t.Fatalf("backlinks on\n got: %q\nwant: %q", got, want)
	}
	// Same chat, backlinks off: no link.
	if got, want := serializeChat(channel, loc, false), "## Team\n[14:32] Alice: hello\n"; got != want {
		t.Fatalf("backlinks off\n got: %q\nwant: %q", got, want)
	}
	// User/basic-group chat (ChannelID 0): no link even with backlinks on.
	plain := telegram.ChatMessages{Title: "DM", ChannelID: 0, Messages: []telegram.Message{m}}
	if got, want := serializeChat(plain, loc, true), "## DM\n[14:32] Alice: hello\n"; got != want {
		t.Fatalf("no channel id\n got: %q\nwant: %q", got, want)
	}
	// Channel with a public username: backlink uses the public form (M28).
	public := telegram.ChatMessages{Title: "Public", ChannelID: 123, Username: "netology", Messages: []telegram.Message{m}}
	if got, want := serializeChat(public, loc, true), "## Public\n[14:32] Alice: hello https://t.me/netology/42\n"; got != want {
		t.Fatalf("public username backlink\n got: %q\nwant: %q", got, want)
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

	got := serializeChat(chat, loc, false)
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

	got := serializeAll(chats, loc, false)
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
	fp := &fakeProvider{reply: func(llm.Input) (llm.Result, error) { return llm.Result{Text: "  digest  "}, nil }}
	opts := Options{OutputLang: "ru", Location: time.UTC, Model: "m", MaxTokens: 100}

	out, _, err := Build(context.Background(), fp, smallChats(), opts)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if out != "digest" {
		t.Fatalf("output not trimmed: %q", out)
	}
	if len(fp.calls) != 1 {
		t.Fatalf("expected exactly 1 call, got %d", len(fp.calls))
	}
	if fp.calls[0].System != singleSystem("ru", "") {
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
	if _, _, err := Build(context.Background(), fp, chats, opts); err != nil {
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
	if fp.calls[2].System != reduceSystem("en", "") {
		t.Fatalf("last call should be the reduce step")
	}
	// Reduce input must carry both chat titles assembled from map outputs.
	if !strings.Contains(fp.calls[2].User, "## A") || !strings.Contains(fp.calls[2].User, "## B") {
		t.Fatalf("reduce input missing chat titles: %q", fp.calls[2].User)
	}
}

func TestBuildMapReduceSkipsEmptyMapOutput(t *testing.T) {
	// A map step that returns blank for chat B should be dropped from reduce input.
	fp := &fakeProvider{reply: func(in llm.Input) (llm.Result, error) {
		if in.System == mapSystem("en") && strings.Contains(in.User, "## B") {
			return llm.Result{Text: "   "}, nil
		}
		return llm.Result{Text: "bullets"}, nil
	}}
	opts := Options{OutputLang: "en", Location: time.UTC, ThresholdChars: 1}

	if _, _, err := Build(context.Background(), fp, smallChats(), opts); err != nil {
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

	out, _, err := Build(context.Background(), fp, chats, Options{OutputLang: "ru"})
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
	fp := &fakeProvider{reply: func(llm.Input) (llm.Result, error) { return llm.Result{}, wantErr }}

	_, _, err := Build(context.Background(), fp, smallChats(), Options{OutputLang: "ru", Location: time.UTC})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped provider error, got %v", err)
	}
}

func TestBuildAggregatesUsageSinglePass(t *testing.T) {
	fp := &fakeProvider{reply: func(llm.Input) (llm.Result, error) {
		return llm.Result{Text: "digest", InputTokens: 30, OutputTokens: 12, Truncated: true}, nil
	}}

	_, usage, err := Build(context.Background(), fp, smallChats(), Options{OutputLang: "ru", Location: time.UTC})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if usage.InputTokens != 30 || usage.OutputTokens != 12 {
		t.Fatalf("single-pass usage = %+v, want in=30 out=12", usage)
	}
	if !usage.Truncated {
		t.Fatalf("Truncated should propagate from the only call")
	}
}

func TestBuildAggregatesUsageMapReduce(t *testing.T) {
	// 2 map calls + 1 reduce; tokens must sum and Truncated must OR across calls.
	fp := &fakeProvider{reply: func(in llm.Input) (llm.Result, error) {
		// Only the reduce step truncates here.
		truncated := in.System == reduceSystem("en", "")
		return llm.Result{Text: "bullets", InputTokens: 10, OutputTokens: 4, Truncated: truncated}, nil
	}}
	opts := Options{OutputLang: "en", Location: time.UTC, ThresholdChars: 1}

	_, usage, err := Build(context.Background(), fp, smallChats(), opts)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if usage.InputTokens != 30 || usage.OutputTokens != 12 {
		t.Fatalf("map-reduce usage = %+v, want in=30 out=12 (3 calls)", usage)
	}
	if !usage.Truncated {
		t.Fatalf("Truncated should be true when any call (here reduce) truncates")
	}
}

func TestBuildExtraInstructionsSinglePass(t *testing.T) {
	const extra = "Prefix the digest with a one-line TL;DR."
	fp := &fakeProvider{}
	opts := Options{OutputLang: "ru", Location: time.UTC, ExtraInstructions: extra}

	if _, _, err := Build(context.Background(), fp, smallChats(), opts); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(fp.calls) != 1 {
		t.Fatalf("expected single pass, got %d calls", len(fp.calls))
	}
	if !strings.Contains(fp.calls[0].System, extra) {
		t.Fatalf("extra_instructions missing from single system prompt")
	}
	if fp.calls[0].System != singleSystem("ru", extra) {
		t.Fatalf("single system should equal singleSystem(lang, extra)")
	}
}

func TestBuildExtraInstructionsMapReduceOnlyFinal(t *testing.T) {
	const extra = "Group items by topic across chats."
	fp := &fakeProvider{}
	opts := Options{OutputLang: "en", Location: time.UTC, ThresholdChars: 1, ExtraInstructions: extra}

	if _, _, err := Build(context.Background(), fp, smallChats(), opts); err != nil {
		t.Fatalf("Build: %v", err)
	}
	// Map calls (0,1) must NOT carry the extra; reduce (2) must.
	for i := range 2 {
		if strings.Contains(fp.calls[i].System, extra) {
			t.Fatalf("map call %d should not include extra_instructions", i)
		}
		if fp.calls[i].System != mapSystem("en") {
			t.Fatalf("map system must stay the unmodified mapSystem (byte-identity)")
		}
	}
	if !strings.Contains(fp.calls[2].System, extra) {
		t.Fatalf("reduce system must include extra_instructions")
	}
}
