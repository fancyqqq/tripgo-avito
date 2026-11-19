package config

import (
	"fmt"
	"time"
)

type PositionsConfig struct {
	CheckInterval time.Duration `env:"POSITION_CHECK_INTERVAL" envDefault:"30s"`
	StaleAfter    time.Duration `env:"POSITION_STALE_AFTER" envDefault:"60s"`
	Workers       int           `env:"POSITION_WORKERS" envDefault:"4"`
	QueueSize     int           `env:"POSITION_QUEUE_SIZE" envDefault:"100"`
}

func (c PositionsConfig) validate() error {
	if c.CheckInterval <= 0 {
		return fmt.Errorf("POSITION_CHECK_INTERVAL must be greater than zero")
	}
	if c.StaleAfter <= 0 {
		return fmt.Errorf("POSITION_STALE_AFTER must be greater than zero")
	}
	if c.Workers <= 0 {
		return fmt.Errorf("POSITION_WORKERS must be greater than zero")
	}
	if c.QueueSize <= 0 {
		return fmt.Errorf("POSITION_QUEUE_SIZE must be greater than zero")
	}

	return nil
}
