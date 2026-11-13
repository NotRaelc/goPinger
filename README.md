# goPinger

Сервис мониторинга игровых серверов (Minecraft Java/Bedrock, Source/A2S) с
веб-интерфейсом, live-обновлениями через SSE, кешированием состояний в Redis и
глобальным rate-limit по адресу.

## Возможности

- **Поддержка протоколов**: Minecraft Modern (1.7+), Legacy (<1.7), Bedrock, Source (A2S).
- **AUTO-определение типа** при первом запросе; результат кешируется навсегда.
- **Планировщик** опрашивает серверы в фоне: Minecraft — раз в 3 минуты, остальные — раз в 10 секунд (настраивается).
- **Глобальный rate-limit по адресу**: сколько бы пользователей ни нажали «ОПРОС», к серверу уходит не более 1 запроса за `MIN_REAL_INTERVAL`. Остальные получают кеш с флагом `from_cache`.
- **Live-обновления**: SSE-канал `/api/events`, любое изменение состояния мгновенно приходит во все открытые вкладки пользователя.
- **Иконки серверов**: Minecraft — favicon из ответа сервера, Source — по `appid` из локальной папки `frontend/static/images/games/<appid>.png`, иначе — дефолтная текстура.
- **Безопасность**: Argon2id-хеши, токены с TTL, кеширование пользователей, проверки владения сервером до мутаций, защита от паники, structured logging.

---

## Требования

| Компонент | Версия | Зачем |
|-----------|--------|-------|
| **Go** | 1.22+ | сборка проекта |
| **PostgreSQL** | 14+ | хранение пользователей и серверов |
| **Redis** | 6+ | кеш, rate-limit, планировщик |

