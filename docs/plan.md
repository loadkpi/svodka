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
  **Локально проверено 2026-06-06:** AC-2 (login→session) и AC-3 (config→дайджест в
  Saved Messages) пройдены вживую — `messages=67, digest_chars=1312, send_ms=99`.

---

## Доработки после v1 (по запросу)

Не входят в v1-scope; дизайн-развилки разобрать ДО кода (working style). Тесты — только
на чистую логику (ADR-7).

## M9 — Настраиваемый промпт (`extra_instructions`)  `[x]`

Цель: править акценты/тон/состав вывода без перекомпиляции — свободный текст из конфига,
дописываемый к системному промпту. **Область (решение):** только **финальный вывод**
(single + reduce), НЕ map — иначе «будь краток» пересжимает промежуточные саммари до reduce;
бонус — map-система остаётся байт-идентичной (prompt cache, ADR-10). Формат-правила Telegram
в приоритете над extra.

- [x] M9.1 `config`: поле `ExtraInstructions string` в `Settings` (yaml `extra_instructions`),
  дефолт `""` (пусто = текущее поведение). Без валидации.
- [x] M9.2 `business/digest/prompt.go`: helper `withExtra(base, extra)` дописывает extra в
  хвост `singleSystem`/`reduceSystem` (после `formatRules`); пусто → `base` без изменений.
  `mapSystem` не трогаем (с комментарием почему).
- [x] M9.3 Проброс: `summarize.Run` → `digest.Options.ExtraInstructions` → `prompt.go`.
- [x] M9.4 Тесты: extra в single и в reduce, и НЕ в map (+ map-система == `mapSystem` byte-identity);
  пустое значение — инвариант покрыт существующими `singleSystem("ru","")`/`reduceSystem(...,"")`.
- [x] M9.5 README (EN+RU) — строка в таблице; `config.yml`/`config.example.yml` — закомментированный пример.
- [x] M9.6 `go build/vet/test ./...`, `gofmt -l .` — зелёные.

## M10 — Логирование расхода токенов + обрыв вывода  `[x]`

Цель: видеть токены за прогон (стоимость) **и** ловить молчаливый обрыв на
`max_output_tokens` (он был невидим — повод сделать сейчас). Метрики — только счётчики,
без контента (NFR-2). Решение — ADR-13.

- [x] M10.1 `business/llm`: контракт `Provider` → `Summarize(ctx, Input) (Result, error)`,
  `Result{Text, InputTokens, OutputTokens int; Truncated bool}` (ADR-13).
- [x] M10.2 `claude.go`: заполняет из `resp.Usage` (Input/Output) и `Truncated =
  resp.StopReason == max_tokens`.
- [x] M10.3 `digest.Build`: суммирует `Usage` по всем вызовам (single или N map + reduce;
  `Truncated` = OR), возвращает `(string, Usage, error)`.
- [x] M10.4 `summarize.Run`: лог `tokens_in`/`tokens_out` рядом с `digest_chars`/`llm_ms`;
  WARN «digest truncated at max_output_tokens» при обрыве.
- [x] M10.5 Тесты: фейк `Provider` отдаёт `Result`; добавлены тесты агрегации `Usage`
  (single/map-reduce + `Truncated`); существующие тесты обновлены под сигнатуру.
- [x] M10.6 `go build/vet/test ./...`, `gofmt -l .` — зелёные.

## M11 — CLI-оверрайды настроек для локального запуска  `[x]`

Цель: разово менять Settings флагами без правки `config.yml`, напр.
`go run ./api/cmd/svodka --window-hours 72 --target-chat me`. Удобно для ручных/тестовых
прогонов; в Actions по-прежнему работает `config.yml`. Приоритет: **flag > yaml > defaults**.
**Решения (ADR-14):** механизм — pointer-флаги в той же conf-модели (проба подтвердила:
conf v3.12.0 оставляет `*T` = `nil`, если не передан, и применяет явные `0`/`""`); охват —
все 7 скаляров + `source_chats` (CSV, режем сами — conf не сплитит флаг-слайс по запятой).

- [x] M11.1 Дизайн: pointer-поля `Overrides` (только non-nil применяются); единый conf-парс
  `struct{Secrets; Overrides}` в `Load()`, `LoadSecrets()` (login) — только Secrets (ADR-14).
- [x] M11.2 `config`: `applyOverrides(&settings, ov)` поверх yaml+defaults (только non-nil;
  flag>yaml>default), `splitChats` для CSV `source_chats`; ADR-3 не нарушён.
- [x] M11.3 `--help`: все 8 оверрайд-флагов видны в общем usage (conf), env-двойники бонусом.
- [x] M11.4 Тесты (`config_test.go`, чистые): nil не трогает Settings; каждое поле применяется;
  явные `0`/`""` проходят (ADR-3); `splitChats` (CSV/trim/пустые/мультиарг).
