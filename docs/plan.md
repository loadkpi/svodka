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

## M2 — Login-команда (Codespaces)  `[ ]`

- [ ] M2.1 `termAuth` — реализация `auth.UserAuthenticator` (Phone/Code/Password из
  stdin; SignUp → ошибка «аккаунт не существует»; AcceptTOS).
- [ ] M2.2 `api/cmd/login/main.go` — `config.Load` (нужны api_id/hash); client.Run;
  `client.Auth().IfNecessary(ctx, auth.NewFlow(termAuth, ...))`.
- [ ] M2.3 По успеху — экспорт session → base64; попытка `gh secret set
  SVODKA_TELEGRAM_SESSION`; fallback: печать строки + инструкция вставить вручную.
- [ ] M2.4 Маскирование: не печатать session, если ушёл в секрет; явное предупреждение
  о чувствительности.
- [ ] M2.5 `go build ./...`, `go vet ./...`.

## M3 — Резолв пиров + чтение истории  `[ ]`

- [ ] M3.1 `business/telegram/resolve.go` — построить `peers.Manager`; `Resolve(s)`:
  нормализация (`@`, `t.me/`), `manager.Resolve` для юзернеймов/доменов.
- [ ] M3.2 Фолбэк для числовых id: один раз собрать карту id→InputPeer через
  `query.GetDialogs(...).ForEach`.
- [ ] M3.3 `business/telegram/history.go` — `FetchWindow(peer, since time.Time) ([]Message)`:
  итератор `GetHistory`, стоп по `date < since`.
- [ ] M3.4 Нормализация: type-switch `*tg.Message`; автор из `Elem.Entities`; текст;
  медиа → плейсхолдер; пропуск сервисных.
- [ ] M3.5 Тип `Message{ChatTitle, Author, Time, Text}` и группировка `ChatMessages`.
- [ ] M3.6 `go build ./...`, `go vet ./...`.

## M4 — LLM-провайдер (Claude)  `[ ]`

- [ ] M4.1 `business/llm/llm.go` — `Provider` интерфейс, `Input{System,User,Model,MaxTokens}`.
- [ ] M4.2 `business/llm/claude.go` — `New(apiKey)`; `Summarize` (POST /v1/messages,
  заголовки, prompt caching на system, таймаут, парсинг ответа).
- [ ] M4.3 Обработка ошибок: коды != 200, пустой content, network → понятные ошибки.
- [ ] M4.4 `go build ./...`, `go vet ./...`.

## M5 — Формирование дайджеста  `[ ]`

- [ ] M5.1 `business/digest/digest.go` — system-промпт (язык `output_lang`, структура).
- [ ] M5.2 Сериализация сообщений в компактный user-текст (по чатам, с автором/временем).
- [ ] M5.3 Оценка объёма → выбор стратегии: один вызов vs map-reduce.
- [ ] M5.4 Map: саммари по чату; Reduce: финальная склейка в дайджест.
- [ ] M5.5 Форматирование под Telegram (заголовки/маркеры, без хрупкого markdown).
- [ ] M5.6 `go build ./...`, `go vet ./...`.

## M6 — Оркестрация + отправка  `[ ]`

- [ ] M6.1 `business/telegram/send.go` — `Send(ctx, target, text)`; `me`→`Self()`,
  иначе `Resolve/To`; сплит >4096 на части.
- [ ] M6.2 `app/summarize/summarize.go` — `Run(ctx, deps)`: resolve+fetch по всем
  чатам → digest.Build → send; вернуть метрики.
- [ ] M6.3 Подключить оркестратор в `api/cmd/svodka/main.go` (вместо smoke).
- [ ] M6.4 Логи-метрики: чатов/сообщений/символов/тайминги (без контента).
- [ ] M6.5 `go build ./...`, `go vet ./...`.

## M7 — GitHub-обвязка  `[ ]`

- [ ] M7.1 `.github/workflows/daily.yml` — `schedule` cron + `workflow_dispatch`;
  `setup-go`; `go run ./api/cmd/svodka`; env = Secrets; `permissions: contents: read`.
- [ ] M7.2 `.devcontainer/devcontainer.json` — образ Go + наличие `gh` (для login).
- [ ] M7.3 Пометить репозиторий как template (инструкция в README; галочка делается в UI).

## M8 — Документация и финал  `[ ]`

- [ ] M8.1 `README.md` — пошагово для Deployer'а (template→secrets→codespaces login→
  config→test→go live), таблица Secrets, заметки про IP/60-дней/приватность.
- [ ] M8.2 Финальные `go build ./...`, `go vet ./...`, `gofmt`.
- [ ] M8.3 Прогон чек-листа критериев приёмки (см. requirements §7), что доступно без
  личных кредов (сборка/vet); функциональное — инструкцией пользователю.

---

## Заметки по проверке

- Функциональный прогон Telegram/Claude требует личных кредов пользователя — выполняет
  Deployer по README. Со стороны разработки верифицируем сборку + `go vet` + ревью.
- Порядок безопасного теста: `target_chat: me`, `window_hours: 2`, один чат → смотреть
  «Saved Messages», затем переключать на группу и сутки.
