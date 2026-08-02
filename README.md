# Finance API

Небольшой HTTP API на Go для учёта личных доходов и расходов.

Сервис умеет:

- создавать категории;
- сохранять доходы и расходы;
- показывать список операций с фильтрами;
- считать статистику за период на стороне PostgreSQL.

Проект сделан как тестовое задание для API планировщика личных финансов.

## Стек

- Go 1.25
- PostgreSQL 17
- chi для HTTP-роутинга
- pgx без ORM
- goose для миграций
- testcontainers-go для интеграционных тестов

ORM не используется намеренно: в проекте мало сущностей, а запросы со статистикой важнее держать явными и предсказуемыми.

## Запуск

Основной способ запуска - через Docker Compose:

```bash
docker compose up --build
```

После старта API доступен на `http://localhost:8080`.

Compose сам поднимает PostgreSQL, ждёт готовности базы, применяет миграции и только потом запускает приложение.

Проверить, что сервис жив:

```bash
curl -s localhost:8080/healthz
```

Загрузить демо-данные:

```bash
docker compose exec -T postgres psql -U finance -d finance < scripts/seed.sql
```

Остановить сервис и удалить данные:

```bash
docker compose down -v
```

Swagger UI можно поднять отдельно:

```bash
docker compose --profile docs up
```

После этого документация будет на `http://localhost:8081`.

## Настройки

Главная переменная окружения - `DATABASE_URL`. Для Docker Compose значения уже прописаны, поэтому `.env` для обычного запуска не нужен.

Также можно настроить:

- `HTTP_ADDR` - адрес API, по умолчанию `:8080`;
- `DB_MAX_CONNS` и `DB_MIN_CONNS` - размер пула соединений;
- `HTTP_READ_TIMEOUT`, `HTTP_WRITE_TIMEOUT`, `HTTP_IDLE_TIMEOUT`;
- `SHUTDOWN_TIMEOUT`;
- `LOG_LEVEL`;
- `LOG_FORMAT`.

## Миграции

Миграции встроены в бинарь через `embed.FS`. В Docker Compose их применяет отдельный сервис `migrator`.

Запустить вручную:

```bash
docker compose run --rm migrator up
docker compose run --rm migrator status
```

Локально доступны команды:

```bash
make migrate-up
make migrate-down
make migrate-status
make migrate-reset
```

## API

Базовый префикс: `/api/v1`.

Во все пользовательские запросы нужно передавать заголовок `X-User-Id` с UUID пользователя.

Основные ручки:

- `POST /api/v1/categories` - создать категорию;
- `GET /api/v1/categories` - получить категории;
- `POST /api/v1/operations` - добавить доход или расход;
- `GET /api/v1/operations` - получить операции с фильтрами;
- `GET /api/v1/stats` - получить статистику за период;
- `GET /healthz` - проверить состояние сервиса.

Полная спецификация лежит в `api/openapi.yaml`.

## Деньги и даты

Все суммы передаются целыми числами в копейках. Например, `150000` означает `1500.00`.

Сумма операции всегда положительная. Доход это или расход, определяется полем `type`: `income` или `expense`.

Параметры `from` и `to` принимают дату `YYYY-MM-DD` или timestamp RFC 3339. Обе границы включительные.

Если передать `to=2026-07-31`, сервис включит весь день 31 июля до конца суток.

Для `/stats` обе границы обязательны.

## Пример

```bash
export USER_ID=11111111-1111-4111-8111-111111111111
```

Создать категорию:

```bash
curl -s -X POST localhost:8080/api/v1/categories \
  -H "X-User-Id: $USER_ID" \
  -H "Content-Type: application/json" \
  -d '{"name":"Продукты"}'
```

Добавить расход:

```bash
curl -s -X POST localhost:8080/api/v1/operations \
  -H "X-User-Id: $USER_ID" \
  -H "Content-Type: application/json" \
  -d '{"category_id":1,"type":"expense","amount":320000,"comment":"магазин"}'
```

Получить расходы за август:

```bash
curl -s -H "X-User-Id: $USER_ID" \
  "localhost:8080/api/v1/operations?from=2026-08-01&to=2026-08-31&type=expense"
```

Получить статистику:

```bash
curl -s -H "X-User-Id: $USER_ID" \
  "localhost:8080/api/v1/stats?from=2026-08-01&to=2026-08-31"
```

Ответ статистики содержит общий доход, общий расход, баланс и расходы по категориям.

## Ошибки

Ошибки возвращаются в одном формате:

```json
{
  "error": {
    "code": "validation_error",
    "message": "request body failed validation",
    "details": [
      { "field": "amount", "message": "must be a positive integer in minor units (kopecks), got -1" }
    ]
  }
}
```

Если категория не существует или принадлежит другому пользователю, сервис отвечает одинаково. Так нельзя перебором узнать чужие категории.

## Структура проекта

- `cmd/api` - запуск HTTP-сервера;
- `cmd/migrator` - запуск миграций;
- `internal/config` - конфигурация;
- `internal/domain` - сущности и правила валидации;
- `internal/service` - бизнес-логика;
- `internal/repository/postgres` - SQL-запросы;
- `internal/transport/http` - роутер, DTO и обработка ошибок;
- `migrations` - SQL-миграции;
- `api/openapi.yaml` - OpenAPI-спецификация;
- `scripts/seed.sql` - демо-данные.

## Важные решения

Категории принадлежат конкретному пользователю. Это проверяется не только в коде, но и на уровне базы: операция ссылается на пару `(category_id, user_id)`. Поэтому нельзя случайно создать расход в чужой категории.

Статистика считается одним SQL-запросом в PostgreSQL. Go только вызывает запрос и отдаёт результат наружу.

Валидация разделена на три уровня:

- транспорт проверяет JSON, заголовки и query-параметры;
- домен проверяет бизнес-правила;
- база защищает данные через `CHECK`, `UNIQUE` и `FOREIGN KEY`.

Время в ответах приводится к UTC, чтобы результат не зависел от настроек хоста.

## Что не делалось

В проект намеренно не добавлялись:

- авторизация и аутентификация;
- frontend;
- мультивалютность;
- интеграции с банками;
- очереди;
- кеширование;
- регулярные платежи;
- пагинация;
- метрики, трассировка, rate limiting и CORS.

Это осталось за рамками тестового задания.
