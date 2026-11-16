package config

import (
	"fmt"
	"net/url"
	"strconv"
	"time"
)

type PushConfig struct {
	HTTPURL        string        `env:"PUSH_HTTP_URL,required,notEmpty"`
	RequestTimeout time.Duration `env:"PUSH_REQUEST_TIMEOUT" envDefault:"1s"`
}

func (c PushConfig) validate() error {
	if c.RequestTimeout <= 0 {
		return fmt.Errorf("PUSH_REQUEST_TIMEOUT must be greater than zero")
	}

	endpoint, err := url.Parse(c.HTTPURL)
	if err != nil || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.Hostname() == "" {
		return fmt.Errorf("PUSH_HTTP_URL must be an absolute HTTP or HTTPS URL with a hostname")
	}

	if port := endpoint.Port(); port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return fmt.Errorf("PUSH_HTTP_URL port must be between 1 and 65535")
		}
	}

	return nil
}
