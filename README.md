# svodka

A Telegram daily-digest userbot: it reads the history of the chats you pick over a
time window, summarizes it with Claude, and posts the digest back to Telegram. It
runs as a scheduled GitHub Actions job — no servers, no database.

> 🇷🇺 На русском: [README.ru.md](README.ru.md)

> **Status: work in progress.** The **Telegram connection** (interactive login → a
> reusable session) is wired up, and that is what this README covers. Reading
> history, summarization, sending and the scheduled workflow are still being built.

## How it works (at a glance)

svodka signs in to Telegram **as you** (an MTProto userbot, not a bot), because a
regular bot cannot read the history of arbitrary chats. You sign in once,
interactively; that produces a base64 *session* which is stored as a GitHub secret
and reused by every scheduled run.

## Connecting your Telegram account

### Prerequisites
- A Telegram account.
- `api_id` and `api_hash` from <https://my.telegram.org> → **API development tools**.

### Why a private repository
A public repo would expose your Actions logs and the account they run under. Create
the project from this template as a **private** repository.

### Steps

1. **Use this template** → create a new **private** repository.
2. Get `api_id` / `api_hash` at <https://my.telegram.org>.
3. Make them available to the login command as environment variables — note the
   `SVODKA_` prefix. In a Codespace you can add them as **Codespaces secrets**, or
   just export them in the terminal:
   ```sh
   export SVODKA_TELEGRAM_API_ID=<api_id>
   export SVODKA_TELEGRAM_API_HASH=<api_hash>
   ```
4. Run the interactive login. A Codespace gives you a browser terminal, so you need
   no local toolchain:
   ```sh
   go run ./api/cmd/login
   ```
   Enter your phone number, the code Telegram sends you, and your 2FA password if you
   have one.
5. On success the session is saved:
   - If the `gh` CLI is present and allowed to write secrets, login stores it
     automatically as the `SVODKA_TELEGRAM_SESSION` secret.
   - Otherwise login prints the session string with a warning. Copy it into
     **Settings → Secrets and variables → Actions** as `SVODKA_TELEGRAM_SESSION`
     yourself, then clear your terminal.

The login command never logs your phone, code or password, and prints the session
only in the manual-fallback case.

### Secrets used for the connection

| Secret | Where from | Notes |
|---|---|---|
| `SVODKA_TELEGRAM_API_ID` | my.telegram.org | integer |
| `SVODKA_TELEGRAM_API_HASH` | my.telegram.org | keep private |
| `SVODKA_TELEGRAM_SESSION` | produced by `login` | **full access to your account** — never share |

> **Heads-up on the variable name.** The config reader adds the `SVODKA_` prefix
> automatically, so a missing-value error may print the bare tag, e.g.
> `required field APIID (env: TELEGRAM_API_ID)`. The variable you actually set is
> **`SVODKA_TELEGRAM_API_ID`** — with the prefix.

## Security & privacy
- The session grants full access to your Telegram account. Keep the repo private and
  never commit or share the session string.
- Secrets live only in GitHub Secrets — never in `config.yml` or the code.
- The logger records only metrics and your numeric user id, never message content;
  the underlying MTProto logger is disabled.

## License
MIT — see [LICENSE](LICENSE).
