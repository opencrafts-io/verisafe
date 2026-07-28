package institution_test

import (
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/opencrafts-io/verisafe/internal/handlers/institution"
	"github.com/opencrafts-io/verisafe/internal/testsupport"
)

// Characterises the branch every method in this handler runs when the pool is
// exhausted or the database is unreachable -- the single most repeated error
// path in the package. Asserting Content-Type as well as the body is the point:
// this handler reports failures through http.Error, and a later change to
// core.WriteError would alter the header. That change is intended and agreed,
// but it must be visible here rather than silent.

func TestInstitutionHandler_ConnectionAcquisitionFailure(t *testing.T) {
	h := &institution.InstitutionHandler{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		DB:     testsupport.FailingAcquireDB(t, errors.New("pool exhausted")),
	}

	req := httptest.NewRequest("GET", "/institutions/all", nil)

	testsupport.WireCase{
		Handler:         h.GetAllInstitutions,
		Request:         req,
		WantStatus:      500,
		WantContentType: "text/plain; charset=utf-8",
		WantBody:        `{"error":"internal server error"}` + "\n",
	}.Run(t)
}
