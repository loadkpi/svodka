# svodka — Implementation Plan (подробно, по подзадачам)

Статусы: `[ ]` todo · `[~]` в работе · `[x]` готово.
Каждая подзадача — небольшой самостоятельный шаг. После M0-M1 после каждого
блока прогоняем `go build ./...` и `go vet ./...`.

---

## M0 — Скаффолд и конфиг  `[x]`

- [x] M0.1 `go mod init svodka`; зависимости: `gotd/td`, `gotd/contrib`,
  `ardanlabs/conf/v3`, `gopkg.in/yaml.v3`, `golang.org/x/time`.
- [x] M0.2 Исследовать API gotd/conf (`go doc`), проверить поведение conf+yaml (probe).
- [x] M0.3 `foundation/logger/logger.go` — slog-обёртка (Info/Warn/Error с ctx),
  вывод в stderr, без логирования контента.
- [x] M0.4 `config/config.go`:
  - [x] структура `Config` (Secrets via conf/env + Settings via yaml.v3);
  - [x] `Load()` + `LoadSecrets()`: conf.Parse(env) → yaml.Unmarshal(config.yml) →
    ручные дефолты нулевых content-полей → валидация;
  - [x] `validate()` для режима svodka (session, ключ Claude, непустой source_chats).
- [x] M0.5 `config.example.yml` + `config.yml` (дефолтный безопасный: `target_chat: me`).
- [x] M0.6 `.gitignore` (`session*.json`, `*.local`, `bin/`, IDE/OS), `LICENSE` (MIT, Pavel Kozlov),
  `Makefile` (`help`/`tidy`/`vet`/`build`/`run`/`login`, default = help).
- [x] M0.7 `go mod tidy`, `go build ./...`, `go vet ./...` — зелёные.
  ⚠️ tidy убрал из `go.mod` `gotd/td`, `gotd/contrib`, `golang.org/x/time`
  (не импортируются в M0); восстановить через `go get` при M1.1.

## M1 — Telegram core + svodka main  `[x]`

- [x] M1.1 `business/telegram/session.go` — `LoadStorage(ctx, b64) (*session.StorageMemory, error)`
  и `Export(ctx, *StorageMemory) (b64 string, error)` через `encoding/base64`.
- [x] M1.2 `business/telegram/client.go` — `New(ctx, cfg)` собирает `telegram.Client`
  (Options: SessionStorage, Middlewares=floodwait, Logger=nil); методы `Run(ctx, fn(ctx, *tg.Client))`,
  `Auth() *auth.Client`, `Storage() *session.StorageMemory`.
- [x] M1.3 `api/cmd/svodka/main.go` — `signal.NotifyContext` (INT/TERM), `config.Load`
  (с обработкой `config.ErrHelp`), `client.Run` → лог только `user_id` (без username/имени).
- [x] M1.4 Проверка авторизации на двух уровнях: `config.validate` падает при пустом
  `SVODKA_TELEGRAM_SESSION` с подсказкой; внутри `Run` — `Auth().Status`, при
  `!Authorized` понятная ошибка про refresh session в Codespace.
- [x] M1.5 `go build ./...`, `go vet ./...` — зелёные.

## M2 — Login-команда (Codespaces)  `[x]`

- [x] M2.1 `termAuth` — реализация `auth.UserAuthenticator` (Phone/Code/Password из
  stdin; SignUp → ошибка «аккаунт не существует»; AcceptTOS).
  `business/telegram/term_auth.go`: промпты в stderr, пароль через `term.ReadPassword`
  с fallback на bufio при не-tty; введённые значения не логируются.
  `term_auth_test.go` — юнит-тесты чистой логики (Phone/Code/Password-fallback, trim,
  EOF, SignUp-отказ, AcceptTOS) через `os.Pipe`. Политика тестов закреплена в ADR-7.
- [x] M2.2 `api/cmd/login/main.go` — `config.LoadSecrets` (НЕ Load: session ещё нет) →
  `&config.Config{Secrets}` с пустым Session → `telegram.New` → `client.Run` →
  `Auth().IfNecessary(ctx, auth.NewFlow(telegram.TermAuth(os.Stdin, os.Stderr), auth.SendCodeOptions{}))`
  → Status-check → лог `user_id`. Экспорт session в Secret — M2.3.
- [x] M2.3 По успеху — `telegram.Export` → base64; `storeSession`: `exec.LookPath("gh")`,
  затем `gh secret set SVODKA_TELEGRAM_SESSION` (stdin = base64). При отсутствии gh или
  ошибке команды — fallback `printManual` (печать строки + инструкция вставить вручную;
  stderr от gh показывается).
