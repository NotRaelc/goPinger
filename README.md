# goPinger

Сервис мониторинга игровых серверов (Minecraft Java/Bedrock, Source/A2S) с live-обновлениями через SSE, кешированием состояний в Redis и защитой от DDoS (глобальный rate-limit по адресу).

## Возможности

- **Поддержка протоколов**: Minecraft Modern, Legacy, Bedrock и Source (A2S).
- **AUTO-определение**: если тип не указан, сервис сам переберёт протоколы и запомнит результат навсегда.
- **Умный кеш**: реальный запрос к серверу происходит не чаще заданного интервала (`MIN_REAL_INTERVAL`). Все остальные запросы получают данные из Redis с пометкой `from_cache`.
- **Live-обновления**: SSE-канал (`/api/events`) мгновенно присылает обновления статусов во все открытые вкладки.
- **Фоновый поллер**: Minecraft опрашивается раз в 3 минуты, остальные — раз в 10 секунд (настраивается).
- **Безопасность**: Argon2id, HttpOnly cookies, защита от SSRF, rate-limit на авторизацию, CSP-заголовки.

---

## 🚀 Быстрый старт (Docker)

Самый простой способ запустить всё сразу (Postgres, Redis, App):

1. Скопируйте пример конфигурации:
   ```bash
   cp .env.example .env
   ```
2. Откройте `.env` и проверьте настройки. По умолчанию всё работает на `http://localhost:8080`.
3. Запустите:
   ```bash
   docker compose up -d --build
   ```
4. Откройте `http://localhost:8080/auth` и зарегистрируйтесь.

> **HTTPS (Let's Encrypt)**: Укажите реальный домен в `.env` (`DOMAIN=example.com`) и запустите с профилем прокси: `docker compose --profile proxy up -d`. Caddy сам получит SSL-сертификат.

---

## 💻 Локальный запуск (без Docker)

**Требования:**
- Go 1.22+
- PostgreSQL 14+
- Redis 6+ (на Windows: Memurai или WSL2)
- [golang-migrate](https://github.com/golang-migrate/migrate) (CLI)

### 1. Настройка переменных

Создайте файл `.env` в корне проекта (программа читает его автоматически при старте):
```env
DATABASE_URL=postgres://postgres:postgres@localhost:5432/gopinger?sslmode=disable
REDIS_URL=redis://localhost:6379/0
```

### 2. Миграции базы данных

В проекте используется `golang-migrate`. Файлы миграций лежат в папке `./migrations`.

Примените миграции:
```bash
migrate -path ./migrations -database "postgres://postgres:postgres@localhost:5432/gopinger?sslmode=disable" up
```

*(Если у вас macOS/Linux и утилита не установлена: `brew install golang-migrate` или `go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest`).*

### 3. Запуск

```bash
go mod tidy
go run .
```

Приложение запустится на `http://localhost:8080`.

---

## ⚙️ Переменные окружения

Все переменные читаются из `.env` или системного окружения. Системные env имеют приоритет.

| Переменная | По умолчанию | Описание |
|---|---|---|
| `DATABASE_URL` | — | Строка подключения PostgreSQL |
| `REDIS_URL` | `redis://localhost:6379/0` | Строка подключения Redis |
| `LISTEN_ADDR` | `:8080` | Адрес и порт HTTP-сервера |
| `PROTOCOL` | `auto` | `auto` / `http` / `https` (влияет на флаг Secure у Cookie) |
| `TRUST_PROXY` | `true` | Доверять заголовку `X-Forwarded-Proto` от прокси |
| `MC_INTERVAL` | `3m` | Интервал фонового опроса Minecraft |
| `OTHER_INTERVAL` | `10s` | Интервал фонового опроса остальных серверов |
| `POLL_TICK` | `5s` | Как часто планировщик проверяет, какие серверы пора опросить |
| `QUERY_TIMEOUT` | `5s` | Таймаут одного сетевого запроса к игровому серверу |
| `MAX_QUERY_CONCURRENT` | `10` | Сколько серверов можно опрашивать одновременно в фоне |
| `MIN_REAL_INTERVAL` | `5s` | Глобальный анти-спам: минимальный интервал реального запроса на 1 адрес |
| `LOCK_WAIT_TIMEOUT` | `3s` | Сколько ждать ответа от другого пользователя, если запрос уже в процессе |
| `MAX_SERVERS_PER_USER` | `200` | Лимит серверов на одного пользователя |
| `SSE_MAX` | `10000` | Глобальный лимит одновременных SSE-соединений |
| `PASSWORD_MIN` | `8` | Минимальная длина пароля |

Полный список с комментариями — в файле `.env.example`.

---

## 🛠 API Reference

Все эндпоинты `/api/*` требуют cookie `token`, который выдается после `POST /login`.

| Метод | Путь | Описание |
|---|---|---|
| `GET` | `/api/me` | Текущий пользователь |
| `GET` | `/api/limits` | Текущие настроенные лимиты |
| `GET` | `/api/servers` | Список серверов с последним состоянием |
| `POST` | `/api/servers` | Добавить сервер |
| `PATCH` | `/api/servers/{id}` | Обновить настройки сервера |
| `DELETE` | `/api/servers/{id}` | Удалить сервер |
| `POST` | `/api/servers/{id}/poll` | Принудительный опрос (учитывает `MIN_REAL_INTERVAL` и кеш) |
| `POST` | `/api/servers/poll-all` | Опрос всех серверов пользователя |
| `POST` | `/api/servers/refresh-stale` | Обновить только те серверы, чей кеш устарел |
| `POST` | `/api/query` | Разовый опрос произвольного адреса (без сохранения в БД) |
| `GET` | `/api/events` | SSE-канал для live-обновлений |

---

## 🩺 Диагностика

Проверка соединения с БД и Redis:
```bash
curl http://localhost:8080/healthz
# Ожидаемый ответ: {"db":true,"redis":true}
```

**Частые проблемы:**
- `Redis init failed` — Redis не запущен. Проверьте `redis-cli ping` (должен быть `PONG`).
- `required table "servers" missing` — не применены миграции (используйте `golang-migrate`).
- `Too many requests` — вы нажали кнопку опроса слишком много раз, ждите (система защищает целевой сервер от спама).