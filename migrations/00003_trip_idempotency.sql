-- +goose Up
CREATE TABLE trip_idempotency (
    idempotency_key UUID PRIMARY KEY,
    request_hash BYTEA NOT NULL CHECK (octet_length(request_hash) = 32),
    trip_id UUID REFERENCES trips(id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL
);

-- +goose Down
DROP TABLE trip_idempotency;