- [x] M11.5 README (EN+RU): пример локального разового запуска с флагами; отмечено, что
  scheduled-прогон в Actions использует `config.yml`.
- [x] M11.6 `go build/vet/test ./...`, `gofmt -l .`, `golangci-lint` — зелёные. ADR-14.

Порядок M12–M19 = приоритет пользователя (B,E,C,D,F,G,H,A). Номера ADR — предварительные.

## M12 — CI + линтинг (Maintainer-гигиена)  `[x]`

Цель: на push/PR гонять проверки, держать публичный template здоровым. Сделано.

- [x] M12.1 `.github/workflows/ci.yml` — `on: push (main) + pull_request`; `actions/checkout@v6`
  + `actions/setup-go@v6` (`go-version-file: go.mod`); job `build-test` = `go build/vet/test ./...`
  + gofmt-check (fail если непусто); `permissions: contents: read`.
- [x] M12.2 `.golangci.yml` (schema `version: "2"`, `default: standard`; errcheck
  `exclude-functions: fmt.Fprint*` — прометки в stderr); job `lint` = `golangci/golangci-lint-action@v9`,
  golangci-lint `v2.12`. Набор линтеров — **standard** (решение).
- [x] M12.3 `.github/dependabot.yml` — `gomod` + `github-actions`, еженедельно.
- [x] M12.4 Зелёный CI на текущем коде: починены 11 замечаний (errcheck-исключение Fprint* +
  `_ =` на `Close()` в тесте + ST1005 reword error-строки в `svodka/main.go`). Локально
  golangci-lint `0 issues`, build/vet/test/gofmt зелёные. Бейдж CI пропущен (нет remote/owner —
  добавит owner после публикации).

## M13 — Бэклинки на сообщения  `[x]` → ADR-15

Цель: ссылки `t.me/c/<id>/<msg>` к ключевым пунктам, чтобы прыгнуть в первоисточник
(расширяет FR-9). Решения ADR-15: формат — только приватный `t.me/c/...` (A1, каналы/
супергруппы); вставка — гибрид (B3: `linkFor` пишет голый URL в user-текст, модель
привязывает к буллетам); голый URL (C1); флаг `backlinks` default-true; приватная
ссылка откроется только участнику — задокументировано.

- [x] M13.1 `telegram`: `Message.ID` + `ChatMessages.ChannelID` в нормализацию/`FetchWindow`.
- [x] M13.2 Способ вставки ссылок (ADR-15): гибрид B3 — `linkFor` в serialize + `formatRules`.
- [x] M13.3 Формат под Telegram — голый URL (автолинк), без markdown (`formatRules`).
- [x] M13.4 Тесты: чистая `linkFor`, serialize-с-бэклинками, флаг конфига. README. Гейт зелёный.

## M14 — Видимость сбоев cron  `[ ]` → ADR-16

Пересматривает «уведомления вне scope» из ADR-12. Развилка/ADR-16: дефолтные email-фейлы
GitHub (документировать, 0 кода) vs активный «при ошибке → строка в `target`/me» (нужен
доступ в failure-ветке) vs оба.

- [ ] M14.1 Решение объёма (ADR-16): док vs telegram-on-error vs оба.
- [ ] M14.2 (если telegram) on-failure шаг: краткая ошибка БЕЗ секретов/контента (реюз `Send`).
- [ ] M14.3 README (EN+RU): «если прогон упал» — где смотреть, как чинить протухшую session.
- [ ] M14.4 `build/vet` зелёные.

## M15 — Режим «календарный день»  `[ ]` → ADR-17 (вбирает идею `until`)

Цель: саммари за прошлый полный день в `timezone`, не скользящие N часов. Развилка/ADR-17:
`rolling` (как сейчас) vs `calendar`; требует верхней границы окна.

- [ ] M15.1 `telegram`: `FetchWindow(... since, until)` — добавить верхнюю границу.
- [ ] M15.2 `config`: `window_mode: rolling|calendar` (дефолт `rolling` — обратная совместимость).
- [ ] M15.3 `summarize`: расчёт `[since, until)` по режиму и `timezone`.
- [ ] M15.4 Тесты (чистые): границы окна по режиму/таймзоне. README. `build/vet/test/gofmt`.

## M16 — Локальный `--no-send` / `--stdout`  `[ ]` → ADR-18 (на инфре M11)

Цель: печатать дайджест вместо отправки — быстрый цикл подгонки промпта (M9) без засорения
«Избранного». Развилка/ADR-18: уточнить связь с ADR-4 (это **локальный CLI-флаг**, не конфиг
`dry_run`); stdout vs stderr; имя флага.

- [ ] M16.1 Флаг через механизм M11: вместо `Send` печатать дайджест.
- [ ] M16.2 Метрики-логи сохраняются; контент идёт в stdout только по явному флагу (NFR-2).
- [ ] M16.3 README (EN+RU): пример локальной подгонки промпта. `build/vet/test/gofmt`.

