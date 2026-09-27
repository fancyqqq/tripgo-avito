package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	HTTPAddr        string
	LogLevel        string
	ShutdownTimeout time.Duration

	Database DatabaseConfig
}

type DatabaseConfig struct {
	URL             string
	MaxConns        int32
	MinConns        int32
	MaxConnLifetime time.Duration
	ConnectTimeout  time.Duration
	QueryTimeout    time.Duration
}

func Load() (Config, error) {
	httpAddr, err := getRequiredEnv("HTTP_ADDR")
	if err != nil {
		return Config{}, err
	}

	logLevel, err := getRequiredEnv("LOG_LEVEL")
	if err != nil {
		return Config{}, err
	}

	shutdownTimeout, err := getDurationEnv("SHUTDOWN_TIMEOUT")
	if err != nil {
		return Config{}, err
	}

	databaseURL, err := getRequiredEnv("DATABASE_URL")
	if err != nil {
		return Config{}, err
	}

	maxConns, err := getInt32Env("DATABASE_MAX_CONNS")
	if err != nil {
		return Config{}, err
	}

	minConns, err := getInt32Env("DATABASE_MIN_CONNS")
	if err != nil {
		return Config{}, err
	}

	maxConnLifetime, err := getDurationEnv("DATABASE_MAX_CONN_LIFETIME")
	if err != nil {
		return Config{}, err
	}

	connectTimeout, err := getDurationEnv("DATABASE_CONNECT_TIMEOUT")
	if err != nil {
		return Config{}, err
	}

	queryTimeout, err := getDurationEnv("DATABASE_QUERY_TIMEOUT")
	if err != nil {
		return Config{}, err
	}

	return Config{
		HTTPAddr:        httpAddr,
		LogLevel:        logLevel,
		ShutdownTimeout: shutdownTimeout,
		Database: DatabaseConfig{
			URL:             databaseURL,
			MaxConns:        maxConns,
			MinConns:        minConns,
			MaxConnLifetime: maxConnLifetime,
			ConnectTimeout:  connectTimeout,
			QueryTimeout:    queryTimeout,
		},
	}, nil
}

func getRequiredEnv(key string) (string, error) {
	value, ok := os.LookupEnv(key)
	if !ok || value == "" {
		return "", fmt.Errorf("%s is required", key)
	}

	return value, nil
}

func getDurationEnv(key string) (time.Duration, error) {
	value, err := getRequiredEnv(key)
	if err != nil {
		return 0, err
	}

	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}

	if duration <= 0 {
		return 0, fmt.Errorf("%s must be greater than zero", key)
	}

	return duration, nil
}

func getInt32Env(key string) (int32, error) {
	value, err := getRequiredEnv(key)
	if err != nil {
		return 0, err
	}

	num, err := strconv.ParseInt(value, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}

	return int32(num), nil
}
