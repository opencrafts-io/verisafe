package core

import (
	"context"
	"time"
)

// Cacher defines a generic key-value storage behavior,
// typically used for ephemeral data.
//

//go:generate mockgen -source=cacher.go -destination=mocks/cacher.go -package=mockscore
type Cacher interface {
	// Get retrieves a value by key.
	// Returns an error if the key doesn't exist or the operation fails.
	Get(ctx context.Context, key string, dest interface{}) error

	// Set stores a value with a specific TTL (Time to Live).
	Set(
		ctx context.Context,
		key string,
		value interface{},
		ttl time.Duration,
	) error

	// Delete removes a key from the cache.
	Delete(ctx context.Context, key string) error

	// Exists checks if a key is present without fetching the data.
	Exists(ctx context.Context, key string) (bool, error)
}
