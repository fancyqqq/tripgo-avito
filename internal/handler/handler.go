package handler

import (
	"github.com/fancyqqq/tripgo-avito/api"
)

type Handler struct {
	api.Unimplemented
}

func New() *Handler {
	return &Handler{}
}
