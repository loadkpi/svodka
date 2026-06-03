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
| LLM | Claude (Anthropic Messages API) через `net/http` | требование; за интерфейсом |
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
- `claude.go`: POST `https://api.anthropic.com/v1/messages`, заголовки `x-api-key`,
  `anthropic-version: 2023-06-01`; **prompt caching** (`cache_control` на system-блоке);
  парсинг `content[0].text`; обработка ошибок/кодов.

### 4.4 `business/digest`
- `Build(chats []ChatMessages, settings) string`:
  - если объём мал → один вызов (system: язык+структура, user: все сообщения);
  - если велик → map (саммари по чату) + reduce (склейка) — несколько вызовов;
  - финальное форматирование под Telegram (заголовки, маркеры, без хрупкого markdown).

### 4.5 `app/summarize`
- Оркестратор: получает `config`, `telegram`, `llm`; выполняет шаги 3-5; возвращает
  метрики (чатов, сообщений, длина дайджеста) для лога.

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
