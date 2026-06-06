# svodka — Handoff (для продолжения в новой сессии)

Дата заморозки: 2026-06-06 (после закрытия M0–M4 + раздел подключения в README).

## Где читать контекст
- `docs/requirements.md` — что и зачем.
- `docs/architecture.md` — как устроено + ADR (важные решения; ADR-7 — тесты, ADR-8 — резолв,
  ADR-9 — Claude через SDK).
- `docs/plan.md` — подзадачи со статусами. M0–M4 — `[x]`, M8.1 — `[~]` (готов раздел подключения),
  на очереди **M5**.
- Auto-memory подхватится автоматически (профиль, стиль работы, ссылки, правило про комменты).

## Текущее состояние

**M0 (скаффолд) — `[x]`** · **M1 (Telegram core + svodka main) — `[x]`** · **M2 (Login) — `[x]`, с тестами.**
Детали этих этапов — в git и в прошлых записях; ниже только то, что важно для M5.

**M3 (резолв пиров + чтение истории) — `[x]`, с тестами:**
- `business/telegram/resolve.go`:
  - `type Peer struct { Input tg.InputPeerClass; Title string }` — резолвленный source-чат.
  - `type Resolver` (один на запуск): `NewResolver(api *tg.Client) *Resolver`;
    `(*Resolver).Resolve(ctx, ref string) (Peer, error)`.
  - `classifyInput(raw)` (чистая): strip `@`/`t.me/`/scheme, отказ на invite-ссылки (`ErrInviteLink`),
    различает username/numeric. Числовой id → ленивый `ensureDialogs` (один проход
    `query.GetDialogs`, карта `TDLibPeerID → Peer`; формула: user→p, chat→-p, channel→-1e12-p).
  - `var ErrInviteLink error`.
- `business/telegram/history.go`:
  - `type Message struct { Author string; Time time.Time; Text string }` (Time — UTC).
  - `type ChatMessages struct { Title string; Messages []Message }` (хронология, старые→новые).
  - `FetchWindow(ctx, api *tg.Client, p Peer, since time.Time) (ChatMessages, error)` —
    `query.Messages(api).GetHistory(p.Input).BatchSize(100).Iter()`, стоп при `date < since`, `reverse`.
  - `normalize(msg tg.NotEmptyMessage, ent peer.Entities) (Message, bool)` (чистая): сервисные
    (`*tg.MessageService`) → skip; автор из `FromID`+`Entities` (пост канала → пусто); медиа →
    `mediaTag`/`documentTag` (`[photo]/[video]/[voice]/[sticker]/[gif]/[audio]/[file]/[poll]/[location]/…`,
    webpage без метки); пустые (без текста и медиа) → skip.
- Тесты: `resolve_test.go` (`classifyInput`, `tdlibID`), `history_test.go` (`mediaTag`, `normalize`, `reverse`).
  Сетевые `Resolve`/`FetchWindow`/scan не юнитим (ADR-7).

**M4 (LLM-провайдер Claude) — `[x]`, с тестом:**
- `business/llm/llm.go`: `type Provider interface { Summarize(ctx, Input) (string, error) }`;
  `type Input struct { System, User, Model string; MaxTokens int }`.
- `business/llm/claude.go`: `type Claude`; `NewClaude(apiKey string) *Claude`;
  `(*Claude).Summarize(ctx, Input) (string, error)` через `anthropic-sdk-go` (`client.Messages.New`).
  `cache_control` на system-блоке (окупается в map-reduce). `extractText(*anthropic.Message) string` —
  сбор `TextBlock` из `Content` (чистая, юнит-тест через `json.Unmarshal`, см. claude_test.go).
  Ретраи 429/5xx — внутри SDK; пустой content → ошибка.
- Зависимость: `github.com/anthropics/anthropic-sdk-go v1.47.0` (прямая). `option.WithAPIKey`.

**README — `[~]` (M8.1, раздел подключения):** `README.md` (EN, основной) + `README.ru.md` (взаимные
ссылки). Готово: template→api_id/hash→Codespaces `login`→session в Secret, таблица connection-секретов,
заметка про `SVODKA_`-префикс, privacy. Остальные разделы (config→test→go live, IP/60-дней) — после M5–M7.

**Состояние сборки:** `go build ./...`, `go vet ./...`, `go test ./...`, `gofmt -l .` — всё зелёное.
**Коммитов в репо ещё НЕ было. Коммиты НЕ делать без явной просьбы.**

## Следующий шаг — M5 (формирование дайджеста)

