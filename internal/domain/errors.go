package domain

import "errors"

var (
	ErrTripNotFound  = errors.New("trip not found")
	ErrTripCompleted = errors.New("trip is already completed")
	ErrDriverBusy    = errors.New("driver busy")
)
