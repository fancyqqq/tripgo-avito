package container

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"

	"github.com/fancyqqq/tripgo-avito/internal/config"
	httptransport "github.com/fancyqqq/tripgo-avito/internal/http"
	"github.com/fancyqqq/tripgo-avito/internal/http/health"
	"github.com/fancyqqq/tripgo-avito/internal/http/middleware"
	createhandler "github.com/fancyqqq/tripgo-avito/internal/http/trip/create"
	createpositionhandler "github.com/fancyqqq/tripgo-avito/internal/http/trip/createposition"
	finishhandler "github.com/fancyqqq/tripgo-avito/internal/http/trip/finish"
	gethandler "github.com/fancyqqq/tripgo-avito/internal/http/trip/get"
	listpositionshandler "github.com/fancyqqq/tripgo-avito/internal/http/trip/listpositions"
	"github.com/fancyqqq/tripgo-avito/internal/infra/postgres"
	"github.com/fancyqqq/tripgo-avito/internal/infra/postgres/transaction"
	"github.com/fancyqqq/tripgo-avito/internal/infra/telemetry"
	"github.com/fancyqqq/tripgo-avito/internal/repository/idempotency"
	positionrepository "github.com/fancyqqq/tripgo-avito/internal/repository/position"
	triprepository "github.com/fancyqqq/tripgo-avito/internal/repository/trip"
	createusecase "github.com/fancyqqq/tripgo-avito/internal/usecase/trip/create"
	createpositionusecase "github.com/fancyqqq/tripgo-avito/internal/usecase/trip/createposition"
	finishusecase "github.com/fancyqqq/tripgo-avito/internal/usecase/trip/finish"
	getusecase "github.com/fancyqqq/tripgo-avito/internal/usecase/trip/get"
	listpositionsusecase "github.com/fancyqqq/tripgo-avito/internal/usecase/trip/listpositions"
)

const meterName = "github.com/fancyqqq/tripgo-avito"

type Container struct {
	HTTPServer *http.Server
	pool       *pgxpool.Pool
}

func New(ctx context.Context, cfg config.Config, logger *slog.Logger) (*Container, error) {
	pool, err := postgres.NewPool(ctx, cfg.Database)
	if err != nil {
		return nil, fmt.Errorf("init postgres: %w", err)
	}

	now := func() time.Time {
		return time.Now().UTC().Truncate(time.Microsecond)
	}

	meter := otel.Meter(meterName)

	tripMetrics, err := telemetry.NewTripMetrics(meter)
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("init trip metrics: %w", err)
	}

	httpMetrics, err := middleware.NewMetrics(meter)
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("init http metrics: %w", err)
	}

	txManager := transaction.New(pool)
	tripRepo := triprepository.New(pool, cfg.Database.QueryTimeout)
	positionRepo := positionrepository.New(pool, cfg.Database.QueryTimeout)

	createUseCase := createusecase.New(tripRepo, idempotency.NewIdempotencyRepository(pool, cfg.Database.QueryTimeout), txManager, tripMetrics, now)
	getUseCase := getusecase.New(tripRepo)
	finishUseCase := finishusecase.New(tripRepo, txManager, tripMetrics, now)
	createPositionUseCase := createpositionusecase.New(
		tripRepo, positionRepo, txManager, tripMetrics, nil, now,
	)
	listPositionsUseCase := listpositionsusecase.New(tripRepo, positionRepo)

	server := httptransport.NewServer(
		createhandler.New(createUseCase, logger),
		gethandler.New(getUseCase, logger),
		finishhandler.New(finishUseCase, logger),
		createpositionhandler.New(createPositionUseCase, logger),
		listpositionshandler.New(listPositionsUseCase, logger),
		health.New(pool, cfg.Database.QueryTimeout, logger),
		logger,
	)

	router := server.Routes()
	handler := middleware.Chain(router,
		middleware.Recover(logger),
		middleware.Tracing(router),
		middleware.Logging(logger),
		httpMetrics.Middleware,
	)

	return &Container{
		HTTPServer: &http.Server{
			Addr:              cfg.HTTP.Addr,
			Handler:           handler,
			ReadTimeout:       cfg.HTTP.ReadTimeout,
			ReadHeaderTimeout: cfg.HTTP.ReadHeaderTimeout,
			WriteTimeout:      cfg.HTTP.WriteTimeout,
			IdleTimeout:       cfg.HTTP.IdleTimeout,
		},
		pool: pool,
	}, nil
}

func (c *Container) Close() {
	c.pool.Close()
}
