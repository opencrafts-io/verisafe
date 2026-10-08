package billing

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jinzhu/copier"

	"github.com/opencrafts-io/verisafe/internal/repository"
)

type SubscriptionService interface {
	GetStatus(ctx context.Context, userID uuid.UUID) (*SubscriptionStatus, error)
}

type subscriptionService struct {
	querier repository.Querier
	logger  *slog.Logger
}

func NewSubscriptionService(
	querier repository.Querier,
	logger *slog.Logger,
) SubscriptionService {
	return &subscriptionService{querier: querier, logger: logger}
}

func (s *subscriptionService) GetStatus(
	ctx context.Context,
	userID uuid.UUID,
) (*SubscriptionStatus, error) {
	row, err := s.querier.GetActiveSubscriptionByUser(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return &SubscriptionStatus{Active: false}, nil
	}
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"failed to retrieve active subscription",
			slog.String("user_id", userID.String()),
			slog.Any("error", err),
		)
		return nil, fmt.Errorf("retrieve active subscription: %w", err)
	}

	var subscription Subscription
	if err := copier.Copy(&subscription, &row); err != nil {
		s.logger.ErrorContext(
			ctx,
			"failed to map active subscription",
			slog.String("user_id", userID.String()),
			slog.Any("error", err),
		)
		return nil, fmt.Errorf("map active subscription: %w", err)
	}

	return &SubscriptionStatus{
		Active:       true,
		Subscription: &subscription,
	}, nil
}
