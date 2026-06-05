# svodka — Handoff (для продолжения в новой сессии)

Дата заморозки: 2026-06-03 (после закрытия M0 и M1).

## Где читать контекст
- `docs/requirements.md` — что и зачем.
- `docs/architecture.md` — как устроено + ADR (важные решения).
- `docs/plan.md` — подзадачи со статусами (чекбоксы).
- Plan-файлы Claude Code:
  - `~/.claude/plans/swift-wondering-pnueli.md` — исходный план фазы M0.
  - `~/.claude/plans/1-pavel-kozlov-2-hazy-sloth.md` — план закрытия M0.6/M0.7.

## Текущее состояние

**M0 (скаффолд) — закрыт `[x]`:**
- `go.mod` (`module svodka`, Go 1.25). Прямые зависимости: `gotd/td v0.144.0`,
  `gotd/contrib v0.21.1`, `ardanlabs/conf/v3 v3.12.0`, `gopkg.in/yaml.v3`.
- `foundation/logger/logger.go` — slog-обёртка (Info/Warn/Error с ctx).
- `config/config.go` — `Secrets` (conf/env) + `Settings` (yaml.v3) + `Load`/`LoadSecrets`.
  `LoadSecrets()` не требует session — подходит для login.
- `config.yml`, `config.example.yml`.
- `.gitignore` (включая `session*.json`, `session*.txt`, `*.local`, `bin/`, IDE/OS).
- `LICENSE` (MIT, Pavel Kozlov 2026).
- `Makefile` (default = `help`; цели: tidy/vet/build/run/login).
- `docs/*`.

**M1 (Telegram core + svodka main) — закрыт `[x]`:**
- `business/telegram/session.go` — `LoadStorage(ctx, b64)` / `Export(ctx, s)` через
  `encoding/base64`. Поверх `session.StorageMemory{StoreSession, LoadSession}`.
- `business/telegram/client.go` — обёртка `Client`:
  - `New(ctx, *config.Config) (*Client, error)` — собирает `telegram.NewClient`
    с `Options{SessionStorage, Middlewares: [floodwait.NewSimpleWaiter()], Logger: nil}`.
  - `Run(ctx, fn func(ctx, *tg.Client) error) error` — pass-through к `tg.Client.Run`,
    прокидывает `c.tg.API()` в fn.
  - `Auth() *auth.Client` — для status-check и login.
  - `Storage() *session.StorageMemory` — для login (Export после auth).
- `api/cmd/svodka/main.go` — entrypoint: `signal.NotifyContext(INT/TERM)` → `config.Load()`
  → `telegram.New` → `client.Run` → `Auth().Status`; smoke-лог `user_id` (БЕЗ username/имени —
  логи Actions могут быть публичны). Обработка `config.ErrHelp` для `--help`.
- Build/vet — зелёные.

**Что НЕ сделано — следующий шаг M2 (Login-команда, Codespaces):**
1. M2.1 `business/telegram/auth.go` (или `term_auth.go`) — `termAuth` тип, реализующий
   `auth.UserAuthenticator`: Phone (stdin), Code (stdin), Password (stdin — для 2FA),
   `AcceptTermsOfService`, `SignUp` → возвращать ошибку «аккаунт не существует, создайте
   через официальный клиент».
2. M2.2 `api/cmd/login/main.go` — `config.LoadSecrets()` (нужны только api_id/hash;
   session ещё нет — поэтому НЕ `Load()`), `telegram.New(ctx, cfg)` с пустым cfg.Telegram.Session;
   `client.Run` → `client.Auth().IfNecessary(ctx, auth.NewFlow(termAuth, auth.SendCodeOptions{}))`.
3. M2.3 По успеху: `telegram.Export(ctx, client.Storage())` → base64 → попытка
   `exec.Command("gh", "secret", "set", "SVODKA_TELEGRAM_SESSION")` со stdin = base64;
   fallback при ошибке gh: печать base64 + инструкция «вставьте вручную в Settings → Secrets».
4. M2.4 Маскирование: если ушёл в Secret через `gh` — НЕ печатать саму строку; явное
   предупреждение «session = полный доступ к аккаунту, не передавайте никому».
5. M2.5 `go build ./...`, `go vet ./...`.

Далее M3…M8 по `docs/plan.md`.

**Важно:** репозиторий уже имеет `.git`. Коммитов ещё не было. Коммиты НЕ делать без
явной просьбы пользователя. `.gitignore` уже на месте — `session*.json` защищён.

## Финализированные имена env (Secrets)
- `SVODKA_TELEGRAM_API_ID`
- `SVODKA_TELEGRAM_API_HASH`
- `SVODKA_TELEGRAM_SESSION`  (base64 session из команды login)
- `SVODKA_ANTHROPIC_KEY`
- `SVODKA_CONFIG` (путь к config.yml, default `config.yml`)

## Ключевые подводные камни (проверено эмпирически)
- `ardanlabs/conf/v3/yaml` **игнорирует нулевые значения** (false/0/"") → НЕ используем
  его для config.yml; читаем файл через `gopkg.in/yaml.v3`. conf — только для env-секретов.
- conf `env:`-тег пишется **без префикса**: `conf:"env:TELEGRAM_API_ID"` →
  итоговое имя `SVODKA_TELEGRAM_API_ID` (префикс добавляется автоматически).
