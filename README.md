# TripGo: trip-service

Сервис поездок учебного проекта TripGo (курс «Разработка микросервисов на Go»). В лабораторной работе 1 появились HTTP API по OpenAPI-контракту, хранение в PostgreSQL, миграции и менеджер транзакций. В лабораторной работе 2 сервис разложен по слоям, покрыт тестами, проверяется линтером и отдаёт трейсы, метрики и структурированные логи через OpenTelemetry.

## Что умеет сервис

| Метод | Путь | Что делает |
|---|---|---|
| `POST` | `/api/v1/trips` | создаёт поездку в статусе `active` |
| `GET` | `/api/v1/trips/{tripId}` | возвращает поездку |
| `POST` | `/api/v1/trips/{tripId}/finish` | завершает поездку |
| `GET` | `/health` | liveness, в базу не ходит |
| `GET` | `/ready` | readiness, проверяет базу и отвечает `503`, если она недоступна |

Ошибки отдаются в `application/problem+json` по RFC 9457. Клиенту нужно смотреть на поле `code`: `invalid_request`, `trip_not_found`, `trip_completed`, `driver_busy` или `internal_error`.

## Требования

- Go 1.27.1 или новее (версия указана в `go.mod`)
- Docker
- [`tripgoctl`](https://github.com/course-go-autumn-2026/course-infra)
- make
- [golangci-lint](https://golangci-lint.run/) v2 для `make lint`
- mockgen для `make generate`: `go install go.uber.org/mock/mockgen@latest`
- psql, если нужно смотреть в базу руками

goose и oapi-codegen отдельно ставить не нужно. Они подключены к модулю через `go get -tool` и запускаются как `go tool goose` и `go tool oapi-codegen`.

## Запуск

```bash
tripgoctl cluster start        # один раз на машине
tripgoctl environment start    # PostgreSQL, OTel Collector, Jaeger, Prometheus, Grafana и файл .env
make migrate
make run
```

`tripgoctl environment start` создаёт в корне файл `.env` с адресом базы, коллектора и остальными переменными. Makefile подключает его сам, экспортировать ничего не нужно. Сервис слушает адрес из `HTTP_ADDR` (в сгенерированном `.env` это `:8080`) и останавливается по Ctrl+C или `SIGTERM`. При остановке он дожидается текущих запросов и отправляет в коллектор накопленные трейсы и метрики.

Адреса Jaeger и Grafana печатает `tripgoctl` при запуске окружения.

## Цели Makefile

| Цель | Что делает |
|---|---|
| `make run` | запускает сервис |
| `make migrate` | накатывает миграции |
| `make migrate-down` | откатывает все миграции |
| `make migrate-status` | показывает, какие миграции применены |
| `make generate` | генерирует `internal/generated/api.gen.go` из контракта и моки через `go generate` |
| `make lint` | `golangci-lint run ./...` с конфигом курса |
| `make fmt` | форматирует код (gofumpt, gci) |
| `make test` | unit-тесты с `-race` и покрытием, база не нужна |
| `make test-integration` | все тесты вместе с интеграционными, нужна поднятая база |
| `make cover` | суммарное покрытие пакетов `usecase` и `domain` |
| `make docker-build` | собирает образ `trip-service:lab1` |
| `make docker-run` | запускает образ рядом с PostgreSQL из `tripgoctl` |

## Архитектура

### Слои

```
            HTTP
              │
   ┌──────────▼───────────┐
   │ internal/http        │  разбор запроса, вызов usecase, JSON или problem+json
   └──────────┬───────────┘
              │ интерфейс useCase
   ┌──────────▼───────────┐
   │ internal/usecase     │  сценарий: что делать, в каком порядке, в какой транзакции
   └───┬──────────────┬───┘
       │              │ интерфейсы tripRepository, txManager, tripMetrics
       │   ┌──────────▼───────────┐
       │   │ internal/repository  │  SQL через squirrel и pgx
       │   └──────────┬───────────┘
       │              │
   ┌───▼──────────────▼───┐
   │ internal/domain      │  поездка и её правила, ни от чего не зависит
   └──────────────────────┘
```

- **domain** (`internal/domain/trip`) хранит сущность поездки и правила. `trip.New` проверяет входные данные и создаёт активную поездку, `Trip.Finish` переводит её в `completed` или возвращает `ErrTripCompleted`. Здесь же объявлены доменные ошибки. Пакет не импортирует ни HTTP, ни базу, ни телеметрию.
- **usecase** (`internal/usecase/trip/{create,get,finish}`) описывает одну бизнес-операцию на пакет. Usecase решает, в каком порядке вызвать домен и репозиторий и что выполнить в одной транзакции. Про `net/http`, `pgx` и `squirrel` он не знает.
- **repository** (`internal/repository/trip`) переводит доменные структуры в строки таблиц и обратно, ошибки PostgreSQL превращает в доменные (`23505` на индексе водителя становится `ErrDriverBusy`).
- **http** (`internal/http`) принимает запрос, вызывает usecase и отдаёт ответ. Какой HTTP-код соответствует какой ошибке, решается в одном месте, в `internal/http/domainerror`.
- **infra** (`internal/infra`) содержит пул соединений, менеджер транзакций, логгер и настройку OpenTelemetry.
- **container** (`internal/container`) создаёт все объекты и связывает их. Только этот пакет знает конкретные типы всех слоёв, остальные знают друг о друге через интерфейсы.

### Интерфейсы объявляет потребитель

Каждый usecase и каждая ручка объявляют в своём `contract.go` небольшой неэкспортируемый интерфейс ровно из тех методов, которые им нужны. Usecase `get` просит у репозитория только `GetByID`, usecase `finish` — `GetByIDForUpdate`, `Update` и `AddStatusChange`. Репозиторий об этих интерфейсах не знает, в Go тип подходит к интерфейсу автоматически.

Это даёт две вещи. Usecase и хендлеры тестируются с моками без базы и без сети: моки генерирует `mockgen` по директиве `//go:generate` в том же `contract.go`. И слой не тянет за собой зависимости, которые ему не нужны.

### Правила зависимостей проверяет линтер

В `.golangci.yml` настроен `depguard`:

| Слой | Не может импортировать |
|---|---|
| `internal/domain` | `net/http`, chi, pgx, squirrel, OpenTelemetry |
| `internal/usecase` | `net/http`, chi, pgx, squirrel |
| `internal/http` | `internal/repository`, `internal/infra` |

Последнее правило добавлено к конфигу курса: транспорт не должен обходить usecase и ходить в базу напрямую. Если кто-то нарушит правило, `make lint` упадёт.

### Как проходит запрос

`POST /api/v1/trips/{tripId}/finish`:

1. Middleware: Recover → Tracing → Logging → Metrics (подробнее в разделе «Наблюдаемость»).
2. Сгенерированный роутер разбирает `tripId`. Если это не UUID, ответ сразу `400 invalid_request`.
3. `http/trip/finish` вызывает usecase.
4. `usecase/trip/finish` открывает транзакцию и внутри неё:
    - читает поездку с `SELECT ... FOR UPDATE`;
    - вызывает `t.Finish(now)`, домен проверяет статус;
    - сохраняет поездку и пишет запись в историю статусов.
5. При ошибке на любом шаге транзакция откатывается, а ошибка уходит наверх обёрнутой (`finish trip: %w`).
6. `domainerror.Problem` превращает ошибку в ответ: `ErrTripNotFound` в `404`, `ErrTripCompleted` в `409`, всё неизвестное в `500` без подробностей для клиента.

### Отличия от дерева из задания

- Ручки `/health` и `/ready` лежат в `internal/http/health`, а не в пакетах поездок: к поездкам они не относятся.
- Общая запись ответов (`JSON` и `problem+json`) вынесена в `internal/http/response`, чтобы не повторять её в каждой ручке.
- Сгенерированный интерфейс `ServerInterface` реализует `internal/http/server.go`. Он только передаёт вызовы ручкам.
- Пакета `view` нет: перевод доменной поездки в ответ лежит в `convert.go` рядом с каждой ручкой.
- Входные данные для создания поездки описаны в `internal/model`, чтобы usecase не зависел от сгенерированных HTTP-типов.
- Точка входа лежит в `cmd/trip-service`, как требует `conventions.md`.

## Наблюдаемость

Трейсы и метрики уходят по OTLP/HTTP в OTel Collector, дальше в Jaeger и Prometheus. Адрес коллектора, имя сервиса, атрибуты ресурса и сэмплер SDK берёт из стандартных переменных `OTEL_*`, своих имён нет. Настройка лежит в `internal/infra/telemetry`.

### Middleware

Порядок такой: Recover → Tracing → Logging → Metrics.

- **Recover** ловит панику в ручке, пишет её в лог со стеком и отвечает `500` в `problem+json`. Стоит снаружи, чтобы поймать панику в любом из следующих middleware.
- **Tracing** — это `otelhttp`. Перед ним chi находит маршрут для запроса (`Routes.Match`), и шаблон записывается в `r.Pattern`. Поэтому спан называется `GET /api/v1/trips/{tripId}`, а в `http.route` попадает шаблон, а не реальный id. Иначе каждая поездка давала бы отдельное имя спана и отдельный временной ряд в метриках.
- **Logging** пишет одну строку на запрос: метод, шаблон маршрута, путь, статус, длительность. Стоит внутри Tracing, поэтому в строке есть `trace_id` и `span_id`.
- **Metrics** считает длительность и число активных запросов. Собственные метрики `otelhttp` выключены, чтобы `http.server.request.duration` не считалась дважды.

### Трейсы

На каждый HTTP-запрос создаётся серверный спан, внутри него лежат спаны запросов к базе от `otelpgx`: получение соединения, `BEGIN`, `SELECT ... FOR UPDATE`, `UPDATE`, `INSERT`, `COMMIT` или `ROLLBACK`. По трейсу видно, что произошло в транзакции. Например, при повторном завершении поездки в нём есть `SELECT` и `ROLLBACK`, а `UPDATE` нет.

При ответе `5xx` ошибка дополнительно записывается в спан, и он помечается как ошибочный. Ответы `4xx` ошибкой спана не считаются: по соглашениям OpenTelemetry это ошибка клиента, сервер отработал штатно.

### Метрики

| Метрика | Тип | Атрибуты | Откуда |
|---|---|---|---|
| `http.server.request.duration` | histogram, с | метод, `http.route`, код ответа | middleware Metrics |
| `http.server.active_requests` | updown counter | метод, `http.route` | middleware Metrics |
| `db.client.operation.duration` | histogram, с | `db.operation.name`, `pgx.operation.type` | `otelpgx` |
| `tripgo.trips.created` | counter | | usecase `create` |
| `tripgo.trips.completed` | counter | | usecase `finish` |

`db.operation.name` (`SELECT`, `INSERT`, `COMMIT`…) в метриках `otelpgx` по умолчанию нет, он включён опцией `WithMetricOperationName`. Значений у него всего несколько, так что число временных рядов не растёт.

Бизнес-счётчики увеличиваются только после успешного коммита. Usecase вызывает их через интерфейс `tripMetrics`, поэтому тесты проверяют и это: при ошибке счётчик не трогается.

Метрики отправляются раз в 15 секунд. В Prometheus точки в именах заменяются на `_` и добавляется единица: `http_server_request_duration_seconds_bucket`, `tripgo_trips_created_total`.

### Логи

`slog` в формате JSON, в stdout. В каждой строке есть `service` (из `OTEL_SERVICE_NAME`). Если в контексте есть спан, добавляются `trace_id` и `span_id`, и по ним можно перейти от строки лога к трейсу в Jaeger. Уровень задаёт `LOG_LEVEL` (`debug`, `info`, `warn`, `error`). Неизвестное значение не даёт сервису стартовать.

### Нагрузка для проверки

`scripts/load.sh` несколько минут гоняет по сервису смесь сценариев в несколько потоков: полный цикл поездки, повторное завершение (`409`), две поездки одному водителю (`409`), несуществующая поездка (`404`), неверный id и тело (`400`). В конце печатает, сколько пришло ответов каждого кода.

```bash
./scripts/load.sh                       # 3 минуты, 4 потока
DURATION=300 WORKERS=8 ./scripts/load.sh
```

## Тесты

| Что | Где | Как |
|---|---|---|
| Правила домена | `internal/domain/trip` | table-driven, включая граничные координаты и цену 0 |
| Сценарии usecase | `internal/usecase/trip/*` | моки `gomock`: порядок вызовов, отсутствие лишних вызовов после ошибки, метрики только при успехе |
| Ошибка → HTTP-ответ | `internal/http/domainerror` | все коды, обёрнутые ошибки, `500` не раскрывает текст исходной ошибки |
| Ручки | `internal/http/trip/*` | `httptest` с моком usecase: тело ответа, `Location`, 9 вариантов неверного тела |
| Роутер, health, middleware | `internal/http`, `internal/http/middleware` | неверный UUID в пути, `/ready` при недоступной базе, шаблон маршрута в спане и метриках, восстановление после паники |
| Логгер | `internal/infra/logging` | `trace_id` и `span_id` из контекста, уровень |
| Репозиторий | `internal/repository/trip` | интеграционные, настоящий PostgreSQL |
| Менеджер транзакций | `internal/infra/postgres/transaction` | интеграционные: коммит, откат при ошибке и при панике, вложенный `Do` |

Интеграционные тесты помечены `//go:build integration` и запускаются через `make test-integration`. Адрес базы они берут из `DATABASE_URL`. Каждый тест удаляет за собой свои строки, так что запускать их можно на той же базе, что и сервис.

Покрытие `usecase` и `domain` (`make cover`) — 100%.

Отдельно проверена гонка завершения. Пять горутин одновременно завершают одну поездку через настоящий usecase и настоящие транзакции. Успешно должна завершиться ровно одна, в истории должна появиться одна запись `completed`, счётчик метрики должен вырасти на один. Если убрать `FOR UPDATE` из `GetByIDForUpdate`, тест падает (`succeeded = 2, want 1`), значит, он действительно ловит эту гонку.

## Запуск в Docker

Образ собирается в две стадии (`deploy/Dockerfile`). На первой в образе `golang` собирается статический бинарь (`CGO_ENABLED=0`). На второй он копируется в `gcr.io/distroless/static-debian12:nonroot`: там нет исходников, Go, кеша модулей и даже шелла, а процесс запускается от пользователя `nonroot`. `.dockerignore` не пускает в контекст сборки `.git`, `.env`, `.tripgo/`, тесты и локальные файлы.

Размер итогового образа: **8.68 МБ** (`docker images trip-service:lab1`).

```bash
make docker-build
make migrate          # миграции накатываются с компьютера, контейнер их не запускает
make docker-run
curl -i localhost:8080/ready
```

Если `go mod download` внутри сборки не может достучаться до `proxy.golang.org`, передайте свой прокси модулей: `docker build -f deploy/Dockerfile --build-arg GOPROXY="$(go env GOPROXY)" -t trip-service:lab1 .`

Контейнер берёт переменные из `.env`, но адрес базы в нём заменяется. В `.env` база записана как `localhost:22032`, а внутри контейнера `localhost` указывает на сам контейнер. Поэтому контейнер подключается к Docker-сети `kind`, в которой работает кластер `tripgoctl`, и ходит в PostgreSQL через узел кластера:

```
DATABASE_URL=postgres://tripgo:tripgo@tripgo-local-control-plane:30203/tripgo?sslmode=disable
```

`30203` — это NodePort PostgreSQL для лабораторной 2. Адрес коллектора `make docker-run` не подменяет, поэтому из контейнера телеметрия не уходит: для проверки наблюдаемости запускайте сервис через `make run`. Если сервис уже запущен через `make run`, его нужно остановить: оба слушают порт 8080.

## Переменные окружения

Обязательные. Если какой-то нет или значение не разбирается, сервис не стартует и пишет, какая переменная виновата.

| Переменная | Пример | Что задаёт |
|---|---|---|
| `HTTP_ADDR` | `:8080` | адрес HTTP-сервера |
| `LOG_LEVEL` | `info` | уровень логов: `debug`, `info`, `warn`, `error` |
| `SHUTDOWN_TIMEOUT` | `10s` | сколько ждать завершения запросов при остановке |
| `DATABASE_URL` | `postgres://tripgo:tripgo@localhost:5432/tripgo?sslmode=disable` | адрес PostgreSQL |
| `DATABASE_MAX_CONNS` | `10` | максимум соединений в пуле |
| `DATABASE_MIN_CONNS` | `2` | сколько соединений держать открытыми без нагрузки |
| `DATABASE_MAX_CONN_LIFETIME` | `30m` | через сколько соединение пересоздаётся |
| `DATABASE_CONNECT_TIMEOUT` | `5s` | таймаут открытия соединения и проверки базы на старте |
| `DATABASE_QUERY_TIMEOUT` | `3s` | таймаут одного запроса к базе |
| `OTEL_SERVICE_NAME` | `trip-service` | имя сервиса в трейсах, метриках и поле `service` логов |

Стандартные переменные OpenTelemetry. Их читает SDK, сервис их не разбирает.

| Переменная | Пример | Что задаёт |
|---|---|---|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `http://localhost:4318` | адрес OTel Collector |
| `OTEL_EXPORTER_OTLP_PROTOCOL` | `http/protobuf` | протокол экспорта |
| `OTEL_RESOURCE_ATTRIBUTES` | `service.namespace=tripgo,deployment.environment=local` | атрибуты ресурса |
| `OTEL_TRACES_SAMPLER` | `parentbased_always_on` | какие запросы трассировать |

Необязательные, со значениями по умолчанию: таймауты HTTP-сервера.

| Переменная | По умолчанию |
|---|---|
| `HTTP_READ_TIMEOUT` | `10s` |
| `HTTP_READ_HEADER_TIMEOUT` | `5s` |
| `HTTP_WRITE_TIMEOUT` | `30s` |
| `HTTP_IDLE_TIMEOUT` | `60s` |

Все переменные с безопасными значениями лежат в `.env.example`.

## Структура

```
cmd/trip-service/            main: конфиг, логгер, телеметрия, запуск и остановка
contracts/                   копия контрактов курса, не редактируется
deploy/Dockerfile            сборка образа сервиса
migrations/                  миграции goose
scripts/load.sh              нагрузка для проверки трейсов и метрик
internal/
  config/                    чтение и проверка переменных окружения
  container/                 сборка зависимостей
  domain/trip/               поездка, её правила, доменные ошибки
  model/                     входные данные usecase
  usecase/trip/
    create/ get/ finish/     по пакету на сценарий: contract.go, usecase.go, тесты
  repository/trip/           запросы к trips и trip_status_history
  http/
    server.go                реализация сгенерированного ServerInterface
    trip/create/ get/ finish/  ручки: contract.go, handler.go, convert.go
    health/                  /health и /ready
    middleware/              Recover, Tracing, Logging, Metrics
    domainerror/             ошибка → HTTP-код и problem+json
    response/                запись JSON и problem+json
  infra/
    postgres/                пул соединений с otelpgx
    postgres/transaction/    менеджер транзакций
    logging/                 slog с trace_id и span_id
    telemetry/               OTLP-экспорт трейсов и метрик, бизнес-счётчики
  generated/                 сгенерированные типы и серверный интерфейс
```

## Решения

### Уровень изоляции

Транзакции открываются с уровнем Read Committed. Это уровень PostgreSQL по умолчанию, но в коде он задан явно.

Более строгий уровень здесь ничего не даёт. Обе гонки, которые есть в сервисе (два создания на одного водителя и два завершения одной поездки), закрыты блокировками самой базы: уникальным индексом и `SELECT ... FOR UPDATE`. Они работают одинаково на любом уровне изоляции. При Serializable пришлось бы ловить ошибки сериализации `40001` и повторять транзакции целиком, и пользы от этого не было бы.

### Менеджер транзакций

Интерфейс состоит из одного метода: `Do(ctx, fn func(ctx) error) error`. Код в `internal/infra/postgres/transaction`.

1. `Do` смотрит, нет ли в контексте уже открытой транзакции. Если есть, просто вызывает `fn` с тем же контекстом: вложенный `Do` работает в транзакции внешнего и сам ничего не коммитит и не откатывает.
2. Иначе открывает транзакцию через `BeginTx` с уровнем Read Committed и кладёт её в контекст по неэкспортируемому ключу.
3. Вызывает `fn` с этим контекстом. Если `fn` вернула `nil`, делает `COMMIT`. Если ошибку, делает `ROLLBACK` и возвращает эту же ошибку без подмены, чтобы выше можно было отличить, например, `driver_busy`.
4. Если `fn` паникнула, срабатывает `defer` с `recover`: транзакция откатывается, паника запускается снова.

Репозитории транзакцию аргументом не получают. Перед каждым запросом они вызывают `GetQuerier(ctx, pool)`: если в контексте есть транзакция, запрос идёт через неё, если нет, то через пул. Репозиторий, вызванный вне `Do`, работает через пул, и каждый его запрос выполняется отдельно с автокоммитом.

Создание поездки и её завершение выполняются внутри одного `Do`: запись в `trips` и запись в `trip_status_history` либо сохраняются обе, либо не сохраняется ни одна. Все эти свойства проверяются интеграционными тестами.

### Запрет двух активных поездок у водителя

В миграции есть частичный уникальный индекс:

```sql
CREATE UNIQUE INDEX trips_driver_active_uniq
    ON trips (driver_id)
    WHERE status = 'active';
```

Он требует уникальности `driver_id` только среди активных поездок, поэтому завершённых у водителя может быть сколько угодно.

При одновременных запросах вторая вставка с тем же водителем ждёт на индексе, пока первая транзакция не завершится. Если первая закоммитилась, вторая получает ошибку `23505`. Репозиторий проверяет код ошибки и имя индекса и возвращает `trip.ErrDriverBusy`, а `domainerror` превращает её в `409 driver_busy`. Проверка через `SELECT` перед `INSERT` так не работает: оба запроса успели бы увидеть, что активной поездки нет.

### Завершение поездки

В ЛР1 статус проверялся прямо в SQL (`UPDATE ... WHERE status = 'active'`). В ЛР2 правило «завершить можно только активную поездку» переехало в домен, а база отвечает только за то, чтобы два запроса не прочитали поездку одновременно.

Usecase `finish` внутри одной транзакции:

1. Читает поездку через `SELECT ... FOR UPDATE`. Если её нет, возвращает `ErrTripNotFound`.
2. Вызывает `t.Finish(now)`. Если поездка уже завершена, домен возвращает `ErrTripCompleted`, и транзакция откатывается.
3. Сохраняет поездку (`UPDATE`) и пишет в историю переход `active → completed`.

`FOR UPDATE` блокирует строку до конца транзакции. Второй параллельный `finish` ждёт на своём `SELECT`, пока первый не закоммитит. После этого он читает уже обновлённую строку со статусом `completed`, и домен ему отказывает. В итоге один запрос получает `200`, второй `409`, `finished_at` не перезаписывается, запись в истории одна.

Так решение о переходе статуса принимается в одном месте, в домене. Его можно проверить обычным unit-тестом, без базы.

### Прочее

- Тело `POST /api/v1/trips` проверяется в три шага. Сначала по сырому JSON проверяется, что есть все обязательные поля контракта и они не `null`: после разбора в структуру отсутствующая цена или координата неотличима от нуля. Потом тело разбирается в сгенерированный тип с запретом неизвестных полей. Значения (непустые UUID, диапазоны координат, неотрицательная цена) проверяет домен в `trip.New`.
- `id` и `started_at` ставит сервис, из запроса они не принимаются: в теле запрещены неизвестные поля. Время обрезается до микросекунд, потому что с такой точностью его хранит PostgreSQL, и ответы `POST` и `GET` совпадают. Функция текущего времени передаётся в usecase параметром, поэтому в тестах время фиксированное.
- Код генерируется только для пяти операций лабораторной (`-include-operation-ids`). Общий контракт курса уже содержит ручки координат из ЛР3, без фильтра они попали бы в интерфейс.
- Ошибку формата `tripId` в пути ловит сгенерированный код. Для неё подставлен свой обработчик, чтобы ответ тоже был в `problem+json` с кодом `invalid_request`.
- Конфигурация читается библиотекой `github.com/caarlos0/env`. В стеке курса библиотеки для конфига нет, эта используется только для разбора переменных окружения в структуру.

## Как проверить

Создать поездку, получить и завершить:

```bash
curl -i -X POST localhost:8080/api/v1/trips -H 'content-type: application/json' \
  -d '{"user_id":"5cb72c04-7650-45c9-a79b-bcdba0631e0c",
       "driver_id":"8860b315-ec86-42eb-a17c-7c163d721ff5",
       "start_point":{"latitude":59.9398,"longitude":30.3146},
       "end_point":{"latitude":59.9290,"longitude":30.3626},
       "price":1450}'

curl -i localhost:8080/api/v1/trips/$TRIP_ID
curl -i -X POST localhost:8080/api/v1/trips/$TRIP_ID/finish   # 200, повторно 409
```

Двадцать одновременных созданий на одного водителя дают один `201`. Тело запроса для этой проверки удобно положить в файл (`trip.json` в `.gitignore`), а таблицы перед проверкой очистить, чтобы у водителя не осталось активной поездки от прошлых запусков:

```bash
cat > trip.json <<'EOF'
{"user_id":"5cb72c04-7650-45c9-a79b-bcdba0631e0c","driver_id":"8860b315-ec86-42eb-a17c-7c163d721ff5","start_point":{"latitude":59.9398,"longitude":30.3146},"end_point":{"latitude":59.9290,"longitude":30.3626},"price":1450}
EOF

psql "$DATABASE_URL" -c "TRUNCATE trips CASCADE"

seq 20 | xargs -P20 -I{} curl -s -o /dev/null -w '%{http_code}\n' \
  -X POST localhost:8080/api/v1/trips -H 'content-type: application/json' \
  -d @trip.json | sort | uniq -c
```

Ожидаемый результат: `1 201` и `19 409`. Чтобы `psql` видел `DATABASE_URL`, загрузите переменные из `.env` в терминал: `set -a; source .env; set +a`.

Два одновременных завершения одной поездки дают `200` и `409`:

```bash
seq 2 | xargs -P2 -I{} curl -s -o /dev/null -w '%{http_code}\n' \
  -X POST localhost:8080/api/v1/trips/$TRIP_ID/finish
```

`/ready` при остановленной базе:

```bash
tripgoctl environment stop
curl -i localhost:8080/ready      # 503
tripgoctl environment start
curl -i localhost:8080/ready      # снова 200
```

Трейсы и метрики: запустите `./scripts/load.sh`, затем откройте в Jaeger сервис `trip-service`, а в Grafana раздел Explore с источником Prometheus. Полезные запросы:

```
sum(rate(tripgo_trips_created_total[1m]))
sum by (http_route, http_response_status_code) (rate(http_server_request_duration_seconds_count[1m]))
sum by (db_operation_name) (rate(db_client_operation_duration_seconds_sum{db_operation_name!=""}[1m]))
  / sum by (db_operation_name) (rate(db_client_operation_duration_seconds_count{db_operation_name!=""}[1m]))
```

Откат миграций: `make migrate-down`, затем `make migrate`.

## Что не сделано

- Задание со звёздочкой ЛР1 про идемпотентность по `Idempotency-Key`.
- Задание со звёздочкой ЛР2 про мутационное тестирование. Вручную проверено только одно место: тест на гонку завершения падает без `FOR UPDATE`.