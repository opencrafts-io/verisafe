package permission_test

import (
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/handlers/permission"
	"github.com/opencrafts-io/verisafe/internal/testsupport"
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

func TestGetPermissionByID(t *testing.T) {
	t.Run("id that is not a uuid is rejected before any database work", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/permissions/not-a-uuid", nil)
		req.SetPathValue("id", "not-a-uuid")

		testsupport.WireCase{
			Handler: core.AppHandler((&permission.PermissionHandler{
				Logger: discardLogger(),
			}).GetPermissionByID),
			Request:         req,
			Authenticated:   true,
			WantStatus:      400,
			WantContentType: "application/json",
			WantBody:        `{"error":"` + msgCheckBody + `"}` + "\n",
		}.Run(t)
	})

	t.Run("connection acquisition failure is a 500", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/permissions/x", nil)
		req.SetPathValue("id", "6f1b6b1e-0000-4000-8000-000000000000")

		h := &permission.PermissionHandler{
			Logger: discardLogger(),
			DB:     testsupport.FailingAcquireDB(t, errors.New("pool exhausted")),
		}

		testsupport.WireCase{
			Handler:         core.AppHandler(h.GetPermissionByID),
			Request:         req,
			WantStatus:      500,
			WantContentType: "application/json",
			WantBody:        `{"error":"` + msgGeneric + `"}` + "\n",
		}.Run(t)
	})
}

func TestCreatePermission_MalformedBodyIsRejected(t *testing.T) {
	h := &permission.PermissionHandler{
		Logger: discardLogger(),
		DB:     testsupport.TxDB(t, testsupport.NewTx(t)),
	}

	testsupport.WireCase{
		Handler: core.AppHandler(h.CreatePermission),
		Request: httptest.NewRequest(
			"POST", "/permissions/create", strings.NewReader("{not json"),
		),
		WantStatus:      400,
		WantContentType: "application/json",
		WantBody:        `{"error":"` + msgCheckBody + `"}` + "\n",
	}.Run(t)
}

func TestGetAllPermissions_ConnectionAcquisitionFailureIsA500(t *testing.T) {
	h := &permission.PermissionHandler{
		Logger: discardLogger(),
		DB:     testsupport.FailingAcquireDB(t, errors.New("pool exhausted")),
	}

	testsupport.WireCase{
		Handler:         core.AppHandler(h.GetAllPermissions),
		Request:         httptest.NewRequest("GET", "/permissions", nil),
		WantStatus:      500,
		WantContentType: "application/json",
		WantBody:        `{"error":"` + msgGeneric + `"}` + "\n",
	}.Run(t)
}

// This handler carried the same discarded-Begin-error defect as the role one,
// and this test pinned the resulting panic. core.InTx returns that error, so
// it now asserts the 500 the endpoint should always have produced.
func TestGetPermissionByID_BeginFailureIsA500(t *testing.T) {
	req := httptest.NewRequest("GET", "/permissions/x", nil)
	req.SetPathValue("id", "6f1b6b1e-0000-4000-8000-000000000000")

	h := &permission.PermissionHandler{
		Logger: discardLogger(),
		DB: testsupport.FailingBeginDB(
			t, errors.New("server closed the connection"),
		),
	}

	testsupport.WireCase{
		Handler:         core.AppHandler(h.GetPermissionByID),
		Request:         req,
		WantStatus:      500,
		WantContentType: "application/json",
		WantBody:        `{"error":"` + msgGeneric + `"}` + "\n",
	}.Run(t)
}
