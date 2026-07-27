package auth

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// providerTokenExpiry replaces a construction that omitted Valid, so every
// login wrote SQL NULL into socials.expires_at. The bug was invisible: no
// error, no log line, just a column that was always empty. These pin both
// halves of the fix.
func TestProviderTokenExpiry(t *testing.T) {
	t.Run("a real expiry is stored, in UTC", func(t *testing.T) {
		nairobi := time.FixedZone("EAT", 3*60*60)
		expiry := time.Date(2026, 7, 26, 15, 4, 5, 0, nairobi)

		got := providerTokenExpiry(expiry)

		assert.True(t, got.Valid, "a real expiry must not be written as NULL")
		assert.Equal(t, time.UTC, got.Time.Location(),
			"the column has no zone, so the value must be normalized to UTC")
		assert.True(t, got.Time.Equal(expiry), "normalizing must not shift the instant")
	})

	// The reason the fix is not simply Valid:true. goth reports a zero time
	// when a provider returns no expiry, and UpdateSocial does
	// COALESCE($3, expires_at) — a valid zero time would overwrite a good
	// stored expiry with year 1, which is worse than the original bug.
	t.Run("a zero expiry stays NULL so COALESCE preserves the stored value", func(t *testing.T) {
		got := providerTokenExpiry(time.Time{})

		assert.False(t, got.Valid,
			"a zero expiry must encode as NULL, not as year 1")
	})
}
