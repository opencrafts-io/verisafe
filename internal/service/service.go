package service

import "errors"

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
)
