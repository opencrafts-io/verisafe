package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/opencrafts-io/verisafe/internal/core"
)

// AppHandler is a http.HandlerFunc that can return an error.
// Use it instead of http.HandlerFunc when you want centralised error handling.
type AppHandler func(w http.ResponseWriter, r *http.Request) error

// ServeHTTP implements http.Handler, adapting AppHandler into the standard library.
// All error-to-HTTP mapping lives here — handlers never touch w.WriteHeader for errors.
func (h AppHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := h(w, r); err != nil {
		handleError(w, err)
	}
}

// handleError maps domain errors to HTTP responses.
// Add new sentinel errors here as the domain grows.
func handleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, core.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, core.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, core.ErrUnauthorized):
		writeError(w, http.StatusUnauthorized, err.Error())
	case errors.Is(err, core.ErrInternal):
		writeError(w, http.StatusInternalServerError, "something went wrong")
	default:
		writeError(w, http.StatusInternalServerError, "something went wrong")
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
