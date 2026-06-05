# svodka — Handoff (для продолжения в новой сессии)

Дата заморозки: 2026-06-05 (после закрытия M0, M1, M2).

## Где читать контекст
- `docs/requirements.md` — что и зачем.
- `docs/architecture.md` — как устроено + ADR (важные решения; ADR-7 — про тесты).
- `docs/plan.md` — подзадачи со статусами (чекбоксы). M0/M1/M2 — `[x]`, M3 — на очереди.
- Auto-memory подхватится автоматически (профиль, стиль работы, ссылки, правило про комменты).

## Текущее состояние

**M0 (скаффолд) — `[x]`:**
- `go.mod` (`module svodka`, Go 1.25). Прямые: `gotd/td v0.144.0`, `gotd/contrib v0.21.1`,
  `ardanlabs/conf/v3 v3.12.0`, `gopkg.in/yaml.v3`, `golang.org/x/term` (добавлен в M2).
- `foundation/logger/logger.go` — slog-обёртка (Info/Warn/Error с ctx), в stderr, без контента.
- `config/config.go` — `Secrets` (conf/env) + `Settings` (yaml.v3) + `Load`/`LoadSecrets`.
- `config.yml`, `config.example.yml`. `.gitignore` (англ. комменты), `LICENSE` (MIT), `Makefile`.

**M1 (Telegram core + svodka main) — `[x]`:**
- `business/telegram/session.go` — `LoadStorage(ctx, b64)` / `Export(ctx, s) (b64, error)`.
- `business/telegram/client.go` — `Client{New, Run, Auth, Storage}`; Options: SessionStorage,
  Middlewares=[floodwait], Logger=nil.
- `api/cmd/svodka/main.go` — smoke: signal → `config.Load` → `client.Run` → `Auth().Status` →
  лог `user_id`.

**M2 (Login-команда, Codespaces) — `[x]`, с тестами:**
- `business/telegram/term_auth.go`:
  - приватный `termAuth` реализует `auth.UserAuthenticator` (Phone/Code/Password из stdin,
    промпты в stderr, значения не логируются; пароль — `term.ReadPassword` с fallback на
    bufio при не-tty; `SignUp`→ошибка «нет аккаунта»; `AcceptTermsOfService`→nil).
  - экспортируемая фабрика `TermAuth(in *os.File, out io.Writer) auth.UserAuthenticator`
    (возвращает интерфейс — как `auth.Constant/CodeOnly/Env` у gotd; тип приватный).
- `business/telegram/term_auth_test.go` — юниты через `os.Pipe` (read-конец = не tty →
  fallback-ветка пароля): Phone/Code/Password, trim, EOF, flow, SignUp-отказ, AcceptTOS.
- `api/cmd/login/main.go` — `config.LoadSecrets` → `&config.Config{Secrets}` с пустым Session →
  `telegram.New` → `client.Run` → `Auth().IfNecessary(NewFlow(TermAuth(os.Stdin, os.Stderr),
  SendCodeOptions{}))` → Status → лог `user_id` → `Export` → `storeSession`.
  - `storeSession` → `persistSession(stdout, stderr, setter, b64)` (чистое ядро) + `ghSecretSet`
    (тонкая `exec`-обёртка). gh есть → `gh secret set SVODKA_TELEGRAM_SESSION` (stdin=b64),
    при успехе строка НЕ печатается; gh нет/упал → `printManual` (warning в stderr + строка в stdout).
- `api/cmd/login/main_test.go` — тест инварианта маскирования (ADR-7): успех→нет утечки в
  stdout/stderr; ошибка setter→fallback+warning+текст ошибки; nil setter→fallback.

**Гигиена репо (M2-побочное):** `.claude/settings.local.json` снят с отслеживания
(`git rm --cached`, файл на диске остался) и добавлен в `.gitignore`. Комменты в `.gitignore`
переведены на английский (правило: артефакты публичного template — англ. комменты, `docs/` — рус.).