Redis официально не поддерживает Windows — см. раздел [Установка Redis на Windows](#установка-redis-на-windows).

---

## Установка

### 1. Клонирование и зависимости

```bash
git clone <repo> goPinger
cd goPinger

go mod tidy
```

Основные зависимости:

- `github.com/jackc/pgx/v5` — PostgreSQL драйвер
- `github.com/redis/go-redis/v9` — Redis клиент
- `github.com/go-playground/validator/v10` — валидация
- `github.com/woozymasta/a2s` — Source (A2S) протокол

### 2. PostgreSQL

**Ubuntu/Debian:**
```bash
sudo apt install postgresql
sudo systemctl enable --now postgresql
sudo -u postgres psql -c "CREATE DATABASE gopinger;"
```

**macOS (Homebrew):**
```bash
brew install postgresql@16
brew services start postgresql@16
createdb gopinger
```

**Windows:** установи [PostgreSQL installer](https://www.postgresql.org/download/windows/), создай БД `gopinger` через pgAdmin или `psql`.

### 3. Redis

**Ubuntu/Debian:**
```bash
sudo apt install redis-server
sudo systemctl enable --now redis-server
```

**macOS:**
```bash
brew install redis
brew services start redis
```

**Docker (любая ОС):**
```bash
docker run -d --name gopinger-redis -p 6379:6379 redis:7-alpine
```

#### Установка Redis на Windows

Официальной сборки нет. Три рабочих варианта:

- **Memurai** (рекомендуется, нативная Windows-служба):
  1. Скачай Developer Edition с [memurai.com](https://www.memurai.com/).
  2. Установи — запустится как служба Windows на порту `6379`.
  3. Проверь: `memurai-cli.exe ping` → должен ответить `PONG`.

- **WSL2** (как в официальной документации):
  ```powershell
  wsl --install
  # затем в Ubuntu-терминале:
  sudo apt update && sudo apt install redis-server
  sudo service redis-server start
  redis-cli ping
  ```

- **Docker Desktop:** `docker run -d -p 6379:6379 redis:7-alpine`.

Если порт отличается от `6379` — задай `REDIS_URL` (см. ниже).

### 4. Миграции

```bash
psql "$DATABASE_URL" -f migrations/000001_init.up.sql
psql "$DATABASE_URL" -f migrations/000002_servers_impl.up.sql
```

Если `psql` не в `PATH` — используй полный путь (`C:\Program Files\PostgreSQL\18\bin\psql.exe` на Windows) или pgAdmin.

После применения должны существовать три таблицы:

```sql
SELECT table_name FROM information_schema.tables WHERE table_schema = 'public';
-- users, servers, server_states
```

---

## Настройка

Всё через переменные окружения. Значения по умолчанию подходят для локального запуска.

### Основные

| Переменная | По умолчанию | Описание |
|------------|--------------|----------|
| `DATABASE_URL` | — (обязательна) | `postgres://user:pass@host:5432/dbname` |
| `REDIS_URL` | `redis://localhost:6379/0` | подключение к Redis |
| `LISTEN_ADDR` | `:8080` | адрес HTTP-сервера |

### Планировщик и опрос

| Переменная | По умолчанию | Границы | Описание |
|------------|--------------|---------|----------|
| `MC_INTERVAL` | `3m` | 10s–1h | интервал фонового опроса Minecraft |
| `OTHER_INTERVAL` | `10s` | 5s–1h | интервал фонового опроса прочих серверов |
| `POLL_TICK` | `5s` | 1s–1m | как часто планировщик просыпается |
| `QUERY_TIMEOUT` | `5s` | 1s–30s | таймаут одного запроса |
| `MAX_QUERY_CONCURRENT` | `10` | 1–200 | сколько адресов опрашиваются одновременно |
| `MIN_REAL_INTERVAL` | `5s` | 1s–1m | глобальный rate-limit: минимальный интервал реального запроса на адрес |
| `LOCK_WAIT_TIMEOUT` | `3s` | 0.5s–30s | сколько ждать кеш, если адрес занят другим опросом |
| `POLL_LOCK_TTL` | `30s` | — | TTL лока поллера на сервер |

### Кеш

| Переменная | По умолчанию | Описание |
|------------|--------------|----------|
| `USER_CACHE_TTL` | `5m` | TTL кеша пользователя по токену |

### Пример для локальной разработки (PowerShell)

```powershell
$env:DATABASE_URL="postgres://postgres:postgres@localhost:5432/gopinger"
$env:REDIS_URL="redis://localhost:6379/0"
$env:LISTEN_ADDR=":8080"
$env:MC_INTERVAL="1m"          # чаще, чтобы видеть обновления
$env:MIN_REAL_INTERVAL="2s"
go run .
```

### Пример `.env` (bash)

```bash
export DATABASE_URL="postgres://postgres:postgres@localhost:5432/gopinger"
export REDIS_URL="redis://localhost:6379/0"
export LISTEN_ADDR=":8080"
export MC_INTERVAL="3m"
export OTHER_INTERVAL="10s"
```

---

## Запуск

### Режим разработки

```bash
go run .
```

Ожидаемый вывод:

```
level=INFO msg="HTTP listening" addr=:8080
```

### Сборка бинарника

```bash
go build -o gopinger .
./gopinger
```

**Windows:**
```powershell
go build -o gopinger.exe .
.\gopinger.exe
```

### Docker

Если нужен образ:

```dockerfile
# Dockerfile
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY . .
RUN go mod download && CGO_ENABLED=0 go build -o /out/gopinger .

FROM alpine:3.20
WORKDIR /app
COPY --from=build /out/gopinger .
COPY frontend ./frontend
COPY migrations ./migrations
EXPOSE 8080
ENTRYPOINT ["/app/gopinger"]
```

```bash
docker build -t gopinger .
docker run --rm -p 8080:8080 \
  -e DATABASE_URL="postgres://user:pass@host.docker.internal:5432/gopinger" \
  -e REDIS_URL="redis://host.docker.internal:6379/0" \
  gopinger
```

---

## Использование

1. Открой `http://localhost:8080/auth`.
2. Зарегистрируйся (вкладка **РЕГИСТРАЦИЯ**) — логин латиницей, пароль от 6 символов.
3. На главной странице:
   - **+ ДОБАВИТЬ** — добавить сервер. Укажи адрес (`host` или `host:port`), тип (`AUTO` определит автоматически) и метку.
   - **ОПРОС** — принудительный запрос. Если по адресу недавно (в пределах `MIN_REAL_INTERVAL`) уже опрашивали — вернётся кеш с бейджем **CACHE**.
   - **ИЗМ** / **✕** — редактировать / удалить.
   - **⚙ КОЛОНКИ** — включить/выключить столбцы таблицы.
   - Клик по строке — развернуть детальную карточку с полным payload.
4. Индикатор **● LIVE** в шапке — SSE-соединение активно; при обновлении сервера строка мигает зелёным.

### Иконки Source-серверов

Для A2S-серверов картинка берётся из `frontend/static/images/games/<appid>.png`. Положи файлы с нужными `appid`:

```
frontend/static/images/games/730.png   # CS2
frontend/static/images/games/440.png   # Team Fortress 2
frontend/static/images/games/550.png   # L4D2
```

Если файла нет — фронт покажет дефолтную розово-чёрную текстуру. Узнать `appid` можно в ответе `/api/query` (`payload.game_id`).

---

## API

Все `/api/*` используют cookie `token`. Аутентификация — `POST /login` с `{username,password}`.

| Метод | Путь | Описание |
|-------|------|----------|
| `GET` | `/api/me` | текущий пользователь |
| `GET` | `/api/servers` | список серверов с последним состоянием |
| `POST` | `/api/servers` | создать сервер; возвращает `ServerDTO` с `state.from_cache` |
| `PATCH` | `/api/servers/{id}` | обновить |
| `DELETE` | `/api/servers/{id}` | удалить |
| `POST` | `/api/servers/{id}/poll` | принудительный опрос (учитывает rate-limit) |
| `POST` | `/api/query` | разовый опрос произвольного адреса (без сохранения) |
| `GET` | `/api/events` | SSE-канал обновлений |

Пример:

```bash
# login (сохраняем куки)
curl -c cookies.txt -X POST http://localhost:8080/login \
  -H "Content-Type: application/json" \
  -d '{"username":"alex","password":"secret12"}'

# добавить сервер
curl -b cookies.txt -X POST http://localhost:8080/api/servers \
  -H "Content-Type: application/json" \
  -d '{"address":"mc.hypixel.net","type":0,"label":"prod"}'

# список с состояниями
curl -b cookies.txt http://localhost:8080/api/servers | jq
```

---

## Архитектура (кратко)

```
cmd/main.go          — точка входа, init БД/Redis, graceful shutdown
config.go            — все env-переменные и их границы
cache.go             — Redis: типы, состояния, таймеры, lastpoll, lock
queries.go           — fetchState (rate-limit + lock + кеш), bootstrap
poller.go            — фоновый планировщик (конкурентный)
servers.go           — CRUD + buildDTO + resolveIconURL
states.go            — сохранение последнего состояния в Postgres
events.go            — SSE-хаб (publishState / subscribe)
handlers.go          — HTTP-эндпоинты
middleware.go        — логирование, auth, CORS, recover
protect.go           — guard-функции + getServerForUser
frontend/            — HTML/CSS/JS без сборки
```

### Поток данных при запросе к серверу

```
apiPollServer / poller.pollServer
   └─> fetchState
         ├─ есть lastpoll < MIN_REAL_INTERVAL?  ──> отдать кеш (from_cache=true)
         ├─ взять lock на адрес  ─> другой уже опрашивает?  ──> ждать кеш
         ├─ реальный запрос через query.QueryServerOpts
         ├─ сохранить в gp:state:<addr> с TTL = интервал типа
         └─ publishState  ─> SSE ─> все вкладки пользователя
```

### Ключи Redis

| Ключ | TTL | Назначение |
|------|-----|------------|
| `gp:type:<addr>` | ∞ | определённый тип сервера |
| `gp:state:<addr>` | interval | последнее успешное состояние |
| `gp:lastpoll:<addr>` | 24h | unix ms последнего реального запроса |
| `gp:lock:<addr>` | QueryTimeout+5s | защита от параллельных опросов |
| `gp:next:<id>` | interval | когда поллеру опрашивать сервер `id` |
| `gp:user:<token>` | UserCacheTTL | кеш пользователя по токену |

Удалить всё разом: `redis-cli --scan --pattern 'gp:*' | xargs redis-cli del`.

---

## Диагностика

### `Redis init failed`

Redis не запущен. На Windows:
```powershell
Get-Service Memurai
Start-Service Memurai
```

### `required table "servers" missing`

Не применена миграция `000002`. Проверь:
```sql
\c gopinger
\dt
```

### `HTTP listening`, но `/app` редиректит на `/auth`

Так и должно быть без cookie `token`. Зарегистрируйся через `/auth` → **РЕГИСТРАЦИЯ**.

### Индикатор OFFLINE

SSE-соединение не установилось. Проверь консоль браузера (`F12 → Network → events`). Сервер при недоступности автоматически переподключится через 3 секунды.

### Сервер всегда OFFLINE

Запусти разовый запрос вручную — ошибка будет в детальной карточке:
```bash
curl -b cookies.txt -X POST http://localhost:8080/api/query \
  -H "Content-Type: application/json" \
  -d '{"address":"play.example.com","type":0}' | jq
```

Частые причины: блокировка loopback (защита `query` от SSRF — `localhost` и `127.0.0.1` запрещены), неверный порт, фаервол.

### Хочу посмотреть что лежит в Redis

```bash
# Memurai / Redis CLI
redis-cli keys "gp:*"
redis-cli ttl "gp:next:1"
redis-cli get "gp:type:mc.hypixel.net"
```

### Данные удалятся при закрытии программы?

- **PostgreSQL** — нет, пользователи и серверы хранятся постоянно.
- **Redis** — Memurai делает RDB-снапшоты, обычно кеш переживает перезапуск. Но проектно он считается одноразовым: при потере кеша максимум будет один лишний реальный запрос.
- **Сессии** — cookie `token` живёт 400 дней, токен хранится в `users.token`.

---

## Разработка

```bash
# сборка
go build ./...

# тесты
go test ./...

# только быстрые (без live-запросов)
go test -short ./...
```

Live-тесты в пакете `query` ходят на публичные серверы (`demo.mcstatus.io`). Пропускаются через `-short` или при недоступности сети.

---

## Лицензия

См. `LICENSE` (если есть), либо уточняй у автора репозитория.