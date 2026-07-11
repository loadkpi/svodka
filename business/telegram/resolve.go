package telegram

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/gotd/td/constant"
	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/telegram/query"
	"github.com/gotd/td/telegram/query/dialogs"
	"github.com/gotd/td/tg"
)

// Peer is a resolved source chat: an input peer ready for history calls plus a
// human-readable title used in the digest.
type Peer struct {
	Input tg.InputPeerClass
	Title string
	// Username is the peer's public @username, or "" if it has none. Used to
	// build a public t.me/<username>/<msg> backlink instead of the private
	// t.me/c/<id>/<msg> form (M28).
	Username string
}

// ErrInviteLink is returned when a source chat is given as an invite link.
// Resolving it would require joining the chat, which is an unwanted side effect
// of a scheduled run, so invite links are rejected (see ADR-8).
var ErrInviteLink = errors.New("invite links are not supported; add the chat by @username or numeric id")

// errChatIDNotFound is returned when a numeric chat id has no match in the
// scanned dialogs. No id in the message: it crosses the business->app
// boundary (NFR-2); the caller has chat_index for context.
var errChatIDNotFound = errors.New("chat id not found among your dialogs (join it or use @username)")

// Resolver turns config chat references (@username, t.me links, numeric ids)
// into peers. A single Resolver is built per run so the peers.Manager cache and
// the lazily-scanned dialog map are shared across all source chats.
type Resolver struct {
	api     *tg.Client
	manager *peers.Manager

	// dialogByID maps a TDLib peer id to its resolved peer. It is populated
	// lazily on the first numeric lookup via a single GetDialogs scan, because
	// a bare numeric id cannot be resolved without an access_hash (ADR-8).
	dialogByID map[int64]Peer
	scanned    bool
}

// NewResolver builds a Resolver over the raw API client.
func NewResolver(api *tg.Client) *Resolver {
	manager := peers.Options{
		Storage: &peers.InmemoryStorage{},
		Cache:   &peers.InmemoryCache{},
	}.Build(api)
	return &Resolver{api: api, manager: manager}
}

// Resolve maps a single config reference to a Peer. Usernames, t.me links and
// domains go through peers.Manager; numeric ids fall back to a dialog scan.
func (r *Resolver) Resolve(ctx context.Context, ref string) (Peer, error) {
	kind, value, err := classifyInput(ref)
	if err != nil {
		return Peer{}, err
	}
	if kind == kindNumeric {
		return r.resolveNumeric(ctx, value)
	}
	p, err := r.manager.Resolve(ctx, value)
	if err != nil {
		// No ref in the error: it crosses the business->app boundary and would
		// otherwise leak which chats are tracked into logs (NFR-2). The caller
		// has chat_index for context.
		return Peer{}, fmt.Errorf("resolve chat: %w", err)
	}
	username, _ := p.Username()
	return Peer{Input: p.InputPeer(), Title: p.VisibleName(), Username: username}, nil
}

// resolveNumeric looks a TDLib peer id up in the lazily-scanned dialog map.
func (r *Resolver) resolveNumeric(ctx context.Context, value string) (Peer, error) {
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return Peer{}, fmt.Errorf("invalid numeric chat id: %w", err)
	}
	if err := r.ensureDialogs(ctx); err != nil {
		return Peer{}, fmt.Errorf("scan dialogs: %w", err)
	}
	p, ok := r.dialogByID[id]
	if !ok {
		return Peer{}, errChatIDNotFound
	}
	return p, nil
}

// ensureDialogs walks every dialog once and records id -> peer. Each dialog's
// input peer already carries the access_hash, so no extra lookups are needed.
func (r *Resolver) ensureDialogs(ctx context.Context) error {
	if r.scanned {
		return nil
	}
	byID := make(map[int64]Peer)
	err := query.GetDialogs(r.api).BatchSize(100).ForEach(ctx, func(ctx context.Context, e dialogs.Elem) error {
		id, ok := tdlibID(e.Peer)
		if !ok {
			return nil
		}
		byID[id] = Peer{Input: e.Peer, Title: dialogTitle(e), Username: dialogUsername(e)}
		return nil
	})
	if err != nil {
		return err
	}
	r.dialogByID = byID
	r.scanned = true
	return nil
}

