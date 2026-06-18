# svodka

A Telegram daily-digest userbot: it reads the history of the chats you pick over a
time window, summarizes it with Claude, and posts the digest back to Telegram. It
runs as a scheduled GitHub Actions job — no servers, no database.

> 🇷🇺 На русском: [README.ru.md](README.ru.md)

## How it works (at a glance)

svodka signs in to Telegram **as you** (an MTProto userbot, not a bot), because a
regular bot cannot read the history of arbitrary chats. You sign in once,
interactively; that produces a base64 *session* which is stored as a GitHub secret
and reused by every scheduled run. Each run reads the configured chats over the last
`window_hours`, summarizes them with Claude (map-reduce when the volume is large), and
posts the digest to your `target_chat`.

## Deploy

### Prerequisites
- A Telegram account.
- `api_id` and `api_hash` from <https://my.telegram.org> → **API development tools**.
- An Anthropic API key from <https://console.anthropic.com>.

### Why a private repository
A public repo would expose your Actions logs and the account they run under. Create
the project from this template as a **private** repository.

### Steps

1. [![Use this template](https://img.shields.io/badge/Use_this_template-2ea44f?style=for-the-badge&logo=github&logoColor=white)](https://github.com/new?template_name=svodka&template_owner=loadkpi) — set visibility to **Private**, click **Create repository**.
2. Get `api_id` / `api_hash` at <https://my.telegram.org>.
3. **Sign in to Telegram** (one time). Open a Codespace — a browser terminal, so you
   need no local toolchain — make `api_id`/`api_hash` available with the `SVODKA_`
   prefix, and run the login command. The Codespace may take a few minutes to start
   ("Building codespace…") before a terminal is available — this is normal, just wait.
   ```sh
   export SVODKA_TELEGRAM_API_ID=<api_id>
   export SVODKA_TELEGRAM_API_HASH=<api_hash>
   go run ./api/cmd/login
   ```
   Enter your phone number, the code Telegram sends you, and your 2FA password if you
   have one. On success the session is saved:
   - If the `gh` CLI is present and allowed to write secrets, login stores it
     automatically as the `SVODKA_TELEGRAM_SESSION` secret.
   - Otherwise login prints the session string with a warning. Copy it into
     **Settings → Secrets and variables → Actions** as `SVODKA_TELEGRAM_SESSION`
     yourself, then clear your terminal.

   The login command never logs your phone, code or password, and prints the session
   only in the manual-fallback case.
4. **Add your Anthropic key** as a repository Actions secret `SVODKA_ANTHROPIC_KEY`
   (Settings → Secrets and variables → Actions). If you also want to test-run from the
   Codespace, export it there too.
5. **Edit `config.yml`** (the web UI is fine): list your `source_chats` and keep
   `target_chat: "me"` for now. See [Configuration](#configuration) below.
6. **Test it.** Open the **Actions** tab, enable workflows if prompted, pick
   **Daily digest** → **Run workflow**. With `target_chat: "me"` the digest lands in
   your **Saved Messages**. Confirm the run logs are clean (only metrics — no message
   content or secrets).
7. **Go live.** Change `target_chat` to a group/channel you can post to. Adjust the
   schedule if you like — see [Schedule](#schedule).

## Configuration

Non-secret settings live in `config.yml` (edit it in the web UI). Secrets never go
here. See [`config.example.yml`](config.example.yml) for a documented example.

| Setting | Default | Meaning |
|---|---|---|
| `source_chats` | — (required) | Chats to read: `@username`, a `t.me/<name>` link, or a numeric id for private chats without a username. |
| `target_chat` | `"me"` | Where to post. `"me"` = your Saved Messages (safe preview); `"@group"` = a group/channel (go live). |
| `window_hours` | `24` | How far back to read, in hours. |
| `output_lang` | `"ru"` | Digest language (ISO 639-1, e.g. `ru`, `en`). |
| `timezone` | `"UTC"` | IANA timezone for time wording in the digest (the example uses `Europe/Belgrade`). |
| `model` | `"claude-sonnet-4-6"` | Anthropic model id (cheaper ↔ better): `claude-haiku-4-5` (cheapest/fastest, fine for most digests) · `claude-sonnet-4-6` (balanced) · `claude-opus-4-8` (highest quality). |
| `max_output_tokens` | `2000` | Output token budget for the summary. |
| `extra_instructions` | `""` | Optional free-text guidance appended to the digest prompt (tone, structure, what to emphasize). Empty = default; Telegram formatting rules still win. |
| `backlinks` | `true` | Add `t.me/c` source links to digest items so you can jump to the original message. Channels/supergroups only; a private link opens **only for members** of that chat. Set `false` to disable. |
| `routes` | — (optional) | Fan out several digests in one run: a list of `{source_chats, target_chat}`. See [Multiple digests](#multiple-digests-routes) below. |

### Multiple digests (`routes`)

By default svodka builds **one** digest from `source_chats` and posts it to one
`target_chat`. To send different chats to different targets in a single run, add a
`routes` list — each route is its own `source_chats` → `target_chat`:

```yaml
routes:
  - target_chat: "@team_one"
    source_chats: ["@chat_a", "@chat_b"]
  - target_chat: "@team_two"   # omit or "" -> Saved Messages
    source_chats: ["@chat_c"]
```

When `routes` is set, the top-level `source_chats`/`target_chat` are ignored, but all
other settings (`window_hours`, `output_lang`, `model`, `backlinks`, …) stay **global**
across every route. Leave `routes` out for the default single-digest behavior. Routes run
independently: if one fails the others still go out, and the run is reported as failed.

### Local one-off overrides (flags)

For a single local run you can override any setting with a flag instead of editing
`config.yml` — handy for tuning the prompt or model and previewing into Saved Messages:

```sh
go run ./api/cmd/svodka \
  --target-chat me --window-hours 2 \
  --model claude-haiku-4-5 \
  --source-chats "@chat_one,@chat_two" \
  --extra-instructions "Keep it to 5 bullets."
```

Precedence is **flag > `config.yml` > default**, and only flags you actually pass take
effect (an unset flag changes nothing). Every setting has a kebab-case flag
(`--window-hours`, `--target-chat`, `--output-lang`, `--timezone`, `--model`,
`--max-output-tokens`, `--extra-instructions`, `--backlinks`, `--source-chats`); run with `--help` to
list them. The **scheduled run in GitHub Actions uses `config.yml`** — flags are a local
convenience only.

## Secrets

All secrets are GitHub Actions secrets, prefixed with `SVODKA_`. They never appear in
`config.yml`, the code, logs or output.

| Secret | Where from | Notes |
|---|---|---|
| `SVODKA_TELEGRAM_API_ID` | my.telegram.org | integer |
| `SVODKA_TELEGRAM_API_HASH` | my.telegram.org | keep private |
| `SVODKA_TELEGRAM_SESSION` | produced by `login` | **full access to your account** — never share |
| `SVODKA_ANTHROPIC_KEY` | console.anthropic.com | Anthropic API key |

> **Heads-up on the variable name.** The config reader adds the `SVODKA_` prefix
> automatically, so a missing-value error may print the bare tag, e.g.
> `required field APIID (env: TELEGRAM_API_ID)`. The variable you actually set is
> **`SVODKA_TELEGRAM_API_ID`** — with the prefix.

## Schedule

The job runs daily at **06:00 UTC** by default. Edit the `cron` line in
[`.github/workflows/daily.yml`](.github/workflows/daily.yml) to change it, or use
**Run workflow** for a manual run any time.

- **GitHub cron is always UTC.** Subtract your UTC offset to pick a local time
  (e.g. 08:00 Europe/Belgrade in summer = 06:00 UTC).
- Scheduled runs are **best-effort** and may be delayed.
- A scheduled workflow is **disabled after 60 days** without repository activity —
  push a commit or re-enable it to resume.
- Runner IPs change between runs, so Telegram may occasionally show a **"new login"**
  notification; your existing session keeps working.

## Security & privacy
- The session grants full access to your Telegram account. Keep the repo private and
  never commit or share the session string.
- Secrets live only in GitHub Secrets — never in `config.yml` or the code.
- The logger records only metrics and your numeric user id, never message content;
  the underlying MTProto logger is disabled.

## License
MIT — see [LICENSE](LICENSE).
