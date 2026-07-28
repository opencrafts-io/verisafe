package social_test

import (
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/handlers/social"
	"github.com/opencrafts-io/verisafe/internal/middleware"
	"github.com/opencrafts-io/verisafe/internal/testsupport"
	"github.com/opencrafts-io/verisafe/internal/tokens"
)

// Byte-exact characterisation of each branch, so the service extraction can be
// shown to have moved nothing observable. See the role handler's tests for the
// reasoning behind driving the handler directly and pre-seeding Content-Type.

const (
	msgGeneric   = "We ran into a problem while servicing your request please try again later"
	msgCheckBody = "Please check your request body and try again"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestGetUserIDSocials(t *testing.T) {
	t.Run("user_id that is not a uuid is rejected before any database work", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/socials/user/not-a-uuid", nil)
		req.SetPathValue("user_id", "not-a-uuid")

		testsupport.WireCase{
			Handler: core.AppHandler(
				(&social.SocialHandler{Logger: discardLogger()}).GetUserIDSocials,
			),
			Request:         req,
			Authenticated:   true,
			WantStatus:      400,
			WantContentType: "application/json",
			WantBody:        `{"error":"` + msgCheckBody + `"}` + "\n",
		}.Run(t)
	})

	t.Run("connection acquisition failure is a 500", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/socials/user/x", nil)
		req.SetPathValue("user_id", "6f1b6b1e-0000-4000-8000-000000000000")

		h := &social.SocialHandler{
			Logger: discardLogger(),
			DB:     testsupport.FailingAcquireDB(t, errors.New("pool exhausted")),
		}

		testsupport.WireCase{
			Handler:         core.AppHandler(h.GetUserIDSocials),
			Request:         req,
			WantStatus:      500,
			WantContentType: "application/json",
			WantBody:        `{"error":"` + msgGeneric + `"}` + "\n",
		}.Run(t)
	})
}

func TestGetAllUserSocials(t *testing.T) {
	t.Run("missing claims is unauthorized", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/socials/me", nil)

		testsupport.WireCase{
			Handler: core.AppHandler(
				(&social.SocialHandler{Logger: discardLogger()}).GetAllUserSocials,
			),
			Request:         req,
			Authenticated:   true,
			WantStatus:      401,
			WantContentType: "application/json",
			WantBody:        `{"error":"authentication required"}` + "\n",
		}.Run(t)
	})

	t.Run("connection acquisition failure is a 500", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/socials/me", nil)
		req = req.WithContext(middleware.WithClaims(req.Context(),
			&tokens.VerisafeClaims{RegisteredClaims: jwt.RegisteredClaims{
				Subject: "6f1b6b1e-0000-4000-8000-000000000000",
			}},
		))

		h := &social.SocialHandler{
			Logger: discardLogger(),
			DB:     testsupport.FailingAcquireDB(t, errors.New("pool exhausted")),
		}

		testsupport.WireCase{
			Handler:         core.AppHandler(h.GetAllUserSocials),
			Request:         req,
			WantStatus:      500,
			WantContentType: "application/json",
			WantBody:        `{"error":"` + msgGeneric + `"}` + "\n",
		}.Run(t)
	})
}
