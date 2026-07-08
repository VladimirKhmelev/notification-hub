# Notification Hub

Self-hosted хаб уведомлений. Источники событий (uptime-мониторинг, RSS, GitHub, курсы валют) публикуют события в единый пайплайн. События доставляются живой лентой через WebSocket, фолбэк — FCM push на телефон.

## Архитектура

```
браузер / мобильный клиент
        │
        ├── HTTP REST ──► api          (порт 8080)
        └── WebSocket ──► ws-gateway   (порт 8081)
                              │
                         LISTEN/NOTIFY
                              │
                         PostgreSQL ◄── event-collector-uptime
                                   ◄── event-collector-rss      (Спринт 2)
                                   ◄── event-collector-github   (Спринт 2)
                                   ◄── event-collector-price    (Спринт 2)
```

## Быстрый старт

```bash
git clone https://github.com/VladimirKhmelev/notification-hub
cd notification-hub
make up
```

Открой [http://localhost:8080](http://localhost:8080).

## Сервисы

| Сервис | Порт | Роль |
|--------|------|------|
| `api` | 8080 | REST API + отдаёт UI |
| `ws-gateway` | 8081 | WebSocket, live-доставка событий |
| `event-collector-uptime` | — | Мониторинг HTTP-доступности URL |

## REST API

### Источники

```
POST   /sources          — создать источник
GET    /sources          — список источников
DELETE /sources/{id}     — удалить источник
```

Пример создания uptime-источника:

```bash
curl -X POST http://localhost:8080/sources \
  -H "Content-Type: application/json" \
  -d '{"type":"uptime","name":"GitHub","config":{"url":"https://github.com"}}'
```

### События

```
GET /events?limit=50&offset=0   — история событий (новые первыми)
```

### WebSocket

```
ws://localhost:8081/ws
```

При подключении клиент получает новые события в реальном времени в формате JSON:

```json
{
  "id": 1,
  "source_id": 2,
  "title": "GitHub is DOWN",
  "body": "GitHub (https://github.com) is not responding",
  "priority": "high",
  "created_at": "2026-07-08T18:00:00Z",
  "read_at": null
}
```

## Конфиг источников

### uptime

```json
{ "url": "https://example.com", "expect": 200 }
```

`expect` — ожидаемый HTTP-статус, по умолчанию 200.

## Makefile

```bash
make up      # поднять все сервисы
make down    # остановить
make logs    # следить за логами
make build   # собрать бинарники локально
```

## Переменные окружения

| Переменная | Описание | По умолчанию |
|------------|----------|--------------|
| `DATABASE_URL` | Postgres DSN | `postgres://hub:hub@localhost:5432/hub?sslmode=disable` |

## Стек

- **Go** — бэкенд
- **PostgreSQL** — хранилище событий и источников, шина через `LISTEN/NOTIFY`
- **nhooyr.io/websocket** — WebSocket
- **golang-migrate** — миграции БД
- **Docker Compose** — инфраструктура
