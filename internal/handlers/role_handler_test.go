package handlers_test

import (
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/opencrafts-io/verisafe/internal/handlers"
	"github.com/stretchr/testify/assert"
)

// GetRoleByID/UpdateRole construct repository.New(tx) internally from a real
// transaction acquired via middleware.GetDBConnFromContext -- meaningful
// coverage of their DB-touching paths needs a real Postgres or brittle
// per-statement mocking of a mocked pgx.Tx, the same accepted gap already
// documented for internal/auth's DB-touching handlers. Only the before-DB
// input-validation branch is covered here.

func TestGetRoleByID_InvalidID(t *testing.T) {
	rh := &handlers.RoleHandler{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}

	req := httptest.NewRequest("GET", "/roles/not-a-uuid", nil)
	req.SetPathValue("id", "not-a-uuid")
	rr := httptest.NewRecorder()

	rh.GetRoleByID(rr, req)

	assert.Equal(t, 400, rr.Code)
}
