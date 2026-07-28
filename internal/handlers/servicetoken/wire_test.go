package servicetoken_test

import (
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/opencrafts-io/verisafe/internal/handlers/servicetoken"
	"github.com/opencrafts-io/verisafe/internal/testsupport"
)

// Characterises the branch every method in this handler runs when the pool is
// exhausted or the database is unreachable -- the single most repeated error
// path in the package. Asserting Content-Type as well as the body is the point:
// this handler reports failures through http.Error, and a later change to
// core.WriteError would alter the header. That change is intended and agreed,
// but it must be visible here rather than silent.

func TestServiceTokenHandler_ConnectionAcquisitionFailure(t *testing.T) {
	h := &servicetoken.ServiceTokenHandler{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		DB:     testsupport.FailingAcquireDB(t, errors.New("pool exhausted")),
	}

	req := httptest.NewRequest("GET", "/api/v1/admin/service-tokens", nil)

	testsupport.WireCase{
		Handler:         h.ListAllServiceTokens,
		Request:         req,
		WantStatus:      500,
		WantContentType: "text/plain; charset=utf-8",
		WantBody:        "Internal server error\n",
	}.Run(t)
}
