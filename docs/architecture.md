# svodka — Architecture Design

## 1. Обзор

`svodka` — короткоживущая CLI-задача на Go, запускаемая по расписанию в GitHub
Actions. За один прогон: подключается к Telegram под пользователем (MTProto), читает
историю заданных чатов за окно времени, делает саммари через Claude и публикует
дайджест. Никаких долгоживущих процессов и БД.

```
GitHub Actions (cron / workflow_dispatch)
        │
        ▼
  go run ./api/cmd/svodka
        │
        ├─(1) load config: conf(env secrets) + yaml(config.yml)   config/
        ├─(2) telegram.Client.Run(session из Secret, floodwait mw) business/telegram
        ├─(3) для каждого source-чата: resolve peer + getHistory   business/telegram
        │     за window_hours → []Message (нормализованные)
        ├─(4) digest.Build(messages) → промпт → llm.Summarize      business/digest, business/llm
        └─(5) send(target | "me"=Saved Messages), сплит >4096       business/telegram
```

## 2. Технологический стек

| Слой | Выбор | Причина |
|---|---|---|
| Язык | Go 1.25 | один статичный бинарник, обычный TCP, лучший MTProto-клиент |
| MTProto | `github.com/gotd/td` | зрелая чистая Go-библиотека |
| FLOOD_WAIT | `gotd/contrib/middleware/floodwait` (+ `ratelimit`) | авто-retry |
| Конфиг (секреты) | `github.com/ardanlabs/conf/v3` | env + `--help` + masking |
| Конфиг (файл) | `gopkg.in/yaml.v3` | корректное применение false/0 (см. ADR-3) |
| LLM | Claude (Anthropic Messages API) через `anthropic-sdk-go` | требование; за интерфейсом (см. ADR-9) |
| Логи | `log/slog` (обёртка в `foundation/logger`) | stdlib, структурно |
| Хостинг | GitHub Actions (cron + dispatch) | бесплатно, ноль инфраструктуры |
| Логин UX | GitHub Codespaces | браузерный терминал, без локального тулчейна |

## 3. Структура репозитория (Ardan-flavored, trimmed)

```
svodka/
  go.mod                       # module svodka
  Makefile                     # run, login, tidy, vet, build
  config.yml                   # пользовательский конфиг (правится в web-UI)
  config.example.yml           # документированный пример
  api/cmd/
    svodka/main.go             # ежедневная задача (entrypoint)
    login/main.go              # одноразовый интерактивный логин → session → gh secret set
  app/
    summarize/summarize.go     # use-case: оркестрация read → digest → send
  business/
    telegram/
      client.go                # сборка gotd Client (Options, middlewares)
      session.go               # session: base64 <-> StorageMemory (env in; экспорт в login)
      resolve.go               # резолв @username/ссылок/числовых id → peer
      history.go               # getHistory за окно + нормализация в []Message
      send.go                  # отправка (Self/peer), сплит >4096
    llm/
      llm.go                   # interface Provider + типы Input/Output
      claude.go                # Claude через net/http (prompt caching)
    digest/
      digest.go                # построение промптов (map/reduce), сборка текста
  foundation/
    logger/logger.go           # slog-обёртка, без логирования контента
  config/
    config.go                  # Config + Load(): conf(env) + yaml(file) + дефолты + валидация
  .github/workflows/daily.yml  # schedule cron + workflow_dispatch
  .devcontainer/devcontainer.json  # Codespaces: Go + gh
  docs/                        # эти документы
  README.md                    # инструкция Deployer'у
  LICENSE                      # MIT
```

Направление зависимостей: `foundation` ← `config`/`business` ← `app` ← `api/cmd`.
(Слои `business/{domain,sdk}` и `app/domain` из Ardan схлопнуты — на ~600 строк
логики три уровня были бы переусложнением.)

## 4. Компоненты

### 4.1 `config`
- `Config`: секреты (`Telegram.APIID/APIHash/Session`, `Anthropic.Key`) + content
  (`SourceChats`, `TargetChat`, `WindowHours`, `OutputLang`, `Timezone`, `Model`,
  `MaxOutputTokens`).
- `Load()`: (1) `conf.Parse("SVODKA", ...)` читает секреты из env, отдаёт `--help`;
  (2) `yaml.Unmarshal(config.yml)` в content-поля; (3) ручные дефолты для нулевых
  content-полей; (4) валидация (есть api_id/hash; для svodka — session, ключ Claude,
  непустой `source_chats`).