- [x] M2.4 Маскирование: при успехе `gh` сама строка НЕ печатается (только лог
  `session stored as GitHub secret`); в fallback — предупреждение «full access, не
  передавать, добавить в Secrets, очистить терминал» (warning в stderr, значение в stdout).
- [x] M2.5 `go build ./...`, `go vet ./...`, `go test ./...`, `gofmt` — зелёные.
  `api/cmd/login/main_test.go` — тест инварианта маскирования (ADR-7): чистое ядро
  `persistSession(stdout, stderr, setter, b64)` (exec вынесен в `ghSecretSet`); кейсы:
  успех → строка не утекает в stdout/stderr; ошибка setter → fallback печатает строку
  + warning + текст ошибки; nil setter (нет gh) → fallback.

## M3 — Резолв пиров + чтение истории  `[x]`

- [x] M3.1 `business/telegram/resolve.go` — `Resolver{manager}` (один на запуск);
  `Resolve(ref)`: `classifyInput` (strip `@`/`t.me/`, отказ на invite-ссылки),
  `manager.Resolve` для юзернеймов/доменов. Юнит-тест `classifyInput`/`tdlibID` (ADR-7).
- [x] M3.2 Фолбэк для числовых id: ленивый `ensureDialogs` — один проход
  `query.GetDialogs(...).ForEach`, карта `TDLibPeerID → Peer` (ADR-8).
- [x] M3.3 `business/telegram/history.go` — `FetchWindow(ctx, api, Peer, since time.Time)
  (ChatMessages, error)`: итератор `GetHistory(...).BatchSize(100).Iter()`, стоп по
  `date < since`, `reverse` в хронологию.
- [x] M3.4 Нормализация `normalize(msg, ent)`: type-switch `*tg.Message` (сервисные →
  skip); автор из `FromID`+`Entities` (пост канала → пусто); `mediaTag`/`documentTag` —
  пометка по типу (`[photo]`/`[video]`/`[voice]`/`[sticker]`/…), webpage без метки.
- [x] M3.5 Тип `Message{Author, Time time.Time, Text}` + `ChatMessages{Title, Messages}`.
  Юнит-тесты `mediaTag`/`normalize`/`reverse` (ADR-7); сетевой `FetchWindow` не юнитим.
- [x] M3.6 `go build ./...`, `go vet ./...`, `go test ./...`, `gofmt` — зелёные.

## M4 — LLM-провайдер (Claude)  `[x]`

- [x] M4.1 `business/llm/llm.go` — `Provider` интерфейс, `Input{System,User,Model,MaxTokens}`.
- [x] M4.2 `business/llm/claude.go` — `NewClaude(apiKey)`; `Summarize` через
  `anthropic-sdk-go` (`client.Messages.New`), `cache_control` на system-блоке (ADR-9),
  модель/MaxTokens из конфига. `extractText` — сбор `TextBlock` из `Content`.
- [x] M4.3 Обработка ошибок: сетевые/коды/ретраи 429-5xx — в SDK; пустой content → ошибка.
- [x] M4.4 `go build`/`vet`/`test`/`gofmt` — зелёные. `claude_test.go` — юнит `extractText`
  (пустой/один/несколько text-блоков, thinking игнорируется) через `json.Unmarshal` (ADR-7).

## M5 — Формирование дайджеста  `[x]`

- [x] M5.1 `business/digest/prompt.go` — system-промпты (single/map/reduce), параметр
  `output_lang` (ISO 639-1), общий `formatRules`, структура «по чатам». `map`-промпт
  собирается один раз и переиспользуется байт-в-байт между map-вызовами (prompt cache).
- [x] M5.2 `business/digest/serialize.go` — `serializeChat`/`serializeAll`: компактный
  user-текст `## Title` + `[HH:MM] Author: text` (автор пуст для постов канала), время
  абсолютное в `Location` (из `Timezone`). Чистые функции.
- [x] M5.3 `useMapReduce(userText, threshold)` — порог по символам (rune count,
  `defaultThresholdChars=16000`, ~4 символа/токен). Чистая, table-тест.
- [x] M5.4 `Build(ctx, llm.Provider, []telegram.ChatMessages, Options)` — single-проход
  ниже порога; map (саммари по чату) + reduce (склейка) выше. Пустой map-вывод чата
  выкидывается из reduce; ошибки оборачиваются по индексу чата (без контента, NFR-2).
  `digest.Build` принимает `Provider` параметром → тест с фейком (ADR-7).
- [x] M5.5 Форматирование под Telegram задаётся `formatRules` (plain text, `- ` буллеты,
  без `#`/таблиц/код-фенсов); финальная склейка в reduce/single, `TrimSpace` на выходе.
