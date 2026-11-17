package push

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"
)

func TestResponseBodyAlwaysClosed(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		status  int
		readErr error
		wantErr bool
	}{
		{name: "accepted", status: http.StatusAccepted, readErr: io.EOF},
		{name: "client error", status: http.StatusBadRequest, readErr: io.EOF, wantErr: true},
		{name: "server error", status: http.StatusServiceUnavailable, readErr: io.EOF, wantErr: true},
		{name: "response read error", status: http.StatusAccepted, readErr: io.ErrUnexpectedEOF, wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			body := NewMockReadCloser(ctrl)
			gomock.InOrder(body.EXPECT().Read(gomock.Any()).Return(0, tt.readErr), body.EXPECT().Close().Return(nil))
			transport := NewMockRoundTripper(ctrl)
			transport.EXPECT().RoundTrip(gomock.Any()).Return(&http.Response{StatusCode: tt.status, Body: body}, nil)
			gateway := newGateway(t, "http://localhost", time.Second)
			gateway.client.Transport = transport
			err := gateway.RequestPosition(context.Background(), uuid.New(), uuid.New())
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %t", err, tt.wantErr)
			}
			if tt.readErr == io.ErrUnexpectedEOF && !errors.Is(err, tt.readErr) {
				t.Fatalf("read error lost: %v", err)
			}
		})
	}
}

func TestTransportErrorPreservesCause(t *testing.T) {
	t.Parallel()
	cause := errors.New("connection refused")
	transport := NewMockRoundTripper(gomock.NewController(t))
	transport.EXPECT().RoundTrip(gomock.Any()).Return(nil, cause)
	gateway := newGateway(t, "http://localhost", time.Second)
	gateway.client.Transport = transport
	err := gateway.RequestPosition(context.Background(), uuid.New(), uuid.New())
	var transportError *TransportError
	if !errors.As(err, &transportError) || !errors.Is(err, cause) {
		t.Fatalf("transport cause lost: %v", err)
	}
}

func TestResponseDrainIsBounded(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	body := NewMockReadCloser(ctrl)
	readBytes := 0
	body.EXPECT().Read(gomock.Any()).DoAndReturn(func(buffer []byte) (int, error) {
		readBytes += len(buffer)
		return len(buffer), nil
	}).AnyTimes()
	body.EXPECT().Close().Return(nil)
	transport := NewMockRoundTripper(ctrl)
	transport.EXPECT().RoundTrip(gomock.Any()).Return(&http.Response{StatusCode: http.StatusAccepted, Body: body}, nil)
	gateway := newGateway(t, "http://localhost", time.Second)
	gateway.client.Transport = transport
	if err := gateway.RequestPosition(context.Background(), uuid.New(), uuid.New()); err != nil {
		t.Fatal(err)
	}
	if readBytes != maxResponseBodySize {
		t.Fatalf("drained %d bytes, want limit %d", readBytes, maxResponseBodySize)
	}
}
