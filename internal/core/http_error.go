package core

import (
	"encoding/json"
	"errors"
	"net/http"
)

// WriteJSON writes body as a JSON response with the given status code.
func WriteJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}

// WriteError writes an APIError{Error: message} JSON response with the given
// status code. Prefer HandleError when you have a domain error to map from a
// sentinel (ErrInvalidInput, etc.) rather than an already-chosen status code.
func WriteError(w http.ResponseWriter, status int, message string) {
	WriteJSON(w, status, APIError{Error: message})
}

// HandleError maps a domain error to an HTTP response by checking it against
// the sentinel errors below. Add new sentinels here as the domain grows.
func HandleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidInput):
		WriteError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrNotFound):
		WriteError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrUnauthorized):
		WriteError(w, http.StatusUnauthorized, err.Error())
	case errors.Is(err, ErrInternal):
		WriteError(w, http.StatusInternalServerError, "something went wrong")
	default:
		WriteError(w, http.StatusInternalServerError, "something went wrong")
	}
}
