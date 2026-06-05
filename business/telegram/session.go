// Package telegram wraps gotd/td with the project's standard options:
// in-memory session storage, floodwait middleware, and no library logger
// (Logger: nil) so message data cannot leak into stderr.
package telegram

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/gotd/td/session"
)

// LoadStorage decodes a base64-encoded session blob (as produced by Export)
// into a StorageMemory ready to pass to telegram.Options.SessionStorage.
// Used by the svodka command to bring SVODKA_TELEGRAM_SESSION into the client.
func LoadStorage(ctx context.Context, b64 string) (*session.StorageMemory, error) {
	if b64 == "" {
		return nil, errors.New("empty session blob")
	}
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, fmt.Errorf("decoding base64 session: %w", err)
	}
	var s session.StorageMemory
	if err := s.StoreSession(ctx, raw); err != nil {
		return nil, fmt.Errorf("storing session: %w", err)
	}
	return &s, nil
}

// Export reads the session bytes out of storage and encodes them as base64 for
// transport into a GitHub Actions Secret. Returns an error if the storage is
// empty (no auth has happened yet).
func Export(ctx context.Context, s *session.StorageMemory) (string, error) {
	if s == nil {
		return "", errors.New("nil storage")
	}
	raw, err := s.LoadSession(ctx)
	if err != nil {
		return "", fmt.Errorf("loading session: %w", err)
	}
	if len(raw) == 0 {
		return "", errors.New("session storage is empty")
	}
	return base64.StdEncoding.EncodeToString(raw), nil
}
