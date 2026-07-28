package institution_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/handlers/institution"
	"github.com/opencrafts-io/verisafe/internal/repository"
	institutionsvc "github.com/opencrafts-io/verisafe/internal/service/institution"
	"github.com/opencrafts-io/verisafe/internal/testsupport"
	"go.uber.org/mock/gomock"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// stubGetByIDFailsService satisfies institutionsvc.Service by embedding it as
// a nil interface and overriding only GetByID, to prove GetInstitutionByID
// treats ANY error -- not just a genuine not-found -- as a 404. That is what
// this endpoint did before the extraction (no errors.Is check existed at
// all), and is preserved rather than tightened.
type stubGetByIDFailsService struct {
	institutionsvc.Service
}

func (stubGetByIDFailsService) GetByID(
	context.Context, int32,
) (repository.Institution, error) {
	return repository.Institution{}, errors.New("driver hiccup, not a missing row")
}

// stubCreateService overrides only Create, so the commit-failure test below
// does not have to mock every SQL call the real service would make.
type stubCreateService struct {
	institutionsvc.Service
}

func (stubCreateService) Create(
	context.Context,
	repository.CreateInstitutionParams,
) (repository.Institution, error) {
	return repository.Institution{}, nil
}

// This was the first migrated handler that used http.Error, moved to
// core.WriteError per ADR 0009. This test now asserts application/json
// rather than the text/plain it asserted before this migration.
func TestGetAllInstitutions_ConnectionAcquisitionFailure(t *testing.T) {
	h := &institution.InstitutionHandler{
		Logger: discardLogger(),
		DB:     testsupport.FailingAcquireDB(t, errors.New("pool exhausted")),
	}

	testsupport.WireCase{
		Handler:         core.AppHandler(h.GetAllInstitutions),
		Request:         httptest.NewRequest("GET", "/institutions/all", nil),
		WantStatus:      500,
		WantContentType: "application/json",
		WantBody:        `{"error":"internal server error"}` + "\n",
	}.Run(t)
}

func TestRegisterInstitution_MalformedBodyIsRejectedBeforeAnyDatabaseWork(t *testing.T) {
	req := httptest.NewRequest(
		"POST", "/institutions/register", strings.NewReader("{not json"),
	)

	testsupport.WireCase{
		Handler: core.AppHandler(
			(&institution.InstitutionHandler{Logger: discardLogger()}).RegisterInstitution,
		),
		Request:         req,
		Authenticated:   true,
		WantStatus:      400,
		WantContentType: "application/json",
		WantBody:        `{"error":"invalid request body"}` + "\n",
	}.Run(t)
}

// Every write method in this handler used to do `tx, _ := conn.Begin(ctx)`,
// discarding the error, and then panic on the resulting nil transaction. Both
// core.InTx and this handler's own wording map a Begin failure to the SAME
// message an Acquire or Commit failure would produce ("internal server
// error") -- unlike activity and streak, this handler never distinguished
// Begin from Commit, so there is only one fallback message to preserve, and
// core.InTx's real fix (returning the error instead of panicking) is the only
// change visible here.
func TestRegisterInstitution_BeginFailureNoLongerPanics(t *testing.T) {
	h := &institution.InstitutionHandler{
		Logger: discardLogger(),
		DB: testsupport.FailingBeginDB(
			t, errors.New("server closed the connection"),
		),
	}

	req := httptest.NewRequest(
		"POST", "/institutions/register", strings.NewReader(`{"name":"x"}`),
	)

	testsupport.WireCase{
		Handler:         core.AppHandler(h.RegisterInstitution),
		Request:         req,
		WantStatus:      500,
		WantContentType: "application/json",
		WantBody:        `{"error":"internal server error"}` + "\n",
	}.Run(t)
}

func TestRegisterInstitution_CommitFailureIsTheSameMessageAsBeginFailure(t *testing.T) {
	tx := testsupport.NewTx(t)
	tx.EXPECT().Commit(gomock.Any()).Return(errors.New("commit failed"))

	h := &institution.InstitutionHandler{
		Logger: discardLogger(),
		DB:     testsupport.TxDB(t, tx),
		Service: func(repository.Querier) institutionsvc.Service {
			return stubCreateService{}
		},
	}

	req := httptest.NewRequest(
		"POST", "/institutions/register", strings.NewReader(`{"name":"x"}`),
	)

	testsupport.WireCase{
		Handler:         core.AppHandler(h.RegisterInstitution),
		Request:         req,
		WantStatus:      500,
		WantContentType: "application/json",
		WantBody:        `{"error":"internal server error"}` + "\n",
	}.Run(t)
}

func TestGetInstitutionByID_InvalidIDIsRejectedAfterAcquire(t *testing.T) {
	// Acquire happens before the id is validated for this endpoint -- unlike
	// most others -- which is the order this endpoint used before the
	// extraction, so the DB here must succeed at Acquire.
	h := &institution.InstitutionHandler{
		Logger: discardLogger(),
		DB:     testsupport.TxDB(t, testsupport.NewTx(t)),
	}

	req := httptest.NewRequest("GET", "/institutions/find/not-a-number", nil)
	req.SetPathValue("id", "not-a-number")

	testsupport.WireCase{
		Handler:         core.AppHandler(h.GetInstitutionByID),
		Request:         req,
		WantStatus:      400,
		WantContentType: "application/json",
		WantBody:        `{"error":"invalid institution id"}` + "\n",
	}.Run(t)
}

func TestGetInstitutionByID_AnyErrorIsA404(t *testing.T) {
	h := &institution.InstitutionHandler{
		Logger: discardLogger(),
		DB:     testsupport.TxDB(t, testsupport.NewTx(t)),
		Service: func(repository.Querier) institutionsvc.Service {
			return stubGetByIDFailsService{}
		},
	}

	req := httptest.NewRequest("GET", "/institutions/find/1", nil)
	req.SetPathValue("id", "1")

	testsupport.WireCase{
		Handler:         core.AppHandler(h.GetInstitutionByID),
		Request:         req,
		WantStatus:      404,
		WantContentType: "application/json",
		WantBody:        `{"error":"institution not found"}` + "\n",
	}.Run(t)
}

// The ownership check (own account_id, or the admin permission) runs before
// any database work: a caller cannot spend an acquired connection to learn
// they were going to be rejected anyway.
func TestAddAcountInstitution_OwnershipMismatchIsForbiddenBeforeAnyDatabaseWork(t *testing.T) {
	body := `{"account_id":"6f1b6b1e-0000-4000-8000-000000000001","institution_id":1}`
	req := httptest.NewRequest(
		"POST", "/institutions/account", strings.NewReader(body),
	)

	testsupport.WireCase{
		Handler: core.AppHandler(
			(&institution.InstitutionHandler{Logger: discardLogger()}).AddAcountInstitution,
		),
		Request:         req,
		Authenticated:   true,
		WantStatus:      403,
		WantContentType: "application/json",
		WantBody:        `{"error":"you can only manage your own institution memberships"}` + "\n",
	}.Run(t)
}

func TestDeleteInstitution_InvalidIDIsRejectedBeforeAnyDatabaseWork(t *testing.T) {
	req := httptest.NewRequest("DELETE", "/institutions/delete/x", nil)
	req.SetPathValue("id", "not-a-number")

	testsupport.WireCase{
		Handler: core.AppHandler(
			(&institution.InstitutionHandler{Logger: discardLogger()}).DeleteInstitution,
		),
		Request:         req,
		Authenticated:   true,
		WantStatus:      400,
		WantContentType: "application/json",
		WantBody:        `{"error":"invalid institution id"}` + "\n",
	}.Run(t)
}
