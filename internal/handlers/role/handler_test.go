package role_test

import (
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/opencrafts-io/verisafe/internal/handlers/role"
	"github.com/opencrafts-io/verisafe/internal/testsupport"
	"github.com/stretchr/testify/assert"
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
			Handler:         (&role.RoleHandler{Logger: discardLogger()}).GetRoleByID,
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
			Handler:         h.GetRoleByID,
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
		Handler:         h.CreateRole,
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
		Handler:         h.GetAllRoles,
		Request:         httptest.NewRequest("GET", "/roles", nil),
		WantStatus:      500,
		WantContentType: "application/json",
		WantBody:        `{"error":"` + msgGeneric + `"}` + "\n",
	}.Run(t)
}

// DEFECT, pinned rather than asserted as correct behaviour.
//
// Every method in this handler does `tx, _ := conn.Begin(ctx)`, discarding the
// error, then immediately `defer tx.Rollback(ctx)`. When Begin fails tx is a
// nil interface and the deferred call panics, so the client gets a dropped
// connection and net/http logs a stack trace, rather than the 500 every other
// failure path produces.
//
// This test exists so the fix is visible as a change to this file. Once the
// handler moves to a helper that owns the transaction lifecycle, replace it
// with an assertion of 500 plus msgGeneric.
func TestGetRoleByID_BeginFailurePanics_KnownDefect(t *testing.T) {
	req := httptest.NewRequest("GET", "/roles/x", nil)
	req.SetPathValue("id", "6f1b6b1e-0000-4000-8000-000000000000")

	h := &role.RoleHandler{
		Logger: discardLogger(),
		DB: testsupport.FailingBeginDB(
			t, errors.New("server closed the connection"),
		),
	}

	assert.Panics(t, func() {
		h.GetRoleByID(httptest.NewRecorder(), req)
	}, "when this stops panicking, replace this test with a 500 assertion")
}
