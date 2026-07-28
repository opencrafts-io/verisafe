package activity_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/handlers/activity"
	"github.com/opencrafts-io/verisafe/internal/repository"
	activitysvc "github.com/opencrafts-io/verisafe/internal/service/activity"
	"github.com/opencrafts-io/verisafe/internal/testsupport"
	"go.uber.org/mock/gomock"
)

// stubCreateService satisfies activitysvc.Service by embedding it as a nil
// interface and overriding only Create, the one method the commit-failure
// test below needs. This bypasses the real repository.Querier entirely, so
// the test does not have to mock every SQL call CreateActivity's real service
// implementation would make -- only the transaction lifecycle around it.
type stubCreateService struct {
	activitysvc.Service
}

func (stubCreateService) Create(
	context.Context,
	repository.CreateActivityParams,
) (repository.Activity, error) {
	return repository.Activity{}, nil
}

// Byte-exact characterisation of each branch, so the service extraction can be
// shown to have moved nothing observable. See the role handler's tests for the
// reasoning behind driving the handler directly.

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// This was the first migrated handler that used http.Error, moved to
// core.WriteError per ADR 0009. This test now asserts application/json rather
// than the text/plain it asserted before this migration.
func TestGetAllActivities_ConnectionAcquisitionFailure(t *testing.T) {
	h := &activity.ActivityHandler{
		Logger: discardLogger(),
		DB:     testsupport.FailingAcquireDB(t, errors.New("pool exhausted")),
	}

	testsupport.WireCase{
		Handler:         core.AppHandler(h.GetAllActivities),
		Request:         httptest.NewRequest("GET", "/activity/all", nil),
		WantStatus:      500,
		WantContentType: "application/json",
		WantBody:        `{"error":"internal server error"}` + "\n",
	}.Run(t)
}

func TestGetAllActivities_BeginFailureIsADistinctMessage(t *testing.T) {
	h := &activity.ActivityHandler{
		Logger: discardLogger(),
		DB: testsupport.FailingBeginDB(
			t, errors.New("server closed the connection"),
		),
	}

	testsupport.WireCase{
		Handler:         core.AppHandler(h.GetAllActivities),
		Request:         httptest.NewRequest("GET", "/activity/all", nil),
		WantStatus:      500,
		WantContentType: "application/json",
		WantBody:        `{"error":"Cannot process your request at the moment"}` + "\n",
	}.Run(t)
}

func TestGetAllUserActivityCompletions_InvalidIDIsRejectedBeforeAnyDatabaseWork(t *testing.T) {
	req := httptest.NewRequest(
		"GET", "/users/activity/completions/for-user/not-a-uuid", nil,
	)
	req.SetPathValue("id", "not-a-uuid")

	testsupport.WireCase{
		Handler: core.AppHandler(
			(&activity.ActivityHandler{Logger: discardLogger()}).GetAllUserActivityCompletions,
		),
		Request:         req,
		Authenticated:   true,
		WantStatus:      400,
		WantContentType: "application/json",
		// This endpoint's own wording: "try that again", not "try again".
		WantBody: `{"error":"Please check your request body and try that again"}` + "\n",
	}.Run(t)
}

func TestCreateActivity_MalformedBodyIsRejectedBeforeAnyDatabaseWork(t *testing.T) {
	req := httptest.NewRequest(
		"POST", "/activity/add", strings.NewReader("{not json"),
	)

	testsupport.WireCase{
		Handler: core.AppHandler(
			(&activity.ActivityHandler{Logger: discardLogger()}).CreateActivity,
		),
		Request:         req,
		Authenticated:   true,
		WantStatus:      400,
		WantContentType: "application/json",
		WantBody:        `{"error":"Please check your request body and try again"}` + "\n",
	}.Run(t)
}

// Commit failure gets a different message (msgGeneric) than Begin failure
// (msgCannotProcess) or a repo-call failure (also msgCannotProcess, for this
// endpoint) -- this locks that three-way distinction, which is why the write
// methods use acquireRunAndCommit rather than the simpler acquireAndRun the
// four list methods share.
func TestCreateActivity_CommitFailureIsADifferentMessageFromBeginFailure(t *testing.T) {
	tx := testsupport.NewTx(t)
	tx.EXPECT().Commit(gomock.Any()).Return(errors.New("commit failed"))

	req := httptest.NewRequest(
		"POST", "/activity/add", strings.NewReader(`{"name":"x"}`),
	)

	h := &activity.ActivityHandler{
		Logger: discardLogger(),
		DB:     testsupport.TxDB(t, tx),
		Service: func(repository.Querier) activitysvc.Service {
			return stubCreateService{}
		},
	}

	testsupport.WireCase{
		Handler:         core.AppHandler(h.CreateActivity),
		Request:         req,
		WantStatus:      500,
		WantContentType: "application/json",
		WantBody:        `{"error":"We ran into a problem while servicing your request please try again later"}` + "\n",
	}.Run(t)
}
