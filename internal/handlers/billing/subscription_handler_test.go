package billing

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/middleware"
	"github.com/opencrafts-io/verisafe/internal/repository"
	billingSvc "github.com/opencrafts-io/verisafe/internal/service/billing"
	"github.com/opencrafts-io/verisafe/internal/testsupport"
	"github.com/opencrafts-io/verisafe/internal/tokens"
)

type subscriptionStatusServiceStub struct {
	billingSvc.SubscriptionService

	status *billingSvc.SubscriptionStatus
	err    error
	userID uuid.UUID
}

func (s *subscriptionStatusServiceStub) GetStatus(
	_ context.Context,
	userID uuid.UUID,
) (*billingSvc.SubscriptionStatus, error) {
	s.userID = userID
	return s.status, s.err
}

func TestSubscriptionHandler_GetMySubscriptionStatusReturnsCallerStatus(t *testing.T) {
	userID := uuid.New()
	service := &subscriptionStatusServiceStub{status: &billingSvc.SubscriptionStatus{
		Active: true,
		Subscription: &billingSvc.Subscription{
			ID:       1,
			PlanCode: "PRO",
			Status:   "active",
		},
	}}
	tx := testsupport.NewTx(t)
	tx.EXPECT().Commit(gomock.Any()).Return(nil)
	handler := &SubscriptionHandler{
		DB:     testsupport.TxDB(t, tx),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Service: func(repository.Querier) billingSvc.SubscriptionService {
			return service
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/subscriptions/me", nil)
	req = req.WithContext(middleware.WithClaims(req.Context(), &tokens.VerisafeClaims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: userID.String()},
	}))
	res := httptest.NewRecorder()

	core.AppHandler(handler.GetMySubscriptionStatus).ServeHTTP(res, req)

	require.Equal(t, http.StatusOK, res.Code)
	assert.Equal(t, userID, service.userID)
	var body billingSvc.SubscriptionStatus
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &body))
	assert.True(t, body.Active)
	require.NotNil(t, body.Subscription)
	assert.Equal(t, "PRO", body.Subscription.PlanCode)
}

func TestSubscriptionHandler_GetMySubscriptionStatusRejectsMissingClaims(t *testing.T) {
	handler := &SubscriptionHandler{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	res := httptest.NewRecorder()

	core.AppHandler(handler.GetMySubscriptionStatus).ServeHTTP(
		res,
		httptest.NewRequest(http.MethodGet, "/subscriptions/me", nil),
	)

	assert.Equal(t, http.StatusUnauthorized, res.Code)
}