**Состояние сборки:** `go build ./...`, `go vet ./...`, `go test ./...`, `gofmt -l .` — всё зелёное.
Коммитов в репо ещё НЕ было. Коммиты НЕ делать без явной просьбы.

**Функциональный прогон login** (ввод телефона/кода в Codespace, реальное создание секрета) —
на стороне Deployer'а: нужны его api_id/api_hash и `gh auth`. Со стороны кода всё собрано.

## Следующий шаг — M3 (резолв пиров + чтение истории)

Подзадачи (см. `docs/plan.md` §M3):
- M3.1 `business/telegram/resolve.go` — `peers.Manager`; `Resolve(s)` с нормализацией
  (`@`, `t.me/`), `manager.Resolve` для юзернеймов/доменов.
- M3.2 Фолбэк числовых id: один раз карта id→InputPeer через `query.GetDialogs(...).ForEach`.
- M3.3 `business/telegram/history.go` — `FetchWindow(peer, since time.Time) ([]Message)`:
  итератор `GetHistory`, стоп по `date < since`.
- M3.4 Нормализация: type-switch `*tg.Message`; автор из `Elem.Entities`; текст; медиа→плейсхолдер;
  пропуск сервисных.
- M3.5 Тип `Message{ChatTitle, Author, Time, Text}` + группировка `ChatMessages`.
- M3.6 `go build`/`vet`/`test`/`gofmt`.

**Развилки M3 обсудить ДО кода (через AskUserQuestion):**
- Формат фолбэка числовых id (ленивый скан всех диалогов vs ошибка с просьбой использовать @username).
- Структура `Message`/`ChatMessages` (поля, время как `time.Time` vs unix int).
- Обработка медиа (единый плейсхолдер `[photo]`/`[document]` vs детальнее) и сервисных сообщений.
- Что тестировать по ADR-7: нормализация `*tg.Message`→`Message` — чистая, тестируемо table-тестом
  без сети; сам `GetHistory`/resolve — сетевые, не юнитим.

## Финализированные имена env (Secrets)
- `SVODKA_TELEGRAM_API_ID`, `SVODKA_TELEGRAM_API_HASH`, `SVODKA_TELEGRAM_SESSION` (base64 из login),
  `SVODKA_ANTHROPIC_KEY`, `SVODKA_CONFIG` (default `config.yml`).

## Ключевые подводные камни (проверено эмпирически)
- `ardanlabs/conf/v3/yaml` игнорирует нулевые значения → config.yml читаем `gopkg.in/yaml.v3`;
  conf — только env-секреты. conf `env:`-тег пишется БЕЗ префикса (SVODKA добавляется сам).
- Нет булева `dry_run`: безопасный режим = `target_chat: "me"` (Saved Messages).
- gotd Logger в Options = `nil` (чтобы не текли данные). В логах — только `user_id`/метрики, не контент.
- Тесты (ADR-7): юнитим только чистую логику без сети/кредов; обёртки над gotd/HTTP/exec — не юнитим
  (build+vet+review+ручной прогон). Для tty-ветки в тестах — `os.Pipe` (read-конец не tty → fallback).
- Комменты в коде/конфигах — на английском (публичный template); `docs/` и общение — на русском.

## Справочник API (выверено через `go doc`)

