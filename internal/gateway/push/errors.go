package push

import "fmt"

type HTTPError struct {
	StatusCode int
	RetryAfter string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("push service returned HTTP %d", e.StatusCode)
}

type TransportError struct {
	Err error
}

func (e *TransportError) Error() string {
	return fmt.Sprintf("push transport: %v", e.Err)
}

func (e *TransportError) Unwrap() error {
	return e.Err
}
