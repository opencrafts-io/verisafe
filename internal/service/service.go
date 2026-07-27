package service

import "errors"

// DEPRECATED — do not use these in new code; return the internal/core
// sentinels instead.
//
// These are distinct values from their identically named counterparts in
// internal/core, so errors.Is(service.ErrNotFound, core.ErrNotFound) is false.
// core.HandleError matches on the core sentinels, which means a service error
// returned through an AppHandler falls through to the default branch and
// silently becomes a 500 instead of the intended 404 or 400.
//
// device_service.go and oauth_grant_service.go already return core sentinels.
// The remaining users of these should be migrated and this block removed.
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