// tdlibID computes the TDLib-style peer id (the form Telegram clients show, e.g.
// -100… for channels) for a dialog's input peer. Self and other peer kinds that
// cannot be a source chat are skipped.
func tdlibID(p tg.InputPeerClass) (int64, bool) {
	var id constant.TDLibPeerID
	switch v := p.(type) {
	case *tg.InputPeerUser:
		id.User(v.UserID)
	case *tg.InputPeerChat:
		id.Chat(v.ChatID)
	case *tg.InputPeerChannel:
		id.Channel(v.ChannelID)
	default:
		return 0, false
	}
	return int64(id), true
}

// dialogTitle reads the display title for a dialog from its entities.
func dialogTitle(e dialogs.Elem) string {
	switch v := e.Peer.(type) {
	case *tg.InputPeerUser:
		if u, ok := e.Entities.User(v.UserID); ok {
			return userTitle(u)
		}
	case *tg.InputPeerChat:
		if c, ok := e.Entities.Chat(v.ChatID); ok {
			return c.Title
		}
	case *tg.InputPeerChannel:
		if c, ok := e.Entities.Channel(v.ChannelID); ok {
			return c.Title
		}
	}
	return ""
}

// dialogUsername reads the peer's main public @username from a dialog's
// entities, or "" if it has none (basic groups never have one; users and
// channels may go without). Only the user/channel peer kinds carry a username
// (M28).
func dialogUsername(e dialogs.Elem) string {
	switch v := e.Peer.(type) {
	case *tg.InputPeerUser:
		if u, ok := e.Entities.User(v.UserID); ok {
			if username, ok := u.GetUsername(); ok {
				return username
			}
		}
	case *tg.InputPeerChannel:
		if c, ok := e.Entities.Channel(v.ChannelID); ok {
			if username, ok := c.GetUsername(); ok {
				return username
			}
		}
	}
	return ""
}

// userTitle joins a user's first and last name.
func userTitle(u *tg.User) string {
	name := strings.TrimSpace(u.FirstName + " " + u.LastName)
	if name != "" {
		return name
	}
	if username, ok := u.GetUsername(); ok {
		return "@" + username
	}
	return ""
}

type inputKind int

const (
	kindUsername inputKind = iota
	kindNumeric
)

var numericRe = regexp.MustCompile(`^-?\d+$`)

// classifyInput normalises a config chat reference into a kind and a clean
// value: it strips the t.me host, a leading '@' and surrounding slashes, then
// decides between a username and a numeric id. Invite links are rejected. This
// is the pure, network-free core covered by unit tests (ADR-7).
func classifyInput(raw string) (inputKind, string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, "", errors.New("empty chat reference")
	}
	s = stripTMe(s)
	s = strings.TrimPrefix(s, "@")
	s = strings.Trim(s, "/")
	if s == "" {
		return 0, "", errors.New("empty chat reference")
	}
	if strings.HasPrefix(s, "+") || strings.HasPrefix(strings.ToLower(s), "joinchat/") {
		return 0, "", ErrInviteLink
	}
	if numericRe.MatchString(s) {
		return kindNumeric, s, nil
	}
	return kindUsername, s, nil
}

// stripTMe removes an optional scheme and a t.me-style host, returning the path
// that follows. Inputs without such a host are returned unchanged.
func stripTMe(s string) string {
	for _, scheme := range []string{"https://", "http://"} {
		if len(s) >= len(scheme) && strings.EqualFold(s[:len(scheme)], scheme) {
			s = s[len(scheme):]
			break
		}
	}
	for _, host := range []string{"t.me/", "telegram.me/", "telegram.dog/"} {
		if len(s) >= len(host) && strings.EqualFold(s[:len(host)], host) {
			return s[len(host):]
		}
	}
	return s
}
