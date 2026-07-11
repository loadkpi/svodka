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

# Опционально: несколько дайджестов за прогон (разные источники → разные target).
# Если задан `routes`, верхнеуровневые source_chats/target_chat игнорируются;
# остальные настройки (окно, язык, модель, ...) — глобальные на все маршруты (ADR-19).
# routes:
#   - target_chat: "@team_one"
#     source_chats: ["@a", "@b"]
#   - target_chat: "@team_two"
#     source_chats: ["@c"]
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
- **ADR-13. `llm.Provider` возвращает `Result` (токены + truncated), не голую строку (M10).**
  Контракт сменён на `Summarize(ctx, Input) (Result, error)`, где
  `Result{Text; InputTokens, OutputTokens int; Truncated bool}`. `claude.go` заполняет токены
  из `resp.Usage` и `Truncated = resp.StopReason == max_tokens`. `digest.Build` суммирует
  `Usage` по всем вызовам (single или N map + reduce; `Truncated` = OR) и возвращает
  `(string, Usage, error)`. `summarize.Run` логирует `tokens_in/tokens_out` и шлёт WARN при
  обрыве. **Зачем:** молчаливый обрыв на `max_output_tokens` был невидим в логах (модель
  останавливается на полуслове, а мы возвращали усечённый текст как валидный) — теперь это
  видно, плюс появилась видимость стоимости. Выбран вариант «сменить сигнатуру» (а не добавлять
  отдельный метод): один источник результата, фейк-`Provider` в тестах тривиально обновляется.
  В лог идут только счётчики/флаг, без контента (NFR-2).
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
- **ADR-14. CLI-оверрайды Settings (M11): pointer-флаги в той же conf-модели,
  приоритет flag > yaml > default.** Разовый локальный запуск может переопределить любую
  скалярную настройку флагом (`--window-hours`, `--target-chat`, `--model`,
  `--output-lang`, `--timezone`, `--max-output-tokens`, `--extra-instructions`) и
  `--source-chats` (CSV); в Actions по-прежнему правит только `config.yml`. **Развилка
  «флаг задан явно» (ADR-3):** нельзя путать «не передан» с нулём/пустой строкой —
  иначе оверрайд `--max-output-tokens 0` неотличим от отсутствия флага. Решение —
  **pointer-поля** (`*int`/`*string`/`*[]string`) в структуре `Overrides`: conf
  (v3.12.0, проверено пробой) оставляет их `nil`, если флаг/env не переданы, и заполняет
  при явной передаче — включая явные `0`/`""`. `applyOverrides` присваивает только non-nil
  поверх yaml+дефолтов. **Развилка «сосуществование с conf»:** conf уже парсит флаги
  секретов на `os.Args` и падает на неизвестных — два парсера нельзя. Решение — **расширить
  ту же conf-модель**: `Load()` парсит `struct{Secrets; Overrides}` одним проходом (флаги
  видны в общем `--help`), а `LoadSecrets()` (login) парсит только `Secrets` — чтобы в
  справке логина не светились настройки svodka. **CSV для `source_chats`:** проба показала,
  что conf НЕ режет флаг-слайс по запятой (`--source-chats '@a,@b'` → один элемент), поэтому
  плоско разбиваем сами чистой `splitChats` (trim, пустые отбрасываются). Тестируем
  `applyOverrides`/`splitChats` как чистые функции (ADR-7); сам conf-парс не юнитим. Имена
  флагов — kebab; env-двойники conf генерирует автоматически (безвредный бонус для локала).
- **ADR-15. Бэклинки на сообщения (M13).** К пунктам дайджеста добавляем ссылку на
  первоисточник (расширяет FR-9). **Формат — только приватный `https://t.me/c/<rawID>/<msgID>`**
  (вариант A1): строим лишь для каналов/супергрупп (`*tg.InputPeerChannel` несёт raw
  `ChannelID`); user-чаты и обычные группы адресуемой per-message формы не имеют → ссылку
  молча опускаем (выпадает само из A1, отдельного кода нет). Публичную `t.me/<user>/<msg>`
  не делаем — экономим поля в `Peer`. **Вставка — гибрид (B3):** `serialize` детерминированно
  дописывает голый URL в конец каждой строки сообщения через чистую `linkFor(channelID, msgID)`,
  а модель привязывает его к буллетам; инструкция в `formatRules` — **обязательная** («каждый
  буллет по сообщениям со ссылками ДОЛЖЕН заканчиваться verbatim-ссылкой самого релевантного
  источника; не выдумывай и не меняй»). Мягкая формулировка («keep the relevant link») не
  работала: на живой проверке haiku выкидывал все ссылки. Так нет битых/галлюцинированных
  ссылок (источник — наш код), а выбор релевантного источника — на модели. **Красная линия prompt-cache (ADR-9/10):** URL живёт только
  в user-тексте, никогда в system-блоке — `mapSystem(lang)` остаётся байт-идентичным между
  map-вызовами. Правка общего `formatRules` затрагивает и `mapSystem` — это разовое изменение
  кода, инвариант (байт-идентичность *в пределах прогона*) цел. **Формат под Telegram — голый
  URL** (C1): Telegram автолинкует, markdown не нужен (`formatRules` его и так запрещает, `Send`
  шлёт plain text). **Опциональность:** флаг `backlinks` в `config.yml` (default `true`) +
  CLI-оверрайд `--backlinks` по образцу ADR-14; `Settings.Backlinks` — `*bool`, чтобы отличить
  отсутствие ключа (→ true) от явного `backlinks: false` (логика ADR-3). `linkFor` живёт в
  `business/digest` (presentation; digest уже завязан на telegram-типы по ADR-10) и покрыт
  табличным тестом (ADR-7). **Приватная ссылка открывается только участником чата** —
  задокументировано в README и комментарии конфига.
