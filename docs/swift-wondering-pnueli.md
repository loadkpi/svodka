# План: `svodka` — Telegram daily-digest userbot (Go, публичный template)

## Context

Нужен сервис, который под **личным аккаунтом** пользователя заходит в указанные
Telegram-чаты, собирает сообщения за сутки, делает саммари через LLM и постит в
отдельный чат/группу. Запуск раз в день.

**Главная цель проекта:** это **публичный GitHub template** — любой человек жмёт
«Use this template», создаёт **приватную** копию, настраивает всё через UI
(Secrets + `config.yml`), один раз логинится в Telegram через **GitHub Codespaces**
и получает работающий ежедневный дайджест. Никакой инфраструктуры — всё крутится в
**GitHub Actions (cron)**.

### Зафиксированные решения
- **Язык:** Go, MTProto через **`gotd/td`** (зрелая чистая Go-библиотека, обычный TCP).
- **Хостинг:** **GitHub Actions** (`schedule` cron + `workflow_dispatch` для теста).
- **Распространение:** **template repository (public) → private инстанс** у каждого.
- **Логин/session:** только **Codespaces** — `go run ./api/cmd/login` в браузерном
  терминале → пишет session в Secret через `gh secret set`.
- **LLM:** только **Claude** (за интерфейсом `llm.Provider`, чтобы потом добавить других).
- **Хранилище:** **БД не нужна.** Единственное состояние между запусками — session
  (auth key), лежит в GitHub Secret (read-only в рантайме). Пиров резолвим каждый
  запуск; апдейты не слушаем (тянем `messages.getHistory` по дате); сообщения не храним.
- **Структура:** по мотивам `ardanlabs/service` (слои `foundation`/`business`/`app`,
  бинарники в `api/cmd/*`, конфиг через `ardanlabs/conf`, slog-логгер, Makefile) —
  **trimmed**: без k8s/HTTP/zarf/otel, т.к. это CLI-cron, а не сервис.

### Безопасность (важно для публичного template)
- session = полный доступ к аккаунту → только Secret, **никогда** в коде/логах.
- На public-репо логи Actions видны всем → деплой-инстанс делается **private**, и код
  **не логирует содержимое сообщений и секреты** (только метрики: сколько чатов/сообщений).
- **`dry_run: true` по умолчанию** — первый прогон постит в «Saved Messages», не в группу.

---

## Архитектура (поток одного запуска)

```
GitHub Actions (cron / ручной запуск)
  └─ go run ./api/cmd/svodka
       1. conf: загрузить config.yml + env (Secrets)        foundation/conf
       2. gotd Client.Run (session из env, floodwait mw)     business/telegram
       3. для каждого source-чата: резолв пира + getHistory   business/telegram
          за последние window_hours → нормализованные msgs
       4. digest: построить промпт (map-reduce если много)    business/digest
          → llm.Provider.Summarize (Claude)                  business/llm
       5. отправить дайджест в target (или Saved Messages)    business/telegram
```

Всё короткое (10 сек–2 мин), без долгоживущих соединений.

---

## Структура репозитория (Ardan-flavored, trimmed)

```
svodka/
  go.mod                      # module svodka  (без привязки к owner — удобно для template)
  Makefile                    # run, login, tidy, vet, build, codespace-login
  config.yml                  # ПОЛЬЗОВАТЕЛЬСКИЙ конфиг (правится в web-UI)
  config.example.yml          # документированный пример
  api/cmd/
    svodka/main.go            # ежедневная задача (cron entrypoint)
    login/main.go             # одноразовый интерактивный логин → session → gh secret set
  app/
    summarize/summarize.go    # use-case: оркестрация read→digest→send (склейка слоёв)
  business/
    telegram/
      client.go               # сборка gotd Client (Options, floodwait/ratelimit mw)
      session.go              # session из/в base64 (StorageMemory ← env; экспорт в login)
      history.go              # резолв пиров + getHistory за окно, нормализация сообщений
      send.go                 # отправка дайджеста (styled text), поддержка "me"/dry-run
    llm/
      llm.go                  # interface Provider { Summarize(ctx, SummarizeInput) (string, error) }
      claude.go               # Claude через net/http (/v1/messages, prompt caching)
    digest/
      digest.go               # построение промпта, map-reduce, формат для Telegram
  foundation/
    logger/logger.go          # тонкая slog-обёртка (Ardan-стиль), без логирования контента
  .github/workflows/
    daily.yml                 # schedule cron + workflow_dispatch
  .devcontainer/
    devcontainer.json         # Codespaces: образ Go + gh (для логина)
  .gitignore                  # session*.json, *.local, bin/
  README.md                   # пошаговая инструкция (template→secrets→codespaces→config→test)
  LICENSE                     # MIT
```

Отступление от Ardan: схлопнул `business/{domain,sdk}` и `app/domain` до плоских
пакетов — три слоя архитектуры на ~600 строк логики были бы переусложнением. Слои
и направление зависимостей (`foundation` ← `business` ← `app` ← `api/cmd`) сохранены.

---

## Ключевые технические детали

**Конфиг (`foundation/conf` + `ardanlabs/conf/v3` и `conf/v3/yaml`).**
Слияние: defaults (struct tags) → `config.yml` → env (`SVODKA_*`). Секреты помечаем
`conf:"mask,noprint"`. Поля:
- `config.yml` (правит пользователь): `source_chats: []string` (`@username`/`t.me/...`/id),
  `target_chat: "me"`, `window_hours: 24`, `output_lang: "ru"`, `timezone`,
  `model: "claude-sonnet-4-6"`, `max_output_tokens`, `dry_run: true`.
