package social_test

import (
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/opencrafts-io/verisafe/internal/middleware"
	"github.com/opencrafts-io/verisafe/internal/tokens"

	"github.com/opencrafts-io/verisafe/internal/handlers/social"
	"github.com/opencrafts-io/verisafe/internal/testsupport"
)

// Characterises the branch every method in this handler runs when the pool is
// exhausted or the database is unreachable -- the single most repeated error
// path in the package. Asserting Content-Type as well as the body is the point:
// this handler reports failures through an inline JSON encode, and a later change to
// core.WriteError would alter the header. That change is intended and agreed,
// but it must be visible here rather than silent.

func TestSocialHandler_ConnectionAcquisitionFailure(t *testing.T) {
	h := &social.SocialHandler{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		DB:     testsupport.FailingAcquireDB(t, errors.New("pool exhausted")),
	}

	req := httptest.NewRequest("GET", "/socials/me", nil)
	req = req.WithContext(middleware.WithClaims(req.Context(),
		&tokens.VerisafeClaims{RegisteredClaims: jwt.RegisteredClaims{
			Subject: "6f1b6b1e-0000-4000-8000-000000000000",
		}},
	))

	testsupport.WireCase{
		Handler:         h.GetAllUserSocials,
		Request:         req,
		WantStatus:      500,
		WantContentType: "application/json",
		WantBody:        `{"error":"We ran into a problem while servicing your request please try again later"}` + "\n",
	}.Run(t)
}
