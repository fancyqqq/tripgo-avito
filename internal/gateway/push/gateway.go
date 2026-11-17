package push

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

const requestPath = "/api/v1/push"

type Gateway struct {
	endpoint  string
	client    *http.Client
	transport *http.Transport
}

func New(baseURL string, requestTimeout time.Duration, options ...otelhttp.Option) (*Gateway, error) {
	endpoint, err := url.Parse(baseURL)
	if err != nil || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.Hostname() == "" {
		return nil, fmt.Errorf("push base URL must be an absolute HTTP or HTTPS URL with a hostname")
	}
	if requestTimeout <= 0 {
		return nil, fmt.Errorf("push request timeout must be greater than zero")
	}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: requestTimeout}).DialContext,
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   requestTimeout,
		ResponseHeaderTimeout: requestTimeout,
		IdleConnTimeout:       requestTimeout,
	}
	options = append([]otelhttp.Option{
		otelhttp.WithSpanNameFormatter(func(string, *http.Request) string { return "POST " + requestPath }),
	}, options...)

	return &Gateway{
		endpoint: endpoint.JoinPath("api", "v1", "push").String(),
		client: &http.Client{
			Timeout:       requestTimeout,
			Transport:     otelhttp.NewTransport(transport, options...),
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		transport: transport,
	}, nil
}

func (g *Gateway) Close() {
	g.transport.CloseIdleConnections()
}
