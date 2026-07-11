package main

import (
	"reflect"
	"testing"
	"time"

	"svodka/business/telegram"
)

// TestSnapshotRoundTrip encodes then decodes a Snapshot and checks the result
// matches the original. CapturedAt uses a fixed non-UTC offset deliberately:
// JSON round-trips time.Time via RFC3339 which preserves the instant but not
// necessarily the original *Location value, so the time field is compared
// with time.Time.Equal (instant equality) while the rest is compared with
// reflect.DeepEqual on a copy that has CapturedAt zeroed out.
func TestSnapshotRoundTrip(t *testing.T) {
	loc := time.FixedZone("UTC+3", 3*60*60)
	want := Snapshot{
		Schema:     currentSchema,
		CapturedAt: time.Date(2026, 7, 11, 12, 30, 0, 0, loc),
		Settings: SnapshotSettings{
			WindowHours:       24,
			OutputLang:        "ru",
			Timezone:          "Europe/Belgrade",
			Backlinks:         true,
			ExtraInstructions: "be terse",
			MaxOutputTokens:   2000,
		},
		Chats: []telegram.ChatMessages{
			{
				Title:     "chat one",
				ChannelID: 123,
				Messages: []telegram.Message{
					{Author: "alice", Time: time.Date(2026, 7, 11, 10, 0, 0, 0, time.UTC), Text: "hello", ID: 1},
				},
			},
		},
	}

	data, err := encodeSnapshot(want)
	if err != nil {
		t.Fatalf("encodeSnapshot: %v", err)
	}
	got, err := decodeSnapshot(data)
	if err != nil {
		t.Fatalf("decodeSnapshot: %v", err)
	}

	if !got.CapturedAt.Equal(want.CapturedAt) {
		t.Errorf("CapturedAt = %v, want %v (same instant)", got.CapturedAt, want.CapturedAt)
	}
	got.CapturedAt = want.CapturedAt // neutralize location-only diff before DeepEqual
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round trip mismatch\n got: %+v\nwant: %+v", got, want)
	}
}

// TestDecodeSnapshotRejectsUnsupportedSchema ensures a future/foreign schema
// version fails loudly instead of being silently misinterpreted.
func TestDecodeSnapshotRejectsUnsupportedSchema(t *testing.T) {
	data := []byte(`{"schema": 2, "captured_at": "2026-07-11T00:00:00Z"}`)
	_, err := decodeSnapshot(data)
	if err == nil {
		t.Fatal("want error for schema 2, got nil")
	}
}
