package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/opencrafts-io/verisafe/internal/middleware"
	"github.com/stretchr/testify/assert"
)

func nextHandlerCalled(t *testing.T) (http.Handler, *bool) {
	t.Helper()
	called := false
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}), &called
}

func TestHasPermission(t *testing.T) {
	t.Run("required permission present", func(t *testing.T) {
		next, called := nextHandlerCalled(t)
		handler := middleware.HasPermission([]string{"read:role:any"})(next)

		req := httptest.NewRequest("GET", "/roles", nil)
		ctx := middleware.WithPermissions(req.Context(), []string{"read:role:any"})
		req = req.WithContext(ctx)
		rr := httptest.NewRecorder()

		handler.ServeHTTP(rr, req)

		assert.True(t, *called)
		assert.Equal(t, http.StatusOK, rr.Code)
	})

	t.Run("required permission missing", func(t *testing.T) {
		next, called := nextHandlerCalled(t)
		handler := middleware.HasPermission([]string{"read:role:any"})(next)

		req := httptest.NewRequest("GET", "/roles", nil)
		ctx := middleware.WithPermissions(req.Context(), []string{"create:role"})
		req = req.WithContext(ctx)
		rr := httptest.NewRecorder()

		handler.ServeHTTP(rr, req)

		assert.False(t, *called)
		assert.Equal(t, http.StatusForbidden, rr.Code)
	})

	t.Run("multiple required permissions is AND, not OR", func(t *testing.T) {
		next, called := nextHandlerCalled(t)
		handler := middleware.HasPermission([]string{"read:role:any", "update:role:any"})(next)

		req := httptest.NewRequest("PATCH", "/roles/1", nil)
		// Caller has only one of the two required permissions.
		ctx := middleware.WithPermissions(req.Context(), []string{"read:role:any"})
		req = req.WithContext(ctx)
		rr := httptest.NewRecorder()

		handler.ServeHTTP(rr, req)

		assert.False(t, *called, "having only one of two required permissions must not be enough")
		assert.Equal(t, http.StatusForbidden, rr.Code)
	})

	t.Run("multiple required permissions all present succeeds", func(t *testing.T) {
		next, called := nextHandlerCalled(t)
		handler := middleware.HasPermission([]string{"read:role:any", "update:role:any"})(next)

		req := httptest.NewRequest("PATCH", "/roles/1", nil)
		ctx := middleware.WithPermissions(req.Context(), []string{"read:role:any", "update:role:any"})
		req = req.WithContext(ctx)
		rr := httptest.NewRecorder()

		handler.ServeHTTP(rr, req)

		assert.True(t, *called)
		assert.Equal(t, http.StatusOK, rr.Code)
	})

	t.Run("missing permissions in context denies safely, does not panic", func(t *testing.T) {
		next, called := nextHandlerCalled(t)
		handler := middleware.HasPermission([]string{"read:role:any"})(next)

		req := httptest.NewRequest("GET", "/roles", nil)
		rr := httptest.NewRecorder()

		assert.NotPanics(t, func() {
			handler.ServeHTTP(rr, req)
		})
		assert.False(t, *called)
		assert.Equal(t, http.StatusForbidden, rr.Code)
	})

	t.Run("empty required permissions list always succeeds", func(t *testing.T) {
		next, called := nextHandlerCalled(t)
		handler := middleware.HasPermission(nil)(next)

		req := httptest.NewRequest("GET", "/anything", nil)
		rr := httptest.NewRecorder()

		handler.ServeHTTP(rr, req)

		assert.True(t, *called)
		assert.Equal(t, http.StatusOK, rr.Code)
	})
}
