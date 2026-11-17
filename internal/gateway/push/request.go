package push

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/google/uuid"
)

const maxResponseBodySize = 1 << 20

type positionRequest struct {
	RequestID   string            `json:"request_id"`
	RecipientID uuid.UUID         `json:"recipient_id"`
	Kind        string            `json:"kind"`
	Data        map[string]string `json:"data"`
}

func (g *Gateway) RequestPosition(ctx context.Context, tripID, driverID uuid.UUID) error {
	payload, err := json.Marshal(positionRequest{
		RequestID: uuid.NewString(), RecipientID: driverID, Kind: "REQUEST_POSITION",
		Data: map[string]string{"trip_id": tripID.String()},
	})
	if err != nil {
		return fmt.Errorf("encode position request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, g.endpoint, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build position request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := g.client.Do(request)
	if err != nil {
		return fmt.Errorf("request driver position: %w", &TransportError{Err: err})
	}
	defer func() { _ = response.Body.Close() }()

	_, readErr := io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseBodySize))
	if response.StatusCode != http.StatusAccepted {
		return &HTTPError{StatusCode: response.StatusCode, RetryAfter: response.Header.Get("Retry-After")}
	}
	if readErr != nil {
		return fmt.Errorf("read push response: %w", &TransportError{Err: readErr})
	}
	return nil
}