Подзадачи (см. `docs/plan.md` §M5):
- M5.1 `business/digest/digest.go` — system-промпт (язык `output_lang`, структура вывода).
- M5.2 Сериализация `[]ChatMessages` в компактный user-текст (по чатам, с автором/временем).
- M5.3 Оценка объёма → выбор стратегии: один вызов vs map-reduce.
- M5.4 Map: саммари по чату; Reduce: финальная склейка в дайджест.
- M5.5 Форматирование под Telegram (заголовки/маркеры, без хрупкого markdown).
- M5.6 `go build`/`vet`/`test`/`gofmt`.

**Развилки M5 обсудить ДО кода (через AskUserQuestion):**
- Стратегия объёма: всегда один вызов (просто, но рискует контекстом/качеством на больших днях)
  vs map-reduce по порогу (сложнее, дороже на токенах, устойчивее). Порог в токенах/символах —
  как считать (грубая оценка по длине vs `count_tokens`)? Для дешевизны — оценка по символам.
- Структура дайджеста: единый список по важности vs по чатам (заголовок чата → буллеты). `output_lang`.
- Формат времени в user-тексте: абсолютное в `timezone` vs относительное. Тайзмона из конфига.
- Что покрывать тестами (ADR-7): сериализация `[]ChatMessages`→user-текст и выбор стратегии по
  порогу — чистые, table-тест. Сам вызов `llm.Summarize` — за интерфейсом, мокаем `Provider` в тесте
  оркестратора (M6), но в M5 `digest.Build` принимает `Provider` параметром → тестируется с фейком.
- Промпт caching: `cache_control` на system уже стоит в `claude.go`; для map-reduce держать system
  байт-в-байт одинаковым между map-вызовами (один `Input.System`).

## Конфиг (поля Settings, дефолты)
`source_chats []string`, `target_chat="me"`, `window_hours=24`, `output_lang="ru"`,
`timezone="Europe/Belgrade"`, `model="claude-sonnet-4-6"`, `max_output_tokens=2000`.
Секреты (env, префикс `SVODKA_`): `TELEGRAM_API_ID/API_HASH/SESSION`, `ANTHROPIC_KEY`, `CONFIG`.

## Ключевые подводные камни
- `claude-sonnet-4-6` — актуальный id (есть в каталоге Anthropic); дефолт по цене для daily-задачи.
  Максимум — `claude-opus-4-8`. Модель конфигурируема, `Summarize` пробрасывает строку как есть.
- SDK `ContentBlockUnion.AsText()` читает из захваченного raw JSON, не из полей структуры → в тестах
  собирать `anthropic.Message` через `json.Unmarshal`, а не литералами.
- gotd Logger в Options = `nil`. В логах — только метрики/`user_id`, не контент.
- Безопасный дефолт публикации = `target_chat:"me"`; булева `dry_run` нет (ADR-4).
- Локальный прогон `login`: `.env` без `export` через `source` не виден дочернему процессу →
  `set -a; source .env; set +a`. `.env` уже в `.gitignore`.
- Тесты (ADR-7): юнитим только чистую логику; обёртки над gotd/SDK не юнитим. Внедряем зависимости
  через интерфейсы (`llm.Provider`) и параметры.
- Комменты в коде/конфигах — на английском (публичный template); `docs/` и общение — на русском.

## Внутренние сигнатуры (актуальные)
```
// config
config.Load() (*Config, error); config.LoadSecrets() (Secrets, error); config.ErrHelp
// telegram
telegram.New(ctx, *config.Config) (*Client, error)
(*Client).Run(ctx, func(ctx, *tg.Client) error) error; .Auth() *auth.Client; .Storage() *session.StorageMemory
telegram.NewResolver(api *tg.Client) *Resolver
(*Resolver).Resolve(ctx, ref string) (telegram.Peer, error)         // ErrInviteLink на invite
telegram.FetchWindow(ctx, api *tg.Client, p Peer, since time.Time) (ChatMessages, error)
telegram.Peer{Input tg.InputPeerClass; Title string}
telegram.Message{Author string; Time time.Time; Text string}
telegram.ChatMessages{Title string; Messages []Message}
// llm
llm.Provider interface { Summarize(ctx, Input) (string, error) }
llm.Input{System, User, Model string; MaxTokens int}
llm.NewClaude(apiKey string) *Claude
// logger
logger.New(service string) *Logger; (*Logger).Info/Warn/Error(ctx, msg, args...)
```

## Открытые вопросы к M5+
- Telegram limit 4096 символов на сообщение → сплит в `send.go` (M6.1).
- Перенос `business/telegram.ChatMessages`/`Message` в digest: digest импортирует пакет telegram
  (направление зависимостей ок: business←business на одном уровне? — telegram и digest оба business;
  чтобы не плодить связь, можно определить вход digest как срез своих типов или принять telegram-типы;
  решить при M5.2). Оркестратор M6 склеит resolve+fetch→digest→send.