### gotd/td (v0.144.0)
```
telegram.NewClient(appID int, appHash string, opt telegram.Options) *Client
telegram.Options{ SessionStorage, Middlewares []Middleware, Logger *zap.Logger(nil), NoUpdates bool }
(*Client).Run(ctx, func(ctx) error) error;  .API() *tg.Client;  .Auth() *auth.Client;  .Self(ctx)(*tg.User,error)

session.StorageMemory{}:  StoreSession(ctx,[]byte)error; LoadSession(ctx)([]byte,error); Bytes(to)([]byte,error)

auth.NewFlow(UserAuthenticator, auth.SendCodeOptions) Flow
auth.UserAuthenticator: Phone(ctx)(string,error); Password(ctx)(string,error);
   AcceptTermsOfService(ctx, tg.HelpTermsOfService)error; SignUp(ctx)(UserInfo,error);
   Code(ctx,*tg.AuthSentCode)(string,error)
auth.SendCodeOptions{AllowFlashCall,CurrentNumber,AllowAppHash bool}
auth.UserInfo{FirstName,LastName string}
helpers: auth.Constant/CodeOnly/Env (все возвращают UserAuthenticator)
(*auth.Client).Status(ctx)(*auth.Status{Authorized bool, User *tg.User},error); .IfNecessary(ctx,Flow)error

floodwait.NewSimpleWaiter() *SimpleWaiter  // telegram.Middleware; .WithMaxRetries(uint).WithMaxWait(dur)
ratelimit.New(r rate.Limit, b int) *RateLimiter

peers.Options{Storage,Cache,Logger}.Build(api *tg.Client) *Manager
   // &peers.InmemoryStorage{}, &peers.InmemoryCache{}
(*Manager).Resolve(ctx, from string)(Peer,error)   // @username/domain
(*Manager).Self(ctx)(User,error)
Peer iface: InputPeer() tg.InputPeerClass; VisibleName() string; ID() int64; Username()(string,bool)

query.Messages(raw).GetHistory(peer tg.InputPeerClass) *GetHistoryQueryBuilder
   .BatchSize(int).OffsetDate(int).OffsetID(int).Iter() *Iterator
   (*Iterator).Next(ctx) bool; .Value() Elem; .Err() error
   Elem{ Msg tg.NotEmptyMessage; Peer tg.InputPeerClass; Entities peer.Entities }
   Elem.Photo()/Document()/File()  // медиа-плейсхолдеры
query.GetDialogs(raw) *GetDialogsQueryBuilder  // .BatchSize.ForEach(ctx, cb(ctx, dialogs.Elem)error)
   dialogs.Elem{ Peer tg.InputPeerClass; Last tg.NotEmptyMessage; Entities peer.Entities }

message.NewSender(raw) *Sender
   Sender.Self()/.To(peer)/.Resolve(from) *RequestBuilder
   (Builder).Text(ctx, msg string)(tg.UpdatesClass,error); .StyledText(ctx, ...StyledTextOption)(...)

// сообщение: type-switch Elem.Msg.(*tg.Message); поля GetID()int, GetDate()int,
// GetMessage()string, GetFromID()(tg.PeerClass,bool). Автора брать из Elem.Entities
// (методы User/Chat/Channel — уточнить сигнатуры при реализации M3.4).
```

### conf (v3.12.0)
```
conf.Parse("SVODKA", &cfg, parsers...) (help string, err)   // errors.Is(err, conf.ErrHelpWanted)
теги: env:(без префикса), default:, flag:, short:, required, mask, noprint, help:; conf.Version для -v
```

### Внутренние сигнатуры
```
config.Load() (*Config, error)         // полная: session+anthropic+source_chats
config.LoadSecrets() (Secrets, error)  // только env, без session (для login)
config.ErrHelp                          // sentinel при --help/--version, выходить с кодом 0
telegram.LoadStorage(ctx, b64) (*session.StorageMemory, error)
telegram.Export(ctx, *session.StorageMemory) (string, error)
telegram.New(ctx, *config.Config) (*Client, error)
telegram.TermAuth(in *os.File, out io.Writer) auth.UserAuthenticator
(*Client).Run(ctx, func(ctx, *tg.Client) error) error; .Auth() *auth.Client; .Storage() *session.StorageMemory
logger.New(service string) *Logger; (*Logger).Info/Warn/Error(ctx, msg, args...)
```

## Открытые вопросы к M3+
- `peer.Entities` — уточнить методы lookup автора по FromID (M3.4).
- Telegram limit 4096 символов на сообщение → сплит в `send.go` (M6.1).
- Актуальный id модели Claude (`claude-sonnet-4-6` как default) — проверить при `claude.go` (M4).
- В `gh secret set` — проверить права в Codespaces (`gh auth status`); функциональный тест login.
