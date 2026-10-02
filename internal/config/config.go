package config

import (
	"fmt"
	"time"

	"github.com/caarlos0/env/v11"
)

type Config struct {
	LogLevel        string        `env:"LOG_LEVEL, required, notEmpty"`
	ShutdownTimeout time.Duration `env:"SHUTDOWN_TIMEOUT" envDefault:"10s"`

	HTTP     HTTPConfig
	Database DatabaseConfig
}

type HTTPConfig struct {
	Addr              string        `env:"HTTP_ADDR, required, notEmpty"`
	ReadTimeout       time.Duration `env:"HTTP_READ_TIMEOUT" envDefault:"10s"`
	ReadHeaderTimeout time.Duration `env:"HTTP_READ_HEADER_TIMEOUT" envDefault:"5s"`
	WriteTimeout      time.Duration `env:"HTTP_WRITE_TIMEOUT" envDefault:"30s"`
	IdleTimeout       time.Duration `env:"HTTP_IDLE_TIMEOUT" envDefault:"60s"`
}
type DatabaseConfig struct {
	URL             string        `env:"DATABASE_URL, required, notEmpty"`
	MaxConns        int32         `env:"DATABASE_MAX_CONNS" envDefault:"10"`
	MinConns        int32         `env:"DATABASE_MIN_CONNS" envDefault:"2"`
	MaxConnLifetime time.Duration `env:"DATABASE_MAX_CONN_LIFETIME" envDefault:"30m"`
	ConnectTimeout  time.Duration `env:"DATABASE_CONNECT_TIMEOUT" envDefault:"5s"`
	QueryTimeout    time.Duration `env:"DATABASE_QUERY_TIMEOUT" envDefault:"3s"`
}

func Load() (Config, error) {
	cfg, err := env.ParseAs[Config]()
	if err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("validate config: %w", err)
	}

	return cfg, nil
}

func (c Config) Validate() error {
	if c.Database.MaxConns <= 0 {
		return fmt.Errorf("DATABASE_MAX_CONNS must be greater than zero")
	}

	if c.Database.MinConns < 0 {
		return fmt.Errorf("DATABASE_MIN_CONNS must not be negative")
	}

	if c.Database.MinConns > c.Database.MaxConns {
		return fmt.Errorf("DATABASE_MIN_CONNS must not exceed DATABASE_MAX_CONNS")
	}

	durations := map[string]time.Duration{
		"SHUTDOWN_TIMEOUT":           c.ShutdownTimeout,
		"HTTP_READ_TIMEOUT":          c.HTTP.ReadTimeout,
		"HTTP_READ_HEADER_TIMEOUT":   c.HTTP.ReadHeaderTimeout,
		"HTTP_WRITE_TIMEOUT":         c.HTTP.WriteTimeout,
		"HTTP_IDLE_TIMEOUT":          c.HTTP.IdleTimeout,
		"DATABASE_QUERY_TIMEOUT":     c.Database.QueryTimeout,
		"DATABASE_CONNECT_TIMEOUT":   c.Database.ConnectTimeout,
		"DATABASE_MAX_CONN_LIFETIME": c.Database.MaxConnLifetime,
	}

	for name, duration := range durations {
		if duration <= 0 {
			return fmt.Errorf("%s must be greater than zero", name)
		}
	}

	return nil
}
