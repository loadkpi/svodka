package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

const fakeSession = "QkFTRTY0LVNFU1NJT04tU0VDUkVU" // base64-looking sentinel

// TestPersistSessionStoredMasksValue is the security check: when the setter
// succeeds, the session string must not appear on stdout or stderr.
func TestPersistSessionStoredMasksValue(t *testing.T) {
	var got string
	setter := func(name, value string) error {
		if name != sessionSecret {
			t.Errorf("secret name = %q, want %q", name, sessionSecret)
		}
		got = value
		return nil
	}

	var stdout, stderr bytes.Buffer
	stored := persistSession(&stdout, &stderr, setter, fakeSession)

	if !stored {
		t.Fatal("stored = false, want true on setter success")
	}
	if got != fakeSession {
		t.Errorf("setter received %q, want the session", got)
	}
	if strings.Contains(stdout.String(), fakeSession) {
		t.Errorf("session leaked to stdout: %q", stdout.String())
	}
	if strings.Contains(stderr.String(), fakeSession) {
		t.Errorf("session leaked to stderr: %q", stderr.String())
	}
}

// TestPersistSessionFallbackOnError: a failing setter must fall back to printing
// the session on stdout with a warning on stderr.
func TestPersistSessionFallbackOnError(t *testing.T) {
	setter := func(name, value string) error { return errors.New("boom") }

	var stdout, stderr bytes.Buffer
	stored := persistSession(&stdout, &stderr, setter, fakeSession)

	if stored {
		t.Fatal("stored = true, want false on setter error")
	}
	if !strings.Contains(stdout.String(), fakeSession) {
		t.Errorf("session not printed to stdout on fallback: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "FULL ACCESS") {
		t.Errorf("warning not printed to stderr: %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "boom") {
		t.Errorf("setter error not surfaced on stderr: %q", stderr.String())
	}
}

// TestPersistSessionNoGh: a nil setter (gh unavailable) goes straight to the
// manual fallback.
func TestPersistSessionNoGh(t *testing.T) {
	var stdout, stderr bytes.Buffer
	stored := persistSession(&stdout, &stderr, nil, fakeSession)

	if stored {
		t.Fatal("stored = true, want false when setter is nil")
	}
	if !strings.Contains(stdout.String(), fakeSession) {
		t.Errorf("session not printed to stdout: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "FULL ACCESS") {
		t.Errorf("warning not printed to stderr: %q", stderr.String())
	}
}
