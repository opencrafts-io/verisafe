package billing

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/opencrafts-io/verisafe/internal/repository"
)

type subscriptionQuerier struct {
	repository.Querier

	row repository.GetActiveSubscriptionByUserRow
	err error
}

func (q *subscriptionQuerier) GetActiveSubscriptionByUser(
	_ context.Context,
	_ uuid.UUID,
) (repository.GetActiveSubscriptionByUserRow, error) {
	return q.row, q.err
}

func TestSubscriptionService_GetStatusReturnsActiveSubscription(t *testing.T) {
	periodEnd := time.Now().Add(30 * 24 * time.Hour).Round(time.Second)
	userID := uuid.New()
	querier := &subscriptionQuerier{row: repository.GetActiveSubscriptionByUserRow{
		ID:                 7,
		PlanID:             3,
		PlanCode:           "PRO",
		PlanName:           "Professional",
		Status:             repository.SubscriptionStatusActive,
		StartedAt:          time.Now().Round(time.Second),
		CurrentPeriodStart: time.Now().Round(time.Second),
		CurrentPeriodEnd:   &periodEnd,
	}}
	service := NewSubscriptionService(
		querier,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)

	status, err := service.GetStatus(context.Background(), userID)

	require.NoError(t, err)
	require.NotNil(t, status.Subscription)
	assert.True(t, status.Active)
	assert.Equal(t, "PRO", status.Subscription.PlanCode)
	assert.Equal(t, periodEnd, *status.Subscription.CurrentPeriodEnd)
}

func TestSubscriptionService_GetStatusReturnsInactiveWhenNoSubscriptionExists(t *testing.T) {
	service := NewSubscriptionService(
		&subscriptionQuerier{err: pgx.ErrNoRows},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)

	status, err := service.GetStatus(context.Background(), uuid.New())

	require.NoError(t, err)
	assert.False(t, status.Active)
	assert.Nil(t, status.Subscription)
}

func TestSubscriptionService_GetStatusReturnsRepositoryErrors(t *testing.T) {
	repositoryErr := errors.New("database unavailable")
	service := NewSubscriptionService(
		&subscriptionQuerier{err: repositoryErr},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)

	_, err := service.GetStatus(context.Background(), uuid.New())

	assert.ErrorIs(t, err, repositoryErr)
}