- Secrets (env): `SVODKA_TELEGRAM_API_ID`, `SVODKA_TELEGRAM_API_HASH`,
  `SVODKA_TELEGRAM_SESSION`, `SVODKA_ANTHROPIC_KEY`.
- cron-расписание живёт в `daily.yml` (требование GitHub) — правится в UI.

**Telegram (`gotd/td`).**
- Клиент: `telegram.NewClient(apiID, apiHash, telegram.Options{SessionStorage, Middlewares})`,
  middlewares — `contrib/middleware/floodwait` (+ `ratelimit`) для авто-FLOOD_WAIT.
- Работа внутри `client.Run(ctx, func(ctx) error {...})`; `api := client.API()`.
- Session: `session.StorageMemory`, преднаполненный байтами из base64-env (read-only;
  auth key стабилен, write-back не нужен).
- Резолв чатов: `telegram/peers.Manager` (юзернеймы/ссылки; приватные без юзернейма —
  через `messages.getDialogs`).
- История: `telegram/query` — `query.Messages(api).GetHistory(peer)` с итерацией и
  отсечкой по `date >= now - window`; нормализуем (автор, время, текст; медиа → `[photo]` и т.п.).
- Отправка: `telegram/message` Sender → styled text; target `"me"` = Saved Messages.
- В `svodka` проверяем `client.Auth().Status`; если не авторизован — понятная ошибка
  «сгенерируйте session через Codespaces».

**LLM (`business/llm`).**
- `Provider` интерфейс; `claude.go` — `net/http` POST `https://api.anthropic.com/v1/messages`,
  заголовки `x-api-key`, `anthropic-version: 2023-06-01`; **prompt caching** (`cache_control`
  на system-блоке); модель из конфига (default `claude-sonnet-4-6`).

**Digest (`business/digest`).**
- Системный промпт: язык из `output_lang`, структура (по чатам → ключевые темы,
  решения, упоминания пользователя, ссылки). Если объём большой — **map-reduce**
  (саммари по чату → финальная склейка), иначе один вызов. Формат — Telegram-friendly.

**Логин (`api/cmd/login`).**
- Интерактив: телефон → код → (2FA) через `auth` flow gotd; по успеху экспорт session
  → base64 → попытка `gh secret set SVODKA_TELEGRAM_SESSION`; при неудаче (нет scope) —
  печать строки и инструкция вставить в Settings → Secrets вручную.

---

## Этапы реализации

1. **M0 — Скаффолд:** `go.mod` (`module svodka`), зависимости (`gotd/td`, `gotd/contrib`,
   `ardanlabs/conf/v3`), `foundation/logger`, `foundation/conf`, `config.example.yml`,
   `.gitignore`, `Makefile`, `LICENSE`.
2. **M1 — Telegram core:** `business/telegram/{client,session,history,send}.go` +
   `api/cmd/svodka/main.go` (signal-context, conf, client.Run; пока просто `getMe()` в лог).
3. **M2 — Логин:** `api/cmd/login/main.go` (интерактив → session → `gh secret set`).
4. **M3 — Чтение истории:** `history.go` — резолв + окно + нормализация.
5. **M4 — LLM:** `business/llm/{llm,claude}.go`.
6. **M5 — Digest:** `business/digest/digest.go` (промпт, map-reduce, формат).
7. **M6 — Оркестрация + отправка:** `app/summarize` + `send.go` + dry-run в Saved Messages.
8. **M7 — GitHub-обвязка:** `.github/workflows/daily.yml` (cron + dispatch),
   `.devcontainer/devcontainer.json` (Go + gh).
9. **M8 — Docs:** `README.md` (пошагово), `config.yml`, финальная `go vet`/`go build`.

---

## Проверка (end-to-end)

1. `go build ./...` и `go vet ./...` — компиляция/статанализ (это я могу прогнать сам).
2. **Локально/в Codespaces (делает пользователь — нужны его api_id/hash/телефон):**
   `go run ./api/cmd/login` → получить session.
3. `SVODKA_*` env + `config.yml` с одним тестовым чатом и `dry_run: true`,
   `window_hours: 2` → `go run ./api/cmd/svodka` → дайджест приходит в Saved Messages.
4. Переключить `dry_run: false`, `target_chat: "@my_group"` → проверить пост в группу.
5. В GitHub: выставить Secrets, нажать **Run workflow** (dispatch) → зелёный прогон,
   дайджест в Telegram, в логах нет содержимого/секретов.
6. Проверить cron (временно `*/10 * * * *`, затем вернуть суточный).

Замечание: реально гонять Telegram/Claude я не могу (нужны личные креды пользователя),
поэтому моя верификация — сборка + `go vet`; функциональный прогон делает пользователь
по README.

---

## Риски / открытые вопросы

1. **Меняющийся IP раннеров** Actions → Telegram может слать «новый вход»; риск
   умеренный (переиспользуем уже авторизованную сессию). Документируем.
2. **`gh secret set` в Codespaces** может не иметь scope на запись секретов → fallback:
   печать session + ручная вставка (заложено в login).
3. **Scheduled workflow отключается** после 60 дней без активности репо → отметить в README.
4. **Приватные чаты без @username** — резолв через dialogs; с юзернеймами проще
   (рекомендация в README).
5. **Объём активных чатов** → стоимость/контекст Claude; решается map-reduce.
