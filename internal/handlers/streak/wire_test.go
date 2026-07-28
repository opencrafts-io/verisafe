package streak_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/handlers/streak"
	"github.com/opencrafts-io/verisafe/internal/middleware"
	"github.com/opencrafts-io/verisafe/internal/repository"
	streaksvc "github.com/opencrafts-io/verisafe/internal/service/streak"
	"github.com/opencrafts-io/verisafe/internal/testsupport"
	"github.com/opencrafts-io/verisafe/internal/tokens"
	"go.uber.org/mock/gomock"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// stubRecordService satisfies streaksvc.Service by embedding it as a nil
// interface and overriding only RecordActivity, so the commit-failure test
// below does not have to mock every SQL call the real service would make --
// only the transaction lifecycle around it.
type stubRecordService struct {
	streaksvc.Service
}

func (stubRecordService) RecordActivity(
	context.Context,
	repository.RecordActivityCompletionParams,
) (repository.RecordActivityCompletionRow, error) {
	return repository.RecordActivityCompletionRow{}, nil
}

// This was the first migrated handler that used http.Error, moved to
// core.WriteError per ADR 0009. This test now asserts application/json
// rather than the text/plain it asserted before this migration.
func TestGetAllActiveStreakAchievements_ConnectionAcquisitionFailure(t *testing.T) {
	h := &streak.StreakHandler{
		Logger: discardLogger(),
		DB:     testsupport.FailingAcquireDB(t, errors.New("pool exhausted")),
	}

	testsupport.WireCase{
		Handler: core.AppHandler(h.GetAllActiveStreakAchievements),
		Request: httptest.NewRequest(
			"GET", "/streaks/milestone/active", nil,
		),
		WantStatus:      500,
		WantContentType: "application/json",
		WantBody:        `{"error":"internal server error"}` + "\n",
	}.Run(t)
}

func TestGetAllActiveStreakAchievements_BeginFailureIsADistinctMessage(t *testing.T) {
	h := &streak.StreakHandler{
		Logger: discardLogger(),
		DB: testsupport.FailingBeginDB(
			t, errors.New("server closed the connection"),
		),
	}

	testsupport.WireCase{
		Handler: core.AppHandler(h.GetAllActiveStreakAchievements),
		Request: httptest.NewRequest(
			"GET", "/streaks/milestone/active", nil,
		),
		WantStatus:      500,
		WantContentType: "application/json",
		WantBody:        `{"error":"Cannot process your request at the moment"}` + "\n",
	}.Run(t)
}

func authedClaims(subject string) *tokens.VerisafeClaims {
	return &tokens.VerisafeClaims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: subject},
	}
}

func TestRecordUserActivity_MalformedBodyIsRejectedBeforeAnyDatabaseWork(t *testing.T) {
	req := httptest.NewRequest(
		"POST", "/users/activity/complete", strings.NewReader("{not json"),
	)

	testsupport.WireCase{
		Handler: core.AppHandler(
			(&streak.StreakHandler{Logger: discardLogger()}).RecordUserActivity,
		),
		Request:         req,
		Authenticated:   true,
		WantStatus:      400,
		WantContentType: "application/json",
		WantBody:        `{"error":"Please check your request body and try again"}` + "\n",
	}.Run(t)
}

// The ownership check runs after decoding but still before any database
// work: a caller cannot spend an acquired connection to learn they were
// going to be rejected anyway.
func TestRecordUserActivity_MismatchedAccountIDIsForbiddenBeforeAnyDatabaseWork(t *testing.T) {
	body := `{"account_id":"6f1b6b1e-0000-4000-8000-000000000001"}`
	req := httptest.NewRequest(
		"POST", "/users/activity/complete", strings.NewReader(body),
	)
	req = req.WithContext(middleware.WithClaims(
		req.Context(),
		authedClaims("6f1b6b1e-0000-4000-8000-000000000002"),
	))

	testsupport.WireCase{
		Handler: core.AppHandler(
			(&streak.StreakHandler{Logger: discardLogger()}).RecordUserActivity,
		),
		Request:         req,
		WantStatus:      403,
		WantContentType: "application/json",
		WantBody:        `{"error":"you can only record activity completions for your own account"}` + "\n",
	}.Run(t)
}

// Commit failure gets a different message (msgGeneric) than Begin failure or
// a repo-call failure (both msgCannotProcess) -- the same three-way
// distinction activity's write methods needed, exercised here through a stub
// service so only the mock transaction's Commit needs stubbing.
func TestRecordUserActivity_CommitFailureIsADifferentMessageFromBeginFailure(t *testing.T) {
	subject := "6f1b6b1e-0000-4000-8000-000000000003"
	body := `{"account_id":"` + subject + `"}`

	tx := testsupport.NewTx(t)
	tx.EXPECT().Commit(gomock.Any()).Return(errors.New("commit failed"))

	req := httptest.NewRequest(
		"POST", "/users/activity/complete", strings.NewReader(body),
	)
	req = req.WithContext(middleware.WithClaims(req.Context(), authedClaims(subject)))

	h := &streak.StreakHandler{
		Logger: discardLogger(),
		DB:     testsupport.TxDB(t, tx),
		Service: func(repository.Querier) streaksvc.Service {
			return stubRecordService{}
		},
	}

	testsupport.WireCase{
		Handler:         core.AppHandler(h.RecordUserActivity),
		Request:         req,
		WantStatus:      500,
		WantContentType: "application/json",
		WantBody:        `{"error":"We ran into a problem while servicing your request please try again later"}` + "\n",
	}.Run(t)
}

func TestDeleteStreakMilestone_InvalidIDIsRejectedBeforeAnyDatabaseWork(t *testing.T) {
	req := httptest.NewRequest(
		"DELETE", "/streaks/milestone/not-a-uuid", nil,
	)
	req.SetPathValue("id", "not-a-uuid")

	testsupport.WireCase{
		Handler: core.AppHandler(
			(&streak.StreakHandler{Logger: discardLogger()}).DeleteStreakMilestone,
		),
		Request:         req,
		Authenticated:   true,
		WantStatus:      400,
		WantContentType: "application/json",
		WantBody:        `{"error":"Please check your request body and try again"}` + "\n",
	}.Run(t)
}
