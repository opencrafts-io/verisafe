package handlers_test

import (
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/opencrafts-io/verisafe/internal/handlers"
	"github.com/stretchr/testify/assert"
)

// See role_handler_test.go for why only the before-DB input-validation
// branch is covered here, not the DB-touching CRUD path.

func TestGetPermissionByID_InvalidID(t *testing.T) {
	ph := &handlers.PermissionHandler{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}

	req := httptest.NewRequest("GET", "/permissions/not-a-uuid", nil)
	req.SetPathValue("id", "not-a-uuid")
	rr := httptest.NewRecorder()

	ph.GetPermissionByID(rr, req)

	assert.Equal(t, 400, rr.Code)
}