- Имена env: `SVODKA_TELEGRAM_API_ID`, `SVODKA_TELEGRAM_API_HASH`,
  `SVODKA_TELEGRAM_SESSION`, `SVODKA_ANTHROPIC_KEY` (через `conf:"env:..."` без префикса).

### 4.2 `business/telegram`
- `client.go`: `telegram.NewClient(apiID, apiHash, Options{SessionStorage, Middlewares})`;
  middlewares = `floodwait.NewSimpleWaiter()` (+ опц. `ratelimit`); gotd Logger = nil
  (чтобы не текли данные). Работа внутри `client.Run(ctx, fn)`.
- `session.go`: base64(JSON session) → `session.StorageMemory.StoreSession`; экспорт
  для login через `LoadSession`. Read-only в рантайме (auth key стабилен).
- `resolve.go`: `peers.Options{Storage:&InmemoryStorage,Cache:&InmemoryCache}.Build(api)`;
  `@username`/`t.me` → `manager.Resolve`; числовой id → фолбэк сканом `query.GetDialogs`.
  Возвращает `tg.InputPeerClass` + отображаемое имя.
- `history.go`: `query.Messages(api).GetHistory(peer).BatchSize(100).Iter()`; стоп при
  `date < now-window`; type-switch на `*tg.Message`; имя автора из `Elem.Entities`.
- `send.go`: `message.NewSender(api)`; `Self()` для `me`, иначе `To(peer)`/`Resolve`;
  `Text(ctx, ...)`; сплит длинного текста по 4096.

### 4.3 `business/llm`
- `Provider interface { Summarize(ctx, Input) (string, error) }`, `Input{System,User,Model,MaxTokens}`.
- `claude.go`: официальный `anthropic-sdk-go` (`client.Messages.New`); модель/`MaxTokens` —
  из конфига; **prompt caching** (`cache_control` на system-блоке — окупается в map-reduce);
  сбор текста из `Message.Content` (type-switch на `TextBlock`); пустой ответ → ошибка.
  Сетевые ошибки/ретраи 429/5xx — внутри SDK.

### 4.4 `business/digest`
- `Build(ctx, p llm.Provider, chats []telegram.ChatMessages, opts Options) (string, error)`
  (`Options{OutputLang, Location *time.Location, Model, MaxTokens, ThresholdChars}`):
  - объём мал (< `ThresholdChars` символов) → один вызов (system: язык+структура, user: все чаты);
  - велик → map (саммари по чату, общий byte-identical system) + reduce (склейка) — несколько вызовов;
  - пустой вход/пустые чаты → `""` (оркестратор пропускает отправку); ошибки оборачиваются по индексу чата.
  - форматирование под Telegram задано промптами (`formatRules`: plain text, `- ` буллеты, без `#`/таблиц).
  - детали и trade-offs — ADR-10.

### 4.5 `app/summarize`
- `Run(ctx, api *tg.Client, Deps{Log, Cfg, Provider})` — исполняется внутри `client.Run`;
  выполняет шаги 3-5 (resolve+fetch всех чатов → `digest.Build` → `Send`), логирует метрики
  (чатов ok/failed, сообщений, символов, тайминги) и возвращает `error`. Толерантен к сбою
  отдельного чата, жёсток к LLM/`Send`, пустой дайджест не отправляет (ADR-11). Чистые
  хелперы — `helpers.go`.

### 4.6 `api/cmd/*`
- `svodka/main.go`: `signal.NotifyContext`; `config.Load`; `telegram.Client.Run`
  → `summarize.Run`; коды выхода и понятные ошибки.
- `login/main.go`: интерактивный `auth.UserAuthenticator` (stdin); по успеху экспорт
  session → base64 → `gh secret set SVODKA_TELEGRAM_SESSION` (fallback: печать + инструкция).

## 5. Модель конфигурации

```yaml
# config.yml — только не-секретные настройки
source_chats:
  - "@chat_one"
  - "@chat_two"
target_chat: "me"        # me = Saved Messages (предпросмотр); "@group" = боевой
window_hours: 24
output_lang: "ru"
timezone: "Europe/Belgrade"
model: "claude-sonnet-4-6"
max_output_tokens: 2000
```

Секреты — только в GitHub Actions Secrets (env), не в файле:
`SVODKA_TELEGRAM_API_ID`, `SVODKA_TELEGRAM_API_HASH`, `SVODKA_TELEGRAM_SESSION`,
`SVODKA_ANTHROPIC_KEY`. cron-расписание — в `daily.yml` (требование GitHub).

