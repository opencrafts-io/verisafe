package core

import (
	"context"
	"errors"
	"time"
)

// Cacher defines a simple key-value cache interface.
// The default implementation uses Redis, but any backend can be swapped in —
// including an in-memory mock for testing.
// mockgen -source=cacher.go -destination=mocks/cacher.go -package=mockscore
type Cacher interface {
	// Set stores a value under the given key with an optional TTL.
	// Pass 0 for ttl to store indefinitely.
	Set(ctx context.Context, key string, value any, ttl time.Duration) error

	// Get retrieves a value by key into dest.
	// Returns ErrCacheMiss if the key does not exist.
	Get(ctx context.Context, key string, dest any) error

	// Delete removes a key from the cache.
	Delete(ctx context.Context, key string) error

	// Exists returns true if the key exists in the cache.
	Exists(ctx context.Context, key string) (bool, error)
}

// ErrCacheMiss is returned by Get when the key does not exist.
var ErrCacheMiss = errors.New("cache: key not found")