## M17 — Чанкинг сверхбольшого чата  `[ ]` (обновляет ADR-10)

Цель: один чат сам по себе > контекста — текущий порог режет по чатам, но мега-чат всё равно
упадёт. Резать на части внутри map.

- [ ] M17.1 `digest`: если userText одного чата > порога — несколько map-вызовов, склейка в
  один per-chat summary.
- [ ] M17.2 Порог/перекрытие; сохранить байт-инвариант map-системы (prompt cache).
- [ ] M17.3 Тесты (чистые) на разбиение. Обновить ADR-10. `build/vet/test/gofmt`.

## M18 — Оценка стоимости в логах  `[ ]` (на M10)

- [ ] M18.1 `config`: цены input/output за 1М токенов. Развилка: конфиг-поля vs зашитая
  таблица по модели. Рекомендация — конфиг-поля (цены/модели меняются).
- [ ] M18.2 `summarize`: `est_cost` = токены × цена в финальный лог (рядом с `tokens_*`).
- [ ] M18.3 Тесты (чистый расчёт). README. `build/vet/test/gofmt`.

## M19 — Preflight / `doctor`  `[ ]`

Цель: проверить конфиг, **зарезолвить каждый `source_chat`**, пингануть Anthropic-ключ — без
отправки. Закрывает разрыв AC-1↔AC-2..5 для онбординга. Развилка: отдельная команда
`api/cmd/doctor` vs флаг `--check`. Рекомендация — отдельная команда (не мешает основному пути).

- [ ] M19.1 `doctor`: `config.Load` → `telegram.Run` → `Resolve` каждого чата (ok/fail список
  без контента) → лёгкий ping Anthropic.
- [ ] M19.2 Понятный отчёт (что не так и как чинить) + exit code.
- [ ] M19.3 README (EN+RU): «проверка перед первым запуском» + Makefile target. `build/vet`.

## M20 — Архитектурная гигиена (ревью по книгам O'Reilly)  `[~]`

Цель: точечные улучшения по итогам сверки с *Learning Go* (Bodner), *The Go Programming
Language* (K&R), *Effective Concurrency in Go* (Serdar). Архитектура в целом признана
здоровой; ниже — реальные шероховатости.

- [x] M20.1 Снять теневой импорт: `business/telegram/client.go` импортирует
  `github.com/gotd/td/telegram` как `gotgram`, чтобы он не затенял имя собственного пакета
  `telegram` (читаемость; K&R/Bodner — не брать имена пакетов, конфликтующие с импортами).
- [x] M20.2 Дедлайны на сетевые шаги (`app/summarize`): backstop-таймауты `chatTimeout` (2м,
  resolve+fetch одного чата через хелпер `fetchChat`), `llmTimeout` (5м, весь `digest.Build`),
  `sendTimeout` (1м) — производные от родительского ctx; зависший Telegram/Anthropic даёт
  понятную ошибку по шагу до общего 10-мин cap Actions. Политика таймаутов — в слое use-case;
  бизнес-пакеты свободны от зашитых значений (кандидат на вынос в конфиг позже).
- [ ] M20.3 (опц., не сделано) Зафиксировать в ADR: `Resolver` не потокобезопасен
  (мутабельные `dialogByID`/`scanned`) и последовательная обработка чатов — осознанный
  trade-off (rate-limit + prompt-cache), чтобы будущий контрибьютор не распараллелил вслепую.

## Идеи (не запланировано)

Согласованы в принципе, без приоритета (гигиена/доки):
- Пин Actions по SHA (supply-chain для template) vs читаемость `@v6`.
- Метрики прогона в `$GITHUB_STEP_SUMMARY` (UI Actions; только счётчики, NFR-2 ок).
- Док про ротацию/отзыв session («завершить сеансы» в Telegram при утечке).
- Фильтры источников: пропускать ботов/авторов/низкоактивные чаты.
- Калибровка порога map-reduce под язык. `defaultThresholdChars=16000` заложен из ~4 символа/
  токен, но для кириллицы реально ~1.6 символа/токен (наблюдение: 10194 символа → 6226 токенов),
  т.е. порог по символам для русского грубоват. Варианты: коэффициент по `output_lang` или
  честный `count_tokens` перед выбором single vs map-reduce (сеть/латентность — взвесить).
- `login` должен игнорировать `SVODKA_TELEGRAM_SESSION` из env (он всё равно минтит новую) —
  убрать грабли «битая сессия в env валит login».
  (Идея «`until` / диапазон дат» поглощена M15.)

---

## Заметки по проверке

- Функциональный прогон Telegram/Claude требует личных кредов пользователя — выполняет
  Deployer по README. Со стороны разработки верифицируем сборку + `go vet` + ревью.
- Порядок безопасного теста: `target_chat: me`, `window_hours: 2`, один чат → смотреть
  «Saved Messages», затем переключать на группу и сутки.
