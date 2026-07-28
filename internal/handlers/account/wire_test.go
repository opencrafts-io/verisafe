package account_test

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/opencrafts-io/verisafe/internal/handlers/account"
	"github.com/opencrafts-io/verisafe/internal/testsupport"
)

// Characterises the branch every method in this handler runs when the pool is
// exhausted or the database is unreachable -- the single most repeated error
// path in the package. Asserting Content-Type as well as the body is the point:
// this handler reports failures through an inline JSON encode, and a later change to
// core.WriteError would alter the header. That change is intended and agreed,
// but it must be visible here rather than silent.

func TestAccountHandler_ConnectionAcquisitionFailure(t *testing.T) {
	h := &account.AccountHandler{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		DB:     testsupport.FailingAcquireDB(t, errors.New("pool exhausted")),
	}

	req := httptest.NewRequest("GET", "/accounts/all", nil)

	testsupport.WireCase{
		Handler:         http.HandlerFunc(h.GetAllUserAccounts),
		Request:         req,
		WantStatus:      500,
		WantContentType: "application/json",
		WantBody:        `{"error":"We ran into a problem while servicing your request please try again later"}` + "\n",
	}.Run(t)
}