- **ADR-19. Мультимаршрутная рассылка (fan-out): разные источники → разные target.**
  За прогон можно построить несколько дайджестов, каждый из своего набора `source_chats`
  в свой `target_chat`. **Схема — опциональный блок `routes`** (список `{source_chats,
  target_chat}`) в `config.yml`. Если `routes` пуст — поведение прежнее: верхнеуровневые
  `source_chats`/`target_chat` образуют один синтетический маршрут (полная обратная
  совместимость, дефолт = один дайджест). Если `routes` задан — он главный, верхнеуровневые
  `source_chats`/`target_chat` игнорируются. **Объём маршрута — только source+target**
  (вариант A, выбран пользователем): прочие настройки (`window_hours`, `output_lang`,
  `timezone`, `model`, `max_output_tokens`, `extra_instructions`, `backlinks`) остаются
  **глобальными** на все маршруты — меньше кода/тестов, проще конфиг; полные пер-маршрутные
  оверрайды можно добавить позже без слома схемы. **Нормализация — чистый
  `Settings.EffectiveRoutes()`** (единственная точка истины «один vs много», пустой
  `target` → `me` по ADR-4); вся развилка маршрутизации тестируема юнитом (ADR-7),
  `summarize` остаётся тонким и сетевым. **Политика сбоев — толерантная** (выбор
  пользователя, в духе ADR-8/ADR-11): маршруты идут последовательно, каждый пытаемся,
  ошибки копим и в конце `errors.Join` → exit 1, если упал хоть один; упавший маршрут не
  блокирует остальные. `Resolver` один на прогон (общий кэш пиров между маршрутами; не
  потокобезопасен — маршруты строго последовательны). Логи — пер-маршрут по индексу +
  метрики/`target`, без контента (NFR-2). **CLI-оверрайды `--source-chats`/`--target-chat`
  действуют только на одно-маршрутный путь** (когда `routes` пуст); в Actions всё равно
  правится только `config.yml`.
- **ADR-20. Мультипровайдер LLM: OpenRouter (M25).** Вторая реализация `llm.Provider`
  рядом с `claude.go`; контракт `Provider`/`Result` (ADR-13) не меняется. **Выбор
  провайдера — явное поле `llm_provider: anthropic|openrouter`** (дефолт `anthropic`,
  полная обратная совместимость template-инстансов) + CLI-оверрайд `--llm-provider`
  (pointer-механизм ADR-14). Никакого вывода провайдера из формата модели или наличия
  ключа — предсказуемо и валидируемо (дух ADR-3/4). **Транспорт — официальный
  `openai-go` v3 с `option.WithBaseURL("https://openrouter.ai/api/v1")`**, а не ручной
  `net/http`: типы + авто-ретраи 429/5xx из SDK (логика ADR-9; граф зависимостей и так
  тяжёлый из-за gotd). Совместимость подтверждена доками OpenRouter (2026-07-11):
  официально поддерживаются OpenAI SDK через base_url override; `usage`
  (`prompt_tokens`/`completion_tokens`) в ответе по умолчанию; `finish_reason`
  нормализован до `stop|length|tool_calls|content_filter|error`, обрыв на max_tokens =
  `length` → `Result.Truncated`. Живой probe с ключом — при первом eval-прогоне (M26).
  Опциональные заголовки `HTTP-Referer`/`X-Title` (маркетинговая идентификация в
  каталоге) в v1 не шлём. **Секрет — `SVODKA_OPENROUTER_KEY`**; `validate` требует ключ
  только выбранного провайдера; при `openrouter` поле `model` обязательно (нейминг
  `vendor/model`, Anthropic-дефолт не подходит). **Prompt caching через OpenRouter в v1
  не шлём** (по M24 кэш и так ниже минимального префикса и молчит); байт-идентичность
  `mapSystem` — провайдеро-независимый инвариант digest, сохраняется (ADR-9/10/15).
  Пустой content → ошибка (зеркало `claude.go`).
