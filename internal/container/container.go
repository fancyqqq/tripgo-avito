package container

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/fancyqqq/tripgo-avito/internal/config"
	httptransport "github.com/fancyqqq/tripgo-avito/internal/http"
	"github.com/fancyqqq/tripgo-avito/internal/http/health"
	createhandler "github.com/fancyqqq/tripgo-avito/internal/http/trip/create"
	finishhandler "github.com/fancyqqq/tripgo-avito/internal/http/trip/finish"
	gethandler "github.com/fancyqqq/tripgo-avito/internal/http/trip/get"
	"github.com/fancyqqq/tripgo-avito/internal/infra/postgres"
	"github.com/fancyqqq/tripgo-avito/internal/infra/postgres/transaction"
	triprepository "github.com/fancyqqq/tripgo-avito/internal/repository/trip"
	createusecase "github.com/fancyqqq/tripgo-avito/internal/usecase/trip/create"
	finishusecase "github.com/fancyqqq/tripgo-avito/internal/usecase/trip/finish"
	getusecase "github.com/fancyqqq/tripgo-avito/internal/usecase/trip/get"
)

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

	txManager := transaction.New(pool)
	tripRepo := triprepository.New(pool, cfg.Database.QueryTimeout)

	createUseCase := createusecase.New(tripRepo, txManager, now)
	getUseCase := getusecase.New(tripRepo)
	finishUseCase := finishusecase.New(tripRepo, txManager, now)

	server := httptransport.NewServer(
		createhandler.New(createUseCase, logger),
		gethandler.New(getUseCase, logger),
		finishhandler.New(finishUseCase, logger),
		health.New(pool, cfg.Database.QueryTimeout, logger),
		logger,
	)

	return &Container{
		HTTPServer: &http.Server{
			Addr:              cfg.HTTP.Addr,
			Handler:           server.Routes(),
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
