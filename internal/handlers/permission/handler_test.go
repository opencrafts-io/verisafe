package permission_test

import (
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/opencrafts-io/verisafe/internal/handlers/permission"
	"github.com/opencrafts-io/verisafe/internal/testsupport"
	"github.com/stretchr/testify/assert"
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
			Handler: (&permission.PermissionHandler{
				Logger: discardLogger(),
			}).GetPermissionByID,
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
			Handler:         h.GetPermissionByID,
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
		Handler: h.CreatePermission,
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
		Handler:         h.GetAllPermissions,
		Request:         httptest.NewRequest("GET", "/permissions", nil),
		WantStatus:      500,
		WantContentType: "application/json",
		WantBody:        `{"error":"` + msgGeneric + `"}` + "\n",
	}.Run(t)
}

// Same discarded-Begin-error defect as the role handler; see the note there.
func TestGetPermissionByID_BeginFailurePanics_KnownDefect(t *testing.T) {
	req := httptest.NewRequest("GET", "/permissions/x", nil)
	req.SetPathValue("id", "6f1b6b1e-0000-4000-8000-000000000000")

	h := &permission.PermissionHandler{
		Logger: discardLogger(),
		DB: testsupport.FailingBeginDB(
			t, errors.New("server closed the connection"),
		),
	}

	assert.Panics(t, func() {
		h.GetPermissionByID(httptest.NewRecorder(), req)
	}, "when this stops panicking, replace this test with a 500 assertion")
}
