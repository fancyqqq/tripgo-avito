# TripGo: trip-service

Сервис поездок на Go. В ЛР3 добавлены сохранение координат, маршрут поездки и фоновый запрос координат через Push Service. Возможности ЛР1–2 сохранены: PostgreSQL, транзакции, идемпотентность создания поездки, слои и OpenTelemetry.

## Запуск

Нужны Go 1.27.1+, Docker, make и [tripgoctl](https://github.com/course-go-autumn-2026/course-infra). Для установки линтера нужен curl, для нагрузочного скрипта — Python 3 и psql. goose, oapi-codegen и mockgen закреплены в `go.mod`.

```bash
tripgoctl cluster start
tripgoctl environment start
tripgoctl connect
make migrate
make run
```

[environment.toml](environment.toml) выбирает окружение ЛР3. Makefile читает созданный `tripgoctl` файл `.env`. Адреса компонентов и доступ к Grafana показывает `tripgoctl connect`. Миграции применяются отдельно от запуска приложения.

## API

| Метод | Путь | Назначение |
|---|---|---|
| POST | `/api/v1/trips` | создать поездку |
| GET | `/api/v1/trips/{tripId}` | получить поездку |
| POST | `/api/v1/trips/{tripId}/finish` | завершить поездку |
| POST | `/api/v1/trips/{tripId}/positions` | сохранить координату |
| GET | `/api/v1/trips/{tripId}/positions` | получить маршрут |
| GET | `/health` | liveness |
| GET | `/ready` | проверка БД; при недоступности — 503 |

Контракт — [trip-service.openapi.yaml](contracts/openapi/trip-service.openapi.yaml). Ошибки возвращаются в `application/problem+json` со стабильным полем `code`.

Координата сохраняется с ответом `201`; отсутствующая поездка даёт `404`, завершённая — `409`, неверные координаты или дата более чем на минуту в будущем — `400`. Маршрут доступен после завершения, сортируется по `recorded_at, id`; без точек возвращается `{"positions":[]}`.

Для существующей активной поездки укажи её UUID в `TRIP_ID`:

```bash
RECORDED_AT=$(date -u +%FT%TZ)
curl -i -X POST "localhost:8080/api/v1/trips/$TRIP_ID/positions" \
  -H 'Content-Type: application/json' \
  -d "{\"coordinates\":{\"latitude\":59.935,\"longitude\":30.335},\"recorded_at\":\"$RECORDED_AT\"}"
curl -i "localhost:8080/api/v1/trips/$TRIP_ID/positions"
```

Вставка точки и обновление `last_position_at` выполняются в одной транзакции. `GREATEST` сохраняет максимальное время при приходе точек не по порядку, `FOR UPDATE` согласует запись с завершением поездки.

Создание поездки поддерживает `Idempotency-Key` на 24 часа: первый запрос — `201`, повтор с теми же данными — `200`, с другими — `409 idempotency_conflict`.

## Опрос устаревших поездок

```text
тикер → SQL-выборка → ограниченный канал → фиксированные воркеры → Push Service
POST positions → COMMIT → отмена ожидающей задачи
```

Сканер последовательно выбирает активные поездки, у которых `COALESCE(last_position_at, started_at)` старше порога. Время сравнивается в SQL; первыми идут самые давно молчащие поездки. Для выборки создан частичный индекс `CONCURRENTLY`.

| Результат постановки | Поведение |
|---|---|
| `queued` | задача поставлена |
| `duplicate` | поездка уже ожидает обработки или обрабатывается |
| `dropped` | очередь заполнена; постановка не блокируется, пишется Warn |

Реестр задач защищён мьютексом. После коммита координаты ожидающая задача отменяется, а воркер пропускает её при чтении канала. Уже начатая отправка завершается по своему контексту. У каждой постановки собственная идентичность, поэтому старый отменённый элемент не затрагивает новую задачу той же поездки.

Гейтвей отправляет `REQUEST_POSITION`: получатель — водитель, в данных — `trip_id`. Используется собственный HTTP-клиент с таймаутами, успех — `202`, тело ответа закрывается. Ошибка не останавливает воркер.

Время отправки не хранится: если координата не пришла, поездку снова обнаружит следующий обход. Выборка выполняется без LIMIT, очередь — FIFO, дедупликация локальна процессу. Батчевое сканирование, приоритетная очередь и ретраи в ЛР3 не реализованы.

По SIGINT/SIGTERM сначала завершаются HTTP-запросы, затем сканер и очередь, закрываются клиенты и БД, отправляется телеметрия. Бюджет `SHUTDOWN_TIMEOUT` общий. По его истечении операции отменяются; остаток очереди отбрасывается, поездки остаются в БД для следующего запуска.

## Конфигурация

Обязательные переменные задаются в `.env`:

| Переменная | Пример |
|---|---|
| `HTTP_ADDR` | `:8080` |
| `LOG_LEVEL` | `info` |
| `SHUTDOWN_TIMEOUT` | `10s` |
| `DATABASE_URL` | адрес из tripgoctl |
| `DATABASE_MAX_CONNS`, `DATABASE_MIN_CONNS` | `10`, `2` |
| `DATABASE_MAX_CONN_LIFETIME` | `30m` |
| `DATABASE_CONNECT_TIMEOUT`, `DATABASE_QUERY_TIMEOUT` | `5s`, `3s` |
| `OTEL_SERVICE_NAME` | `trip-service` |
| `PUSH_HTTP_URL` | адрес из tripgoctl |

Параметры ЛР3 со значениями по умолчанию:

| Переменная | Значение | Назначение |
|---|---|---|
| `POSITION_CHECK_INTERVAL` | `30s` | период обхода |
| `POSITION_STALE_AFTER` | `60s` | порог устаревания |
| `POSITION_WORKERS` | `4` | число воркеров |
| `POSITION_QUEUE_SIZE` | `100` | ёмкость канала |
| `PUSH_REQUEST_TIMEOUT` | `1s` | таймаут отправки |

HTTP-таймауты по умолчанию: `HTTP_READ_TIMEOUT=10s`, `HTTP_READ_HEADER_TIMEOUT=5s`, `HTTP_WRITE_TIMEOUT=30s`, `HTTP_IDLE_TIMEOUT=60s`. Неположительные размеры и таймауты отклоняются при старте.

OpenTelemetry использует стандартные `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_EXPORTER_OTLP_PROTOCOL`, `OTEL_RESOURCE_ATTRIBUTES`, `OTEL_TRACES_SAMPLER`. Версия ресурса по умолчанию — `dev`, идентификатор экземпляра — UUID; оба переопределяются через `OTEL_RESOURCE_ATTRIBUTES`. Пример переменных — [.env.example](.env.example).

## Наблюдаемость и нагрузка

Трейсы и метрики отправляются через OTLP/HTTP. Спан воркера связан с проверкой через span link, HTTP-клиент передаёт `traceparent`. Логи — JSON в stdout с `service`, `trace_id` и `span_id`.

Метрики ЛР3: `tripgo.positions.saved`, `tripgo.stale_checks{result}`, `tripgo.position_requests.queue.size`, `tripgo.position_requests.workers.active`, `tripgo.position_requests{result}`, `tripgo.position_requests.duration`. Gauge читаются из текущего состояния пула, длительность измеряется в секундах.

Импортируй [дашборд](deploy/grafana/positions.json) в Grafana с источником `prometheus`. В окружении курса экземпляр приложения выбирается по метке `exported_instance`.

```bash
make test-position-pool-load
```

Скрипт создаёт отдельную схему и процесс: 200 поездок, очередь 10, четыре воркера, задержка пушей 5 с и таймаут 1 с. Проверяет переполнение, доступность HTTP и возвращение пула к нулю после разгрузки. Затем удаляет схему и сбрасывает задержку заглушки. Запускай отдельно от остальных тестов той же заглушки.

Результаты: [JSON](reports/positions/worker-pool-load.json), [отчёт](reports/positions/worker-pool-load.txt), [Grafana](reports/positions/worker-pool-grafana.png). Скрипт обновляет JSON; изображения фиксируют сохранённый прогон.

## Архитектура и проверки

`domain` содержит правила, `usecase` — сценарии, `repository` — SQL, `http` — транспорт. Сканер и пул находятся в `internal/worker/positions`, гейтвей — в `internal/gateway/push`, зависимости собираются в `internal/container`. Интерфейсы объявляет потребитель, правила слоёв проверяет depguard.

```bash
make generate
make build
make lint
GOFLAGS=-count=2 make test
make test-integration
make cover
```

Тесты проверяют атомарность, старые точки, сортировку, SQL-выборку, дедупликацию, отмену, переполнение, фиксированный параллелизм и остановку. Используются `-race`, `goleak`, моки и управляемый тикер. Интеграционным тестам нужны PostgreSQL и Push Service.

## Docker и материалы ЛР3

```bash
make docker-build
make migrate
make docker-run
```

Образ `trip-service:lab3` запускается от nonroot в сети `kind`. Через узел `tripgo-local-control-plane` используются NodePort: БД `30303`, Push HTTP `30308`, OTLP/HTTP `30306`. Адреса можно переопределить переменными `DOCKER_DATABASE_URL`, `DOCKER_PUSH_HTTP_URL`, `DOCKER_OTLP_ENDPOINT`; внешний порт — через `DOCKER_HTTP_PORT`.

[EXPLAIN на 20 000 поездках](reports/positions/stale-trips-explain.txt)
