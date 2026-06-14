# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`svodka` is a Telegram daily-digest **userbot**: a short-lived Go CLI that signs in to
Telegram *as a real user* (MTProto, not the Bot API — a bot can't read arbitrary chat
history), reads configured chats over a time window, summarizes them with Claude
(map-reduce when volume is large), and posts the digest back to Telegram. It runs as a
scheduled GitHub Actions job — **no servers, no database**. The repo is also a public
GitHub *template*; users deploy by creating a private repo from it.

## Commands

```sh
make build        # builds bin/svodka and bin/login
make vet          # go vet ./...
make test         # go test ./...
make run          # go run ./api/cmd/svodka
make login        # go run ./api/cmd/login (interactive Telegram sign-in)

go test -run TestName ./business/digest/   # single test
gofmt -l .                                  # list unformatted files (must be empty)
golangci-lint run                           # standard linter set (see .golangci.yml)
```

**The acceptance gate** is `build + vet + test + gofmt -l + golangci-lint`, all green.
Every milestone ends with it. Invoke it with the `/green` slash command (read-only,
runs all five and reports a pass/fail table) — prefer it over running checks ad-hoc.

### Running locally

The committed `config.yml` is a safe placeholder (`source_chats: []`). Put real chats in
the gitignored `config.local.yml` and point at it, or override per-run with flags:

```sh
SVODKA_CONFIG=config.local.yml go run ./api/cmd/svodka
go run ./api/cmd/svodka --config config.local.yml --target-chat me --window-hours 2 --source-chats "@a,@b"
```

A functional run needs the user's own Telegram + Anthropic credentials (env vars prefixed
`SVODKA_`, e.g. `SVODKA_TELEGRAM_API_ID`, `SVODKA_ANTHROPIC_KEY`). Development verifies
build/vet/test + review; live Telegram/Claude runs are done by the deployer per README.

## Architecture

Ardan-flavored layering; **dependencies point inward**:
`foundation` ← `config`/`business` ← `app` ← `api/cmd`. (The standard Ardan
`business/{domain,sdk}` and `app/domain` split is intentionally collapsed — ~600 lines of
logic didn't warrant three levels.)

- `api/cmd/svodka` — the daily job entrypoint. `config.Load` → `telegram.New` →
  `client.Run(...)`; everything else executes **inside** `client.Run` because gotd needs a
  live `*tg.Client`.
- `api/cmd/login` — one-time interactive sign-in. Produces a base64 session and stores it
  as the `SVODKA_TELEGRAM_SESSION` GitHub secret (via `gh secret set`, with a manual print
  fallback).
- `app/summarize` — the thin use-case orchestrator. Runs steps 3–5: resolve+fetch each
  chat → `digest.Build` → `telegram.Send`. Tolerant of a single chat failing (warn+skip);
  hard-fails on all-chats-failed, LLM error, or Send error. Empty digest = success, no send.
- `business/telegram` — gotd wrappers: `client.go` (Client assembly + floodwait
  middleware), `session.go` (base64 ↔ in-memory session), `resolve.go` (refs → peers),
  `history.go` (windowed GetHistory + normalize to `[]Message`), `send.go` (send + 4096 split).
- `business/digest` — prompt construction (`prompt.go`), user-text serialization +
  backlinks (`serialize.go`), and the hybrid single-vs-map-reduce `Build`.
- `business/llm` — `Provider` interface + `claude.go` (Anthropic SDK, prompt caching).
- `config` — `Config = Secrets + Settings`; `Load()` / `LoadSecrets()`.
- `foundation/logger` — slog wrapper; logs metrics only, never content.

### Design invariants you must not break

These are load-bearing decisions, each backed by an ADR — read the relevant ADR in
`docs/architecture.md` before touching the area:

- **No message content or secrets in logs (NFR-2).** The logger records counts, timings,
  and the numeric user id only. The gotd MTProto logger is deliberated disabled (`Logger:
  nil`). Failures are logged by chat *index*, never by chat ref. Content goes to stdout
  only behind an explicit flag.
- **Prompt-cache byte-identity (ADR-9/10/15).** The map-phase system prompt
  (`mapSystem`) must be **byte-identical across all map calls within a run** so
  `cache_control` pays off. Anything per-message (e.g. backlink URLs, absolute times) lives
  in the *user* text, never the system block. `extra_instructions` is appended to
  single/reduce systems only — never to map.
- **Config dual-source (ADR-3).** Secrets/flags come from env via `ardanlabs/conf`;
  user settings come from `config.yml` via `yaml.v3`. conf's yaml parser silently drops
  zero values (`false`/`0`/`""`), which would make settings un-disable-able — hence the
  split. The same reason is why `Backlinks` is `*bool` and CLI overrides are pointer fields
  (`nil` = flag absent; explicit zero is honored). Precedence: **flag > config.yml > default**.
- **No DB, no dry_run boolean.** Only the session is persisted (as a secret). Safe-preview
  mode is expressed as `target_chat: me` (Saved Messages), not a flag (ADR-4/ADR-5).
- **Backlinks are private `t.me/c/<id>/<msg>` only**, built by our code for
  channels/supergroups; the model attaches them to bullets but never invents them (ADR-15).

### Testing policy (ADR-7)

Unit-test **pure logic only** — config parse/defaults/validation, message normalization,
digest assembly, the 4096 split, override application, `splitChats`, `linkFor`, etc. Thin
gotd/HTTP wrappers that need a live Telegram/Claude are *not* unit-tested; they're covered
by build, vet, review, and the deployer's manual run with `target_chat: me`. Inject
dependencies (interfaces, `io.Reader`) to keep cores testable.

## Conventions

- **Comments in code/config/.gitignore are in English; the `docs/` are in Russian.**
- `docs/architecture.md` is the **design source of truth** — every non-trivial decision is
  an ADR (ADR-1 … ADR-15). `docs/plan.md` is the **status source of truth** — milestones
  M0–M13 + M20.1/.2 are done; M14–M19 are planned. Update both when you make decisions.
- Working rhythm: for a new milestone, settle the design fork **before writing code** (the
  `adr-scout` read-only agent produces a decision brief; record the choice as an ADR), then
  implement in the main thread, then run `/green`.
- The scheduled run in Actions always uses `config.yml`; flags are a local-only convenience.
