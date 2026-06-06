# svodka — Handoff (для продолжения в новой сессии)

Дата заморозки: 2026-06-06 (после закрытия M0–M6: подключение → логин → резолв/история →
LLM → дайджест → оркестрация+отправка). На очереди **M7** (GitHub-обвязка).

## Где читать контекст
- `docs/requirements.md` — что и зачем.
- `docs/architecture.md` — как устроено + ADR (ADR-7 тесты, ADR-8 резолв, ADR-9 Claude,
  ADR-10 дайджест, ADR-11 оркестрация).
- `docs/plan.md` — подзадачи со статусами. M0–M6 — `[x]`, M8.1 — `[~]` (готов раздел
  подключения в README), на очереди **M7** и **M8**.
- Auto-memory подхватится автоматически (профиль, стиль работы, ссылки, правило про комменты).

## Текущее состояние

**M0–M4 — `[x]`** (скаффолд/конфиг, telegram core + login, резолв+история, llm.Provider/Claude).
Детали — в git и в `docs/`. Ниже только то, что важно для M7.

**M5 (формирование дайджеста) — `[x]`, с тестами** (`business/digest/`):
- `digest.go`: `Build(ctx, llm.Provider, []telegram.ChatMessages, Options) (string, error)` —
  гибрид: < порога один вызов, ≥ порога map-reduce. `Options{OutputLang, Location *time.Location,
  Model, MaxTokens, ThresholdChars}`. Порог по символам (`useMapReduce`, rune count,
  `defaultThresholdChars=16000`). Пустой вход → `""`. Ошибки map оборачиваются по индексу чата.
- `serialize.go`: `serializeChat`/`serializeAll` — `## Title` + `[HH:MM] Author: text`, время
  абсолютное в `Location`.
- `prompt.go`: `singleSystem`/`mapSystem`/`reduceSystem` (+общий `formatRules`), параметр
  `output_lang` (ISO 639-1). `mapSystem` собирается один раз → байт-в-байт между map-вызовами
  (prompt cache, ADR-9).
- Тесты: сериализация (+timezone), `useMapReduce` (table), `Build` с фейк-`Provider`
  (single/map-reduce/пустой/пропуск пустого map-вывода/проброс ошибки/инвариант map-system).

**M6 (оркестрация + отправка) — `[x]`, с тестами:**
- `business/telegram/send.go`: `Send(ctx, api *tg.Client, r *Resolver, target, text string) error` —
  `me`→`Self()` (`isSelf`, case-insensitive), иначе `r.Resolve`→`sender.To(p.Input)`.
  Чистый `splitMessage(text, limit)` (режет по последнему `\n` в окне, иначе hard-cut),
  `maxMessageRunes=4096`. Тесты `splitMessage`/`isSelf`.
- `app/summarize/summarize.go`: `Run(ctx, api *tg.Client, Deps{Log, Cfg, Provider}) error` —
  внутри `client.Run`: `LoadLocation(Timezone)` → `windowStart(now,WindowHours)` → один
  `NewResolver` → `collect`(resolve+`FetchWindow`) → `digest.Build` → `Send`. Толерантен к
  сбою чата (warn по `chat_index` + skip), все упали → ошибка, LLM/`Send` → ошибка, пустой
  дайджест → не шлёт (ADR-11).
- `app/summarize/helpers.go`: чистые `windowStart`, `chatChars`, `runeLen`, тип `stats`. Тесты.
- `api/cmd/svodka/main.go`: smoke заменён реальным прогоном — после auth-check зовёт
  `summarize.Run(ctx, api, Deps{Provider: llm.NewClaude(cfg.Anthropic.Key)})`.

**README — `[~]` (M8.1, раздел подключения):** `README.md` (EN) + `README.ru.md`. Готов поток
template→api_id/hash→Codespaces `login`→session в Secret, таблица connection-секретов,
`SVODKA_`-префикс, privacy. Остальное (config→test→go live, IP/60-дней) — дописать в M8.

**Состояние сборки:** `go build ./...`, `go vet ./...`, `go test ./...`, `gofmt -l .` — всё зелёное.
**История ведётся по вехам** (коммиты `scaffold`→`digest`=M5); M6-M8 коммитятся отдельно.
**Коммиты делать только по явной просьбе пользователя.**

## M7 (GitHub-обвязка) — `[~]` (M7.1/M7.2 закрыты, M7.3 — UI+README)

Артефакты написаны, провалидированы (YAML/JSON valid, `go build/vet` зелёные, `gofmt` чист);
функциональный прогон — только Deployer'ом с личными кредами. Решения — ADR-12.
- M7.1 `.github/workflows/daily.yml` — `[x]`: cron `0 6 * * *` (06:00 UTC) + `workflow_dispatch`;
  `actions/checkout@v6` + `actions/setup-go@v6` (`go-version-file: go.mod`, кэш дефолтный);
  `go run ./api/cmd/svodka`; `permissions: contents: read`; `timeout-minutes: 10`; env — 4 секрета.
- M7.2 `.devcontainer/devcontainer.json` — `[x]`: `mcr.microsoft.com/devcontainers/go:1.25`
  + фича `ghcr.io/devcontainers/features/github-cli:1`.
- M7.3 — `[~]`: «Use this template» включает владелец в UI (Settings → Template repository);
  инструкцию Deployer'у дописать в README (M8.1).

