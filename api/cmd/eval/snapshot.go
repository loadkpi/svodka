package main

import (
	"encoding/json"
	"fmt"
	"time"

	"svodka/business/telegram"
)

// currentSchema is the snapshot format version. decodeSnapshot rejects any
// other value so a future format change fails loudly instead of silently
// misinterpreting fields that changed shape or meaning.
const currentSchema = 1

// Snapshot is eval capture's on-disk output: a frozen input so eval run needs
// no Telegram secrets and every candidate model sees byte-identical input
// (ADR-21). Settings mirrors just what digest.Build needs to reconstruct
// Options; it deliberately excludes llm_provider/model — run picks the model
// explicitly per candidate via --models.
type Snapshot struct {
	Schema     int                     `json:"schema"`
	CapturedAt time.Time               `json:"captured_at"`
	Settings   SnapshotSettings        `json:"settings"`
	Chats      []telegram.ChatMessages `json:"chats"`
}

// SnapshotSettings is the subset of config settings digest.Build needs,
// frozen at capture time so run needs neither config.yml nor Telegram
// secrets.
type SnapshotSettings struct {
	WindowHours       int    `json:"window_hours"`
	OutputLang        string `json:"output_lang"`
	Timezone          string `json:"timezone"`
	Backlinks         bool   `json:"backlinks"`
	ExtraInstructions string `json:"extra_instructions"`
	MaxOutputTokens   int    `json:"max_output_tokens"`
}

// encodeSnapshot renders s as indented JSON, human-readable for anyone
// peeking at a capture file directly.
func encodeSnapshot(s Snapshot) ([]byte, error) {
	return json.MarshalIndent(s, "", "  ")
}

// decodeSnapshot parses data into a Snapshot and rejects an unsupported
// schema version outright, rather than silently misreading fields that may
// have changed shape or meaning across versions.
func decodeSnapshot(data []byte) (Snapshot, error) {
	var s Snapshot
	if err := json.Unmarshal(data, &s); err != nil {
		return Snapshot{}, fmt.Errorf("parse snapshot: %w", err)
	}
	if s.Schema != currentSchema {
		return Snapshot{}, fmt.Errorf("unsupported snapshot schema %d (want %d)", s.Schema, currentSchema)
	}
	return s, nil
}
