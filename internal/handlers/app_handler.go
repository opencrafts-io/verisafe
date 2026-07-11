package handlers

import (
	"net/http"

	"github.com/opencrafts-io/verisafe/internal/core"
)

// AppHandler is a http.HandlerFunc that can return an error.
// Use it instead of http.HandlerFunc when you want centralised error handling.
type AppHandler func(w http.ResponseWriter, r *http.Request) error

// ServeHTTP implements http.Handler, adapting AppHandler into the standard library.
// All error-to-HTTP mapping lives in core.HandleError — handlers never touch
// w.WriteHeader for errors.
func (h AppHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := h(w, r); err != nil {
		core.HandleError(w, err)
	}
}