## M8 (документация и финал) — `[x]`
- M8.1 — `[x]`: `README.md`+`README.ru.md` переписаны в единый deploy-поток (шаги 1-7) +
  справочные секции (таблица настроек, таблица 4 секретов, Schedule с UTC/60-дней/IP, privacy).
  WIP-баннер снят.
- M8.2 — `[x]`: `go build/vet/test ./...` + `gofmt -l .` зелёные.
- M8.3 — `[x]`: AC-1 пройден авто; AC-2..AC-5 (нужны личные креды) покрыты инструкцией в README.

## Статус проекта: M0–M8 закрыты (фичи готовы)
Остаётся только то, что **не код**: (1) владелец помечает публичный репо как template в UI
(Settings → Template repository) — M7.3; (2) функциональный прогон AC-2..AC-5 Deployer'ом с
кредами. Код по M5 закоммичен; M6-M8 коммитятся сейчас. Коммиты — только по явной просьбе.
Возможные будущие задачи (вне scope): `config.yml` вне репо (env→yaml content), другие LLM-провайдеры.

## Конфиг (поля Settings, дефолты)
`source_chats []string`, `target_chat="me"`, `window_hours=24`, `output_lang="ru"`,
`timezone="UTC"` (дефолт в коде; в `config.example.yml` — `Europe/Belgrade`),
`model="claude-sonnet-4-6"`, `max_output_tokens=2000`.
Секреты (env, префикс `SVODKA_`): `TELEGRAM_API_ID/API_HASH/SESSION`, `ANTHROPIC_KEY`.
`SVODKA_CONFIG` — **не секрет**, а путь к `config.yml` (default `config.yml`, см. `config.go`):
файл коммитится и правится в web-UI, workflow читает его с диска после checkout (ADR-12).

## Ключевые подводные камни
- gotd требует работать внутри `client.Run` (живой `*tg.Client`); `summarize.Run` исполняется
  в колбэке. Один `NewResolver` на запуск (общий кэш резолвов + dialog-скан, ADR-8).
- `claude-sonnet-4-6` — актуальный id (дефолт по цене); максимум — `claude-opus-4-8`. Модель
  конфигурируема, `Summarize` пробрасывает строку как есть.
- SDK `ContentBlockUnion.AsText()` читает из raw JSON → в тестах собирать `anthropic.Message`
  через `json.Unmarshal` (см. `claude_test.go`).
- В логах — только метрики/`user_id`/`chat_index`, не контент и не секреты (NFR-2). gotd
  Logger = nil.
- Безопасный дефолт публикации = `target_chat:"me"`; булева `dry_run` нет (ADR-4).
- Telegram limit 4096 на сообщение — сплит уже в `send.go` (`splitMessage`).
- GitHub cron — в UTC; scheduled workflow отключается после 60 дней без активности репо (C-3).
  IP раннеров меняется → Telegram может слать «новый вход» (C-4).
- Локальный прогон: `.env` через `set -a; source .env; set +a` (без `export` не виден дочернему
  процессу). `.env` уже в `.gitignore`.
- Тесты (ADR-7): юнитим только чистую логику; сетевые обёртки (gotd/SDK) и оркестратор не юнитим.
  Зависимости внедряем через интерфейсы (`llm.Provider`) и `Deps`.
- Комменты в коде/конфигах — на английском (публичный template); `docs/` и общение — на русском.

## Внутренние сигнатуры (актуальные)
```
// config
config.Load() (*Config, error); config.LoadSecrets() (Secrets, error); config.ErrHelp
config.Settings{SourceChats, TargetChat, WindowHours, OutputLang, Timezone, Model, MaxOutputTokens}
// telegram
telegram.New(ctx, *config.Config) (*Client, error)
(*Client).Run(ctx, func(ctx, *tg.Client) error) error; .Auth() *auth.Client; .Storage() *StorageMemory
telegram.NewResolver(api *tg.Client) *Resolver
(*Resolver).Resolve(ctx, ref string) (telegram.Peer, error)         // ErrInviteLink на invite
telegram.FetchWindow(ctx, api *tg.Client, p Peer, since time.Time) (ChatMessages, error)
telegram.Send(ctx, api *tg.Client, r *Resolver, target, text string) error   // me->Self, split>4096
telegram.Peer{Input tg.InputPeerClass; Title string}
telegram.Message{Author string; Time time.Time; Text string}
telegram.ChatMessages{Title string; Messages []Message}
// llm
llm.Provider interface { Summarize(ctx, Input) (string, error) }
llm.Input{System, User, Model string; MaxTokens int}
llm.NewClaude(apiKey string) *Claude
// digest
digest.Build(ctx, llm.Provider, []telegram.ChatMessages, digest.Options) (string, error)
digest.Options{OutputLang string; Location *time.Location; Model string; MaxTokens, ThresholdChars int}
// app
summarize.Run(ctx, api *tg.Client, summarize.Deps) error
summarize.Deps{Log *logger.Logger; Cfg *config.Config; Provider llm.Provider}
// logger
logger.New(service string) *Logger; (*Logger).Info/Warn/Error(ctx, msg, args...)
```

## Открытые вопросы к M8
- M7 закрыт (ADR-12); открытых развилок нет. Если захотим держать `config.yml` вне репо
  (source_chats как приватные данные) — нужна доработка кода (env→yaml content), это новая
  задача вне текущего scope.
- M8: дописать README (config→run workflow→go live, заметки про IP/60-дней), финальные
  `go build/vet/gofmt`, прогон чек-листа приёмки (AC-1 — авто; AC-2..AC-5 — инструкцией
  Deployer'у, нужны личные креды).