## 6. Развёртывание (поток Deployer'а)

1. «Use this template» → создать **private** репозиторий.
2. Получить `api_id`/`api_hash` на my.telegram.org → задать Secrets.
3. Открыть Codespace → `go run ./api/cmd/login` → ввести телефон/код/2FA → session
   попадает в Secret.
4. Задать `ANTHROPIC_API_KEY` Secret; отредактировать `config.yml` (чаты, target).
5. Включить Actions; нажать **Run workflow** для теста (target=me → Saved Messages).
6. Сменить `target_chat` на группу; при необходимости поправить cron.

## 7. Безопасность и приватность

- session/ключи — только Secrets; в коде/логах/выводе их нет.
- gotd Logger отключён; наш логгер пишет только метрики, не контент.
- Публичный репо = публичные логи Actions → деплой-инстанс делается private.
- Безопасный дефолт публикации — «Saved Messages».

## 8. Architecture Decision Records (кратко)

- **ADR-1. MTProto, не Bot API.** Бот не видит историю чужих чатов → нужен userbot.
- **ADR-2. GitHub Actions вместо Cloudflare Workers.** На Workers нет обычного TCP →
  пришлось бы писать кастомный MTProto-транспорт. Обычный рантайм убирает риск; cron
  и Secrets уже встроены, бесплатно.
- **ADR-3. yaml.v3 для файла, conf для секретов.** `ardanlabs/conf/v3/yaml` игнорирует
  нулевые значения (`false`/`0`/`""`) → нельзя выключить флаг из yaml. Поэтому файл
  читаем yaml.v3, а conf оставляем для env-секретов (masking, `--help`).
- **ADR-4. Нет булева dry_run.** Безопасный режим выражается через `target_chat: me`
  (Saved Messages). Избегаем бага ADR-3 и упрощаем UX.
- **ADR-5. Без БД.** Персистим только session (Secret). Пиры резолвим каждый запуск,
  апдейты не слушаем, сообщения не храним.
- **ADR-6. Логин в Codespaces.** Интерактивный код нельзя получить «в один клик»;
  Codespaces даёт браузерный терминал без локального тулчейна.
- **ADR-9. Claude через официальный `anthropic-sdk-go`, не `net/http`.** Изначально
  планировался ручной `net/http` ради минимума зависимостей, но официальный Go-SDK даёт
  типы, константы моделей и авто-ретраи 429/5xx (которые иначе пришлось бы писать руками),
  а тяжёлый граф зависимостей и так уже есть из-за `gotd`. Провайдер спрятан за интерфейсом
  `llm.Provider`, поэтому выбор транспорта локализован в `claude.go`. Модель и `max_tokens`
  берём из конфига; `cache_control` на system-блоке окупается только в map-reduce (M5).
- **ADR-11. Оркестрация: тонкий use-case, толерантность к чатам, жёсткость к
  публикации.** `app/summarize.Run` исполняется внутри `client.Run` (gotd требует живой
  `*tg.Client`), зовёт конкретные `Resolve`/`FetchWindow`/`Send`/`digest.Build` напрямую —
  слой сетевой, не юнитим (ADR-7); чистые хелперы (`windowStart`, `chatChars`) вынесены и
  покрыты тестами. Политика сбоев: сбой resolve/fetch одного чата → warn (по индексу, не по
  ссылке) + skip + continue (ADR-8); если упали **все** source-чаты → ошибка/exit 1 (сломан
  конфиг или сессия); сбой LLM или `Send` → ошибка/exit 1. Пустой дайджест (нет сообщений за
  окно, `Build`→`""`) — успех без отправки (тихие дни не шумят в целевом чате). Метрики в
  логах — только счётчики и тайминги (NFR-2). Зависимости (`llm.Provider`, `Cfg`, `Log`)
  внедряются через `Deps`; `Timezone→*time.Location` парсит оркестратор, в `digest` уходит
  готовая зона.
