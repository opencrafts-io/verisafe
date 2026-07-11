package auth

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/opencrafts-io/verisafe/internal/core"
	mockscore "github.com/opencrafts-io/verisafe/internal/core/mocks"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

// ExchangeAuthCodeHandler is the one AppHandler-wrapped auth endpoint that
// touches no database at all (only the cacher), so it's fully testable here
// without the real-Postgres-or-brittle-SQL-mock problem that blocks
// RefreshTokenHandler/RevokeTokenHandler/CallbackHandler's DB-touching paths
// (see the "not covered" note on those below).

func TestExchangeAuthCodeHandler(t *testing.T) {
	t.Run("valid code returns the stored token pair", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		cacher := mockscore.NewMockCacher(ctrl)

		want := tokenResponse{
			AccessToken:      "at",
			RefreshToken:     "rt",
			AccessExpiresAt:  time.Now().Add(time.Hour),
			RefreshExpiresAt: time.Now().Add(24 * time.Hour),
		}

		cacher.EXPECT().
			Get(gomock.Any(), authCodePrefix+"validcode", gomock.Any()).
			DoAndReturn(func(_ context.Context, _ string, dest any) error {
				*dest.(*tokenResponse) = want
				return nil
			})
		cacher.EXPECT().
			Delete(gomock.Any(), authCodePrefix+"validcode").
			Return(nil)

		h := &AuthHandler{cacher: cacher, logger: testLogger()}
		req := httptest.NewRequest(
			"POST", "/auth/token/exchange", strings.NewReader(`{"code":"validcode"}`),
		)
		rr := httptest.NewRecorder()

		err := h.ExchangeAuthCodeHandler(rr, req)

		assert.NoError(t, err)
		assert.Equal(t, 200, rr.Code)
		var got tokenResponse
		assert.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
		assert.Equal(t, want.AccessToken, got.AccessToken)
		assert.Equal(t, want.RefreshToken, got.RefreshToken)
	})

	t.Run("missing code", func(t *testing.T) {
		h := &AuthHandler{logger: testLogger()}
		req := httptest.NewRequest("POST", "/auth/token/exchange", strings.NewReader(`{}`))
		rr := httptest.NewRecorder()

		err := h.ExchangeAuthCodeHandler(rr, req)

		assert.ErrorIs(t, err, core.ErrInvalidInput)
	})

	t.Run("expired or unknown code", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		cacher := mockscore.NewMockCacher(ctrl)
		cacher.EXPECT().
			Get(gomock.Any(), authCodePrefix+"gonecode", gomock.Any()).
			Return(core.ErrCacheMiss)

		h := &AuthHandler{cacher: cacher, logger: testLogger()}
		req := httptest.NewRequest(
			"POST", "/auth/token/exchange", strings.NewReader(`{"code":"gonecode"}`),
		)
		rr := httptest.NewRecorder()

		err := h.ExchangeAuthCodeHandler(rr, req)

		assert.ErrorIs(t, err, core.ErrUnauthorized)
	})
}

// TestRefreshTokenHandler_InputValidation covers only the early-return
// validation branch, which runs before any database access. The success
// path (token rotation) goes through h.db.Acquire -> core.WithTransaction ->
// repository.New(tx), issuing real generated SQL through whatever pgx.Tx it's
// given — testing that meaningfully needs a real Postgres or brittle
// per-statement mocking of a mocked pgx.Tx, not something to fake here. Same
// reasoning applies to RevokeTokenHandler's post-auth DB path and to
// CallbackHandler as a whole (see build/tech-debt-report). This is an
// accepted, documented coverage gap, not an oversight.
func TestRefreshTokenHandler_InputValidation(t *testing.T) {
	h := &AuthHandler{logger: testLogger()}
	req := httptest.NewRequest(
		"POST", "/auth/token/refresh", strings.NewReader(`{"refresh_token":""}`),
	)
	rr := httptest.NewRecorder()

	err := h.RefreshTokenHandler(rr, req)

	assert.ErrorIs(t, err, core.ErrInvalidInput)
}

// TestRevokeTokenHandler_MissingClaims covers only the early-return
// authorization check, which runs before any database access. See the
// TestRefreshTokenHandler_InputValidation comment above for why the
// DB-touching success path isn't covered here.
func TestRevokeTokenHandler_MissingClaims(t *testing.T) {
	h := &AuthHandler{logger: testLogger()}
	req := httptest.NewRequest("POST", "/auth/token/revoke", strings.NewReader(`{}`))
	rr := httptest.NewRecorder()

	err := h.RevokeTokenHandler(rr, req)

	assert.ErrorIs(t, err, core.ErrUnauthorized)
}