- [x] M5.6 `go build`/`vet`/`test`/`gofmt` — зелёные. `digest_test.go`: `serializeChat`
  (+timezone), `serializeAll`, `useMapReduce` (table), `Build` (single/map-reduce/пустой
  вход/пропуск пустого map-вывода/проброс ошибки/инвариант идентичного map-system).

## M6 — Оркестрация + отправка  `[x]`

- [x] M6.1 `business/telegram/send.go` — `Send(ctx, api, *Resolver, target, text)`:
  `me`→`Self()` (`isSelf`, case-insensitive), иначе `Resolver.Resolve`→`sender.To(p.Input)`;
  сплит >4096 рун через чистый `splitMessage` (режет по последнему `\n` в окне, иначе
  hard-cut). Юнит-тесты `splitMessage`/`isSelf` (ADR-7).
- [x] M6.2 `app/summarize/summarize.go` — `Run(ctx, api, Deps{Log,Cfg,Provider})`:
  `LoadLocation(Timezone)` → `windowStart(now,WindowHours)` → один `NewResolver` на запуск →
  `collect` (resolve+FetchWindow по чатам) → `digest.Build` → `telegram.Send`. Тонкий,
  сетевой (не юнитим); чистые хелперы — в `helpers.go` (`windowStart`, `chatChars`,
  `runeLen`, `stats`), с тестами. Толерантность к чатам и пустому дайджесту — ADR-11.
- [x] M6.3 Подключён в `api/cmd/svodka/main.go`: внутри `client.Run` после auth-check
  → `summarize.Run(ctx, api, Deps{Provider: llm.NewClaude(key)})`.
- [x] M6.4 Логи-метрики (counts only, NFR-2): `chats_total/ok/failed`, `messages`, `chars`,
  `digest_chars`, тайминги `fetch_ms/llm_ms/send_ms/total_ms`. Сбой чата логируется по
  `chat_index` (не по ссылке). Контент/секреты не логируются.
- [x] M6.5 `go build ./...`, `go vet ./...`, `go test ./...`, `gofmt -l .` — зелёные.

## M7 — GitHub-обвязка  `[~]`

- [x] M7.1 `.github/workflows/daily.yml` — `schedule` cron `0 6 * * *` (06:00 UTC) +
  `workflow_dispatch`; `actions/checkout@v6` + `actions/setup-go@v6`
  (`go-version-file: go.mod`, кэш дефолтный); `go run ./api/cmd/svodka`;
  `permissions: contents: read`; `timeout-minutes: 10`. Env — 4 секрета
  (`SVODKA_TELEGRAM_API_ID/API_HASH/SESSION`, `SVODKA_ANTHROPIC_KEY`); `SVODKA_CONFIG`
  не секрет, а путь к `config.yml` (ADR-12).
- [x] M7.2 `.devcontainer/devcontainer.json` — образ `mcr.microsoft.com/devcontainers/go:1.25`
  + фича `ghcr.io/devcontainers/features/github-cli:1` (gh для login).
- [~] M7.3 Пометить репозиторий как template — действие владельца в UI
  (Settings → Template repository); инструкция Deployer'у — в README (M8.1).

## M8 — Документация и финал  `[x]`

- [x] M8.1 `README.md` (EN, основной) + `README.ru.md` — двуязычно, параллельно.
  Единый deploy-поток (шаги 1-7: template→api_id/hash→Codespaces login→session→
  Anthropic-ключ→config.yml→Run workflow тест (`target_chat: me`)→go live) + справочные
  секции: полная таблица настроек `config.yml`, таблица секретов (4 шт.),
  «Schedule» (UTC-cron + пересчёт, best-effort, 60 дней, смена IP/новый вход), privacy.
- [x] M8.2 Финальные `go build ./...`, `go vet ./...`, `go test ./...`, `gofmt -l .` — зелёные.
- [x] M8.3 Чек-лист приёмки (requirements §7): **AC-1** (build+vet) — пройден авто;
  **AC-2..AC-5** — функциональные, требуют личных кредов Deployer'а → покрыты
  инструкцией в README (login→session, тест через `workflow_dispatch` с `target_chat: me`,
  смена target на группу, чистые логи). Прогон выполняет Deployer.

---

## Заметки по проверке

- Функциональный прогон Telegram/Claude требует личных кредов пользователя — выполняет
  Deployer по README. Со стороны разработки верифицируем сборку + `go vet` + ревью.
- Порядок безопасного теста: `target_chat: me`, `window_hours: 2`, один чат → смотреть
  «Saved Messages», затем переключать на группу и сутки.