- **ADR-10. Дайджест: гибридная стратегия объёма, структура по чатам, абсолютное
  время, вход — `telegram.ChatMessages`.** (1) *Объём:* ниже порога — один вызов; на
  уровне/выше — map (саммари по чату) + reduce (склейка), чтобы не упираться в контекст
  (FR-7). Порог считаем грубо по числу символов (`utf8.RuneCountInString`, ~4 символа/
  токен, дефолт 16000) — без отдельного сетевого `count_tokens`: дёшево, без лишней
  латентности, тестируемо чистой функцией. (2) *Структура:* по чатам (заголовок → буллеты:
  темы/решения/упоминания владельца/ссылки, FR-9) — естественно ложится на map=один чат.
  (3) *Время* в user-тексте — абсолютное `HH:MM` в `Timezone`, не относительное: стабильно
  между прогонами → дружелюбно к prompt-кэшу. (4) *Границу типов* провели по входу: `Build`
  принимает `[]telegram.ChatMessages` напрямую (telegram и digest на одном business-уровне),
  без промежуточных DTO и маппинга — на ~600 строк логики развязка пакетов не окупается.
  `map`-system собирается один раз и переиспользуется байт-в-байт между map-вызовами, чтобы
  окупился `cache_control` (ADR-9). `llm.Provider` внедряется параметром → тест с фейком
  (ADR-7); `Timezone→*time.Location` парсит оркестратор, в `digest` приходит готовая зона.
- **ADR-12. GitHub-обвязка (M7).** Workflow `daily.yml`: `schedule` `0 6 * * *`
  (06:00 UTC — GitHub cron всегда в UTC; пересчёт из локали — в inline-комментарии) +
  `workflow_dispatch`. `permissions: contents: read` (least privilege),
  `timeout-minutes: 10` против зависших прогонов. Версия Go — `go-version-file: go.mod`
  (единый источник правды), кэш модулей `actions/setup-go` — дефолтный (on, ключ по
  `go.sum`). Запуск `go run ./api/cmd/svodka`; ненулевой код выхода валит step → job
  красный (уведомления вне scope v1). Версии экшенов проверены до зашивания:
  `actions/checkout@v6`, `actions/setup-go@v6`. В env job — только 4 секрета
  (`SVODKA_TELEGRAM_API_ID/API_HASH/SESSION`, `SVODKA_ANTHROPIC_KEY`). **`SVODKA_CONFIG`
  — не секрет**, а путь к `config.yml` (default `config.yml`, см. `config.go`): файл
  коммитится в (приватный) инстанс и правится в web-UI (FR-19), workflow читает его с
  диска после checkout — отдельного content-секрета не нужно. devcontainer:
  `mcr.microsoft.com/devcontainers/go:1.25` (под `go.mod`) + фича
  `ghcr.io/devcontainers/features/github-cli:1` (gh нужен `login` для `gh secret set`,
  ADR-6). M7.3 «template» — действие владельца в UI (Settings → Template repository);
  инструкция — в README (M8). **Локальная разработка Maintainer'а:** committed
  `config.yml` держим безопасным плейсхолдером (`source_chats: []`); реальные чаты — в
  gitignored `config.local.yml` (запуск `SVODKA_CONFIG=config.local.yml`/`--config`).
  `.gitignore` ловит `config.*.yml` с исключением `!config.example.yml`; сам `config.yml`
  паттерн не задевает и остаётся в git — он нужен «Use this template» (копируются только
  закоммиченные файлы) и для чтения workflow с диска после checkout.
- **ADR-8. Резолв пиров: имя/ссылка → manager.Resolve; числовой id → ленивый скан
  диалогов.** Голый числовой id нельзя резолвить через MTProto без `access_hash`,
  которого нет в конфиге. При первом числовом id один раз сканируем `query.GetDialogs`
  (его `Elem.Peer` несёт `access_hash`) и строим карту `TDLibPeerID → peer`, дальше
  переиспользуем. Ключ — TDLib-id (то, что показывают клиенты, напр. `-100…`). Invite-
  ссылки (`t.me/+hash`, `joinchat/…`) отклоняем ошибкой: их резолв через `ImportInvite`
  означал бы вступление в чат — нежелательный сайд-эффект ежедневной задачи. Сбой резолва
  одного чата не валит прогон: логируем warning и продолжаем с остальными (устойчиво для
  cron). `peers.Manager` строим один раз на запуск (общий кэш резолвов).
- **ADR-7. Юнит-тесты только на чистую логику.** Тестируем код без сети и кредов:
  разбор/дефолты/валидацию конфига, `termAuth` (парсинг stdin, отказ в SignUp),
  нормализацию сообщений, сборку дайджеста, сплит >4096. Тонкие обёртки над
  gotd/HTTP (требуют живого Telegram/Claude) не юнитим — их покрывают `go build`,
  `go vet`, ревью и ручной прогон Deployer'ом (`target_chat: me`). Зависимости для
  тестируемости внедряем через интерфейсы/`io.Reader` (для tty-ветки `term.ReadPassword`
  в тестах используем `os.Pipe`, где read-конец не терминал → fallback-путь).
