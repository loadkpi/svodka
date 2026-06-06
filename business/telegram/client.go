package telegram

import (
	"context"

	"github.com/gotd/contrib/middleware/floodwait"
	"github.com/gotd/td/session"
	gotgram "github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"

	"svodka/config"
)

// Client is a thin wrapper around gotd's telegram.Client that fixes the
// project's standard options in one place. The gotd package is imported as
// gotgram so it does not shadow this package's own name (telegram).
type Client struct {
	tg      *gotgram.Client
	storage *session.StorageMemory
}

// New builds a Client using credentials from cfg. When cfg.Telegram.Session is
// non-empty it is loaded into storage; otherwise an empty storage is used so
// the login command can perform initial auth.
func New(ctx context.Context, cfg *config.Config) (*Client, error) {
	storage, err := loadOrEmpty(ctx, cfg.Telegram.Session)
	if err != nil {
		return nil, err
	}
	opts := gotgram.Options{
		SessionStorage: storage,
		Middlewares: []gotgram.Middleware{
			floodwait.NewSimpleWaiter(),
		},
		// Logger left nil on purpose: the gotd zap logger would otherwise
		// pour MTProto traffic (including message data) into stderr.
	}
	c := gotgram.NewClient(cfg.Telegram.APIID, cfg.Telegram.APIHash, opts)
	return &Client{tg: c, storage: storage}, nil
}

func loadOrEmpty(ctx context.Context, b64 string) (*session.StorageMemory, error) {
	if b64 == "" {
		return &session.StorageMemory{}, nil
	}
	return LoadStorage(ctx, b64)
}

// Run connects to Telegram and invokes fn with the raw tg API. It is a thin
// pass-through to (*gotgram.Client).Run.
func (c *Client) Run(ctx context.Context, fn func(ctx context.Context, api *tg.Client) error) error {
	return c.tg.Run(ctx, func(ctx context.Context) error {
		return fn(ctx, c.tg.API())
	})
}

// Auth exposes the gotd auth client (used by login and status checks).
func (c *Client) Auth() *auth.Client {
	return c.tg.Auth()
}

// Storage returns the in-memory session storage. The login command exports it
// to base64 after a successful sign-in.
func (c *Client) Storage() *session.StorageMemory {
	return c.storage
}
