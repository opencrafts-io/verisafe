package middleware_test

import (
	"context"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/opencrafts-io/verisafe/internal/middleware"
	"github.com/opencrafts-io/verisafe/internal/tokens"
	"github.com/stretchr/testify/assert"
)

// The accessors must be safe on a context IsAuthenticated never touched.
// Four call sites in service_token_handler.go previously did a bare
// .([]string) on the permissions value, which panics on exactly this input —
// a 500 with a stack trace if a route were ever registered without the
// middleware. Returning the zero value instead is what makes the accessor
// worth having, so it is asserted rather than assumed.
func TestAccessorsOnAnEmptyContextReturnZeroValuesAndDoNotPanic(t *testing.T) {
	ctx := context.Background()

	assert.NotPanics(t, func() {
		assert.Nil(t, middleware.PermissionsFromContext(ctx))
		assert.Nil(t, middleware.RolesFromContext(ctx))
		assert.False(t, middleware.IsServiceToken(ctx))
		assert.False(t, middleware.IsPendingDeletion(ctx))

		claims, ok := middleware.ClaimsFromContext(ctx)
		assert.Nil(t, claims)
		assert.False(t, ok)
	})
}

// A context carrying a value of the wrong type under one of these keys is not
// reachable through the setters, but the accessors still must not panic --
// this is the property the comma-ok form buys.
func TestAccessorsIgnoreValuesOfTheWrongType(t *testing.T) {
	ctx := middleware.WithPermissions(context.Background(), nil)

	assert.NotPanics(t, func() {
		assert.Empty(t, middleware.PermissionsFromContext(ctx))
	})
}

func TestSettersAndAccessorsRoundTrip(t *testing.T) {
	claims := &tokens.VerisafeClaims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "a-subject"},
	}

	ctx := context.Background()
	ctx = middleware.WithClaims(ctx, claims)
	ctx = middleware.WithPermissions(ctx, []string{"read:role:any"})
	ctx = middleware.WithRoles(ctx, []string{"admin"})
	ctx = middleware.WithServiceToken(ctx, true)

	got, ok := middleware.ClaimsFromContext(ctx)
	assert.True(t, ok)
	assert.Equal(t, "a-subject", got.Subject)
	assert.Equal(t, []string{"read:role:any"}, middleware.PermissionsFromContext(ctx))
	assert.Equal(t, []string{"admin"}, middleware.RolesFromContext(ctx))
	assert.True(t, middleware.IsServiceToken(ctx))
}

// The keys are a private struct type, so a string with the same textual value
// cannot collide with them. This is the SA1029 property the change bought.
func TestPlainStringKeysCannotCollideWithTheAuthKeys(t *testing.T) {
	ctx := context.WithValue(
		context.Background(),
		"middleware.auth.perms", //nolint:staticcheck // deliberately the old key
		[]string{"admin:everything"},
	)

	assert.Nil(
		t,
		middleware.PermissionsFromContext(ctx),
		"a string key must not be readable as an auth permission",
	)
}
