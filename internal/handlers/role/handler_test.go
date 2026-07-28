package role_test

import (
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/handlers/role"
	"github.com/opencrafts-io/verisafe/internal/testsupport"
)

// These lock the exact bytes each branch puts on the wire, so that extracting
// a service layer out of this handler can be shown to have moved nothing
// observable. They are expected to pass unchanged through that refactor.
//
// The handler is invoked directly rather than through its middleware stack.
// Where a branch returns before the handler sets Content-Type itself, the case
// is marked Authenticated so the harness pre-seeds the header the way
// IsAuthenticated does in production; otherwise the golden would record Go's
// default rather than what a client actually receives.

const (
	msgGeneric   = "We ran into a problem while servicing your request please try again later"
	msgCheckBody = "Please check your request body and try again"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestGetRoleByID(t *testing.T) {
	t.Run("id that is not a uuid is rejected before any database work", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/roles/not-a-uuid", nil)
		req.SetPathValue("id", "not-a-uuid")

		// This branch returns before the handler's own Content-Type call, so
		// in production the header comes from IsAuthenticated.
		testsupport.WireCase{
			Handler: core.AppHandler(
				(&role.RoleHandler{Logger: discardLogger()}).GetRoleByID,
			),
			Request:         req,
			Authenticated:   true,
			WantStatus:      400,
			WantContentType: "application/json",
			WantBody:        `{"error":"` + msgCheckBody + `"}` + "\n",
		}.Run(t)
	})

	t.Run("connection acquisition failure is a 500", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/roles/x", nil)
		req.SetPathValue("id", "6f1b6b1e-0000-4000-8000-000000000000")

		h := &role.RoleHandler{
			Logger: discardLogger(),
			DB:     testsupport.FailingAcquireDB(t, errors.New("pool exhausted")),
		}

		testsupport.WireCase{
			Handler:         core.AppHandler(h.GetRoleByID),
			Request:         req,
			WantStatus:      500,
			WantContentType: "application/json",
			WantBody:        `{"error":"` + msgGeneric + `"}` + "\n",
		}.Run(t)
	})
}

func TestCreateRole_MalformedBodyIsRejected(t *testing.T) {
	req := httptest.NewRequest(
		"POST", "/roles/create", strings.NewReader("{not json"),
	)

	h := &role.RoleHandler{
		Logger: discardLogger(),
		DB:     testsupport.TxDB(t, testsupport.NewTx(t)),
	}

	testsupport.WireCase{
		Handler:         core.AppHandler(h.CreateRole),
		Request:         req,
		WantStatus:      400,
		WantContentType: "application/json",
		WantBody:        `{"error":"` + msgCheckBody + `"}` + "\n",
	}.Run(t)
}

func TestGetAllRoles_ConnectionAcquisitionFailureIsA500(t *testing.T) {
	h := &role.RoleHandler{
		Logger: discardLogger(),
		DB:     testsupport.FailingAcquireDB(t, errors.New("pool exhausted")),
	}

	testsupport.WireCase{
		Handler:         core.AppHandler(h.GetAllRoles),
		Request:         httptest.NewRequest("GET", "/roles", nil),
		WantStatus:      500,
		WantContentType: "application/json",
		WantBody:        `{"error":"` + msgGeneric + `"}` + "\n",
	}.Run(t)
}

// This was a defect until the service extraction, and this test was written
// pinning it: every method did `tx, _ := conn.Begin(ctx)`, discarding the
// error, then `defer tx.Rollback(ctx)` on the resulting nil interface. A failed
// Begin panicked, so the client got a dropped connection and net/http logged a
// stack trace instead of the 500 every other failure path produced.
//
// core.InTx owns the transaction lifecycle and returns the Begin error, so the
// assertion is now what it always should have been. The other cases in this
// file were not touched by that change, which is the evidence that the
// extraction fixed this and moved nothing else.
func TestGetRoleByID_BeginFailureIsA500(t *testing.T) {
	req := httptest.NewRequest("GET", "/roles/x", nil)
	req.SetPathValue("id", "6f1b6b1e-0000-4000-8000-000000000000")

	h := &role.RoleHandler{
		Logger: discardLogger(),
		DB: testsupport.FailingBeginDB(
			t, errors.New("server closed the connection"),
		),
	}

	testsupport.WireCase{
		Handler:         core.AppHandler(h.GetRoleByID),
		Request:         req,
		WantStatus:      500,
		WantContentType: "application/json",
		WantBody:        `{"error":"` + msgGeneric + `"}` + "\n",
	}.Run(t)
}