- **ADR-21. Локальное сравнение моделей: eval-конвейер capture → run, судья — вне кода
  (M26-lite).** Отдельная команда `api/cmd/eval` (образец M19: не мешает основному пути),
  всё локально — никаких Actions и Telegram-send. **Вход — capture-replay, не
  live-fetch:** фаза `capture` один раз забирает сообщения (реюз `Resolver`/`FetchWindow`
  внутри `client.Run`) и пишет снапшот `eval/capture-<ts>.json`; все кандидаты и все
  последующие итерации промпта получают байт-идентичный вход, повторные прогоны не ходят
  в Telegram. **Формат снапшота:** версионированный JSON (`schema: 1`, `captured_at`,
  использованные настройки сериализации — `output_lang`, `timezone`, `backlinks`,
  `extra_instructions`, `window_hours` — и `[]telegram.ChatMessages`; `Time` в RFC3339).
  Настройки едут в снапшоте, чтобы `run` не требовал Telegram-секретов и конфига и был
  воспроизводим. **Фаза `run`:** `--snapshot` + `--models` (CSV, дух ADR-14) → для каждой
  модели `digest.Build` с прод-промптами через **OpenRouter** (один ключ на всех
  кандидатов, включая Anthropic-модели через неймспейс `anthropic/...`; транспорт ADR-20)
  → `eval/<ts>/digest-<model>.md` + сводка метрик (tokens in/out, ms, chars, truncated).
  **Судью в v1 не кодим** (отход от исходного M26): ранжирование выполняет ревьюирующий
  агент в сессии разработки по фиксированной рубрике (полнота; точность/галлюцинации;
  сохранность бэклинков verbatim, ADR-15; соблюдение formatRules; язык) + цена/латентность
  из метрик, результат — документ с метаданными в `docs/`. Слепые метки при этом
  невозможны — риск bias к именам моделей признаём и принимаем для разового выбора;
  кодовый judge (слепые метки A/B/C, парсинг вердикта) вернём отдельным милстоуном, если
  правки промпта станут регулярными. **NFR-2:** контент попадает только в gitignored
  `eval/` и только по явному локальному действию (аналогия со stdout-флагом ADR-18);
  в логах eval — по-прежнему только счётчики.
- **ADR-22. Инвариант текстов ошибок: без идентификаторов чатов (M23, усиление NFR-2).**
  Ошибки, пересекающие границу business→app, не содержат пользовательских ref
  (`@username`/ссылка/числовой id) и Title чатов: `summarize.collect` и `errors.Join`
  доносят их до логов Actions, а логи публичного/форкнутого репозитория читают чужие
  глаза. Контекст «какой чат упал» даёт `chat_index` в вызывающем слое — этого
  достаточно для диагностики. Тексты ошибок описывают причину (parse/resolve/fetch,
  тип входа), но не вход. Осознанное исключение: `Send` раскрывает target — он и так
  публикуется самим фактом отправки. Инвариант закреплён тест-стражем на конструкторах
  ошибок (чистые пути, ADR-7).
- **ADR-15 (дополнение, M27). Страж бэклинков: детерминированная валидация после Build.**
  Eval 2026-07-11 (`docs/model_eval_2026-07-11.md`) показал: модель может исказить цифру
  message ID в «verbatim»-ссылке (530943 → 531043) — промпт-инструкция «копируй
  посимвольно» снижает частоту, но не гарантирует. Поэтому финальный текст дайджеста
  (после single/reduce; map-выводы промежуточные и не чистятся) проходит чистую
  `sanitizeLinks`: каждая `t.me/c/<id>/<msg>`-ссылка сверяется с множеством пар
  (ChannelID, MsgID), собранным из входных сообщений; ссылка вне множества молча
  удаляется (текст пункта остаётся), счётчик удалённых идёт в `Usage.LinksRemoved` и
  логируется (WARN при >0; только счётчик, NFR-2). Битая ссылка физически не может
  дойти до получателя независимо от дисциплины модели.
- **ADR-15 (дополнение, M28). Публичные бэклинки для чатов с юзернеймом.** Для go-live
  в публичный канал приватный `t.me/c/...` бесполезен подписчикам-нечленам чата-источника,
  поэтому пересмотрено решение A1: при непустом публичном @username чата `linkFor` строит
  `https://t.me/<username>/<msgID>` (открывается всем); без юзернейма — прежний приватный
  формат. Требование `ChannelID != 0` сохраняется: у личек/базовых групп адресуемых
  per-message ссылок нет даже при наличии username. Username добывается в обоих путях
  резолва (`peers.Peer.Username()`; для числовых id — из entities диалога, условные
  TL-поля через `GetUsername()`). Санитайзер M27 расширен на оба формата (единый regex,
  литерал `c/` пробуется первым; юзернеймы 5–32 символов не коллидируют с `c`); valid —
  множество канонических URL, ровно один на сообщение, поэтому «смена формата» моделью
  тоже режется. Дополнительно в valid включаются t.me-ссылки, дословно встречающиеся в
  текстах входных сообщений, — процитированный моделью контент не считается битой ссылкой.
  Снапшоты eval обратно совместимы (новое поле → пустой username → приватный формат).
