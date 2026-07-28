package device_test

import (
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/handlers/device"
	"github.com/opencrafts-io/verisafe/internal/middleware"
	"github.com/opencrafts-io/verisafe/internal/testsupport"
	"github.com/opencrafts-io/verisafe/internal/tokens"
	"github.com/stretchr/testify/assert"
)

// This handler already returns an error and is wrapped in core.AppHandler, so
// unlike its siblings it never writes an error response itself. These assert
// the sentinel it returns, which is what determines the status the adapter
// then writes, plus the bytes that come out the far end.

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestGetPersonalDevices_WithoutClaimsIsUnauthorized(t *testing.T) {
	h := &device.DeviceHandler{Logger: discardLogger()}

	err := h.GetPersonalDevices(
		httptest.NewRecorder(),
		httptest.NewRequest("GET", "/devices/mine", nil),
	)

	assert.ErrorIs(t, err, core.ErrUnauthorized)
}

func TestGetPersonalDevices_SubjectThatIsNotAUUIDIsRejected(t *testing.T) {
	h := &device.DeviceHandler{Logger: discardLogger()}

	req := httptest.NewRequest("GET", "/devices/mine", nil)
	req = req.WithContext(middleware.WithClaims(req.Context(),
		&tokens.VerisafeClaims{
			RegisteredClaims: jwt.RegisteredClaims{Subject: "not-a-uuid"},
		},
	))

	err := h.GetPersonalDevices(httptest.NewRecorder(), req)

	assert.Error(t, err)
}

// The end-to-end shape: a failed acquire becomes ErrInternal, which
// core.AppHandler renders as a 500 that does not leak the underlying cause.
func TestGetPersonalDevices_AcquireFailureRendersAsAGeneric500(t *testing.T) {
	h := &device.DeviceHandler{
		Logger: discardLogger(),
		DB:     testsupport.FailingAcquireDB(t, errors.New("pool exhausted")),
	}

	req := httptest.NewRequest("GET", "/devices/mine", nil)
	req = req.WithContext(middleware.WithClaims(req.Context(),
		&tokens.VerisafeClaims{
			RegisteredClaims: jwt.RegisteredClaims{
				Subject: "6f1b6b1e-0000-4000-8000-000000000000",
			},
		},
	))

	rr := httptest.NewRecorder()
	core.AppHandler(h.GetPersonalDevices).ServeHTTP(rr, req)

	assert.Equal(t, 500, rr.Code)
	assert.Equal(t, `{"error":"something went wrong"}`+"\n", rr.Body.String())
	assert.NotContains(t, rr.Body.String(), "pool exhausted")
}
