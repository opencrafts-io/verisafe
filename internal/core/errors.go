package core

import "errors"

// APIError is the standard error response body returned by the API.
type APIError struct {
	Error string `json:"error" example:"the provided input is invalid or malformed"`
}

var (
	// ErrInternal indicates a failure in the system (DB down, disk full, etc.)
	ErrInternal = errors.New("an internal system error occurred")

	// ErrInvalidInput indicates the client sent bad data
	ErrInvalidInput = errors.New("the provided input is invalid or malformed")

	// ErrNotFound indicates a requested resource does not exist
	ErrNotFound = errors.New("the requested resource was not found")

	// ErrConflict indicates a state conflict (e.g., duplicate unique key)
	ErrConflict = errors.New(
		"a conflict occurred with the current state of the resource",
	)

	// ErrUnauthorized indicates missing or invalid authentication
	ErrUnauthorized = errors.New(
		"authentication is required to access this resource",
	)

	// ErrForbidden indicates the caller is authenticated but not allowed to
	// perform this action. Distinct from ErrUnauthorized: re-authenticating
	// would not help.
	ErrForbidden = errors.New(
		"you do not have permission to perform this action",
	)

	// ErrUnavailable indicates an upstream dependency (an OAuth provider, say)
	// failed transiently. Callers should retry rather than treat the resource
	// as broken.
	ErrUnavailable = errors.New(
		"an upstream dependency is temporarily unavailable",
	)
)
