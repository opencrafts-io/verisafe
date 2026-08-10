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

// publicError carries a client-facing message alongside a sentinel.
//
// Handlers migrating off hand-written response blocks use it to keep the exact
// bytes their previous code emitted while still routing through HandleError.
// Without it, every migrated 500 would silently switch from whatever wording
// the handler used to "something went wrong".
type publicError struct {
	sentinel error
	msg      string
}

func (e *publicError) Error() string { return e.msg }

// Unwrap is what lets errors.Is find the sentinel, so statusFor keeps working.
func (e *publicError) Unwrap() error { return e.sentinel }

// Public wraps sentinel so the response takes its status from sentinel but its
// body from msg. Use it when a specific wording is part of the live contract;
// return the bare sentinel when it is not.
func Public(sentinel error, msg string) error {
	return &publicError{sentinel: sentinel, msg: msg}
}

// Fallback returns err unchanged if it already carries a public message, and
// wraps it otherwise.
//
// Use it at a transaction boundary, where the error may have come from deep
// inside the closure (already public) or from Begin or Commit (not).
func Fallback(err error, sentinel error, msg string) error {
	var pe *publicError
	if errors.As(err, &pe) {
		return err
	}
	return Public(sentinel, msg)
}

// statusFor maps a domain error to its HTTP status. Add new sentinels here as
// the domain grows.
func statusFor(err error) int {
	switch {
	case errors.Is(err, ErrInvalidInput):
		return http.StatusBadRequest
	case errors.Is(err, ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, ErrUnauthorized):
		return http.StatusUnauthorized
	case errors.Is(err, ErrForbidden):
		return http.StatusForbidden
	case errors.Is(err, ErrConflict):
		return http.StatusConflict
	case errors.Is(err, ErrUnavailable):
		return http.StatusServiceUnavailable
	default:
		// ErrInternal and anything unclassified.
		return http.StatusInternalServerError
	}
}

// HandleError maps a domain error to an HTTP response.
//
// An error carrying a public message is rendered with that message. Otherwise
// a 500 is rendered as a fixed string, because the wrapped detail can hold a
// driver error or a query fragment, and any other status echoes the error text.
func HandleError(w http.ResponseWriter, err error) {
	status := statusFor(err)

	var pe *publicError
	if errors.As(err, &pe) {
		WriteError(w, status, pe.msg)
		return
	}

	if status == http.StatusInternalServerError {
		WriteError(w, status, "something went wrong")
		return
	}

	WriteError(w, status, err.Error())
}