- Нет булева `dry_run`: безопасный режим = `target_chat: "me"` (Saved Messages).
- gotd Logger в Options оставляем `nil` (zap) — чтобы не текли данные в логи.
- В `svodka/main.go` логируем **только `user_id`** — username/имя считаем PII-смежным
  (логи Actions у форкнутого template могут быть видимы посторонним).

## Справочник API (выверено через `go doc`, чтобы не исследовать заново)

### gotd/td (v0.144.0)
```
telegram.NewClient(appID int, appHash string, opt telegram.Options) *Client
telegram.Options{ SessionStorage, Middlewares []Middleware, Logger *zap.Logger(nil),
                  UpdateHandler, NoUpdates bool }
(*Client).Run(ctx, func(ctx) error) error
(*Client).API() *tg.Client
(*Client).Auth() *auth.Client
(*Client).Self(ctx) (*tg.User, error)

session.StorageMemory{}:  StoreSession(ctx, []byte) error;  LoadSession(ctx)([]byte,error)
                          Bytes(to []byte)([]byte,error)

auth.NewFlow(UserAuthenticator, auth.SendCodeOptions) Flow
auth.UserAuthenticator iface: Phone(ctx)(string,error); Password(ctx)(string,error);
   AcceptTermsOfService(ctx, tg.HelpTermsOfService)error; SignUp(ctx)(UserInfo,error);
   Code(ctx, *tg.AuthSentCode)(string,error)
helpers: auth.Constant(phone,pass,code), auth.CodeOnly(phone,code), auth.Env(prefix,code)
(*auth.Client).Status(ctx) (*auth.Status{Authorized bool, User *tg.User}, error)
(*auth.Client).IfNecessary(ctx, Flow) error

floodwait.NewSimpleWaiter() *SimpleWaiter   // implements telegram.Middleware (Handle)
   .WithMaxRetries(uint) .WithMaxWait(time.Duration)
ratelimit.New(r rate.Limit, b int) *RateLimiter

peers.Options{Storage, Cache, Logger}.Build(api *tg.Client) *Manager
   // use &peers.InmemoryStorage{}, &peers.InmemoryCache{}
(*Manager).Resolve(ctx, from string)(Peer,error)   // @username/domain
(*Manager).Self(ctx)(User,error)
Peer iface: InputPeer() tg.InputPeerClass; VisibleName() string; ID() int64; Username()(string,bool)

query.Messages(raw).GetHistory(peer tg.InputPeerClass) *GetHistoryQueryBuilder
   .BatchSize(int).OffsetDate(int).OffsetID(int).Iter() *Iterator
   (*Iterator).Next(ctx) bool; .Value() Elem; .Err() error
   Elem{ Msg tg.NotEmptyMessage; Peer tg.InputPeerClass; Entities peer.Entities }
   Elem.Photo()/Document()/File()  // для медиа-плейсхолдеров
query.GetDialogs(raw) *GetDialogsQueryBuilder  // .BatchSize.ForEach(ctx, cb(ctx, dialogs.Elem)error)
   dialogs.Elem{ Peer tg.InputPeerClass; Last tg.NotEmptyMessage; Entities peer.Entities }

message.NewSender(raw) *Sender
   Sender.Self() *RequestBuilder; Sender.To(peer) *RequestBuilder; Sender.Resolve(from) *RequestBuilder
   (Builder).Text(ctx, msg string)(tg.UpdatesClass,error)
   (Builder).StyledText(ctx, ...StyledTextOption)(tg.UpdatesClass,error)

// сообщение: type-switch Elem.Msg.(*tg.Message); поля GetID()int, GetDate()int,
// GetMessage()string, GetFromID()(tg.PeerClass,bool). Автора брать из Elem.Entities
// (методы User/Chat/Channel — уточнить сигнатуры в следующей сессии при реализации).
```

### conf (v3.12.0)
```
conf.Parse("SVODKA", &cfg, parsers...) (help string, err)   // errors.Is(err, conf.ErrHelpWanted)
теги: env: (без префикса), default:, flag:, short:, required, mask, noprint, help:
встроить conf.Version для -v/--version
```

### Внутренние сигнатуры (написаны в M0/M1)
```
// config
config.Load() (*Config, error)         // полная, требует SVODKA_TELEGRAM_SESSION + ANTHROPIC + source_chats
config.LoadSecrets() (Secrets, error)  // только env, без session (подходит для login)
config.ErrHelp                          // sentinel при --help/--version, выходить с кодом 0
// telegram
telegram.LoadStorage(ctx, b64) (*session.StorageMemory, error)
telegram.Export(ctx, *session.StorageMemory) (string, error)
telegram.New(ctx, *config.Config) (*Client, error)
(*Client).Run(ctx, func(ctx, *tg.Client) error) error
(*Client).Auth() *auth.Client
(*Client).Storage() *session.StorageMemory
// logger
logger.New(service string) *Logger
(*Logger).Info/Warn/Error(ctx, msg, args...)
```

## Открытые вопросы к следующей сессии
- Проверить актуальный id модели Claude (`claude-sonnet-4-6` как default) при реализации `claude.go`.
- `peer.Entities` — уточнить методы lookup автора по FromID.
- Telegram limit 4096 символов на сообщение → сплит в `send.go`.
- В `gh secret set` — проверить, что Codespaces CLI имеет права `gh auth status` (обычно да);
  если нет — какие именно сообщения помогать показать.
