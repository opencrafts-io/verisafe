package billing

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jinzhu/copier"

	"github.com/opencrafts-io/verisafe/internal/config"
	"github.com/opencrafts-io/verisafe/internal/repository"
)

var (
	ErrNoChargeAttemptFound error = errors.New(
		"no charge attempt(s) retrieved",
	)

	ErrFailedToConstructChargeAttemptResponse error = errors.New(
		"failed to copy charge attempt to dto",
	)
)

type ChargeAttemptService interface {
	GetChargeAttempt(
		ctx context.Context,
		id uuid.UUID,
	) (*ChargeAttempt, error)

	GetPendingChargeAttempts(
		ctx context.Context,
		params ListPendingChargeAttempts,
	) ([]ChargeAttempt, error)

	CreateChargeAttempt(
		ctx context.Context,
		params CreateChargeAttempt,
	) (*ChargeAttempt, error)

	ResolveChargeAttempt(
		ctx context.Context,
		params ResolveChargeAttempt,
	) (*ChargeAttempt, error)
}

type chargeAttemptService struct {
	Cfg     *config.Config
	Logger  *slog.Logger
	querier repository.Querier
}

func NewChargeAttemptService(
	cfg *config.Config,
	logger *slog.Logger,
	querier repository.Querier,
) *chargeAttemptService {
	return &chargeAttemptService{
		Cfg:     cfg,
		Logger:  logger,
		querier: querier,
	}
}

func (cas *chargeAttemptService) GetChargeAttempt(
	ctx context.Context,
	id uuid.UUID,
) (*ChargeAttempt, error) {
	rawAttempt, err := cas.querier.GetChargeAttempt(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			cas.Logger.WarnContext(
				ctx,
				"charge attempt not found",
				slog.String("id", id.String()),
			)
			return nil, ErrNoChargeAttemptFound
		}

		cas.Logger.ErrorContext(
			ctx,
			"failed to retrieve charge attempt",
			slog.String("id", id.String()),
			slog.String("error", err.Error()),
		)

		return nil, fmt.Errorf(
			"failed to retrieve charge attempt: %w",
			err,
		)
	}

	var attempt ChargeAttempt

	if err := copier.Copy(&attempt, &rawAttempt); err != nil {
		cas.Logger.ErrorContext(
			ctx,
			"failed to map charge attempt to dto",
			slog.String("id", id.String()),
			slog.String("error", err.Error()),
		)

		return nil, ErrFailedToConstructChargeAttemptResponse
	}

	return &attempt, nil
}

func (cas *chargeAttemptService) GetPendingChargeAttempts(
	ctx context.Context,
	params ListPendingChargeAttempts,
) ([]ChargeAttempt, error) {
	if params.OrderID == "" {
		return nil, fmt.Errorf("order id cannot be empty")
	}

	rawAttempts, err := cas.querier.GetPendingChargeAttemptsByOrder(
		ctx,
		repository.GetPendingChargeAttemptsByOrderParams{
			OrderID:    params.OrderID,
			PageSize:   params.Limit,
			PageOffset: params.Offset,
		},
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNoChargeAttemptFound
		}

		cas.Logger.ErrorContext(
			ctx,
			"failed to retrieve pending charge attempts",
			slog.String("order_id", params.OrderID),
			slog.String("error", err.Error()),
		)

		return nil, fmt.Errorf(
			"failed to retrieve pending charge attempts: %w",
			err,
		)
	}

	result := make([]ChargeAttempt, len(rawAttempts))

	for i, rawAttempt := range rawAttempts {
		if err := copier.Copy(&result[i], &rawAttempt); err != nil {
			cas.Logger.ErrorContext(
				ctx,
				"failed to map charge attempt to dto",
				slog.String("order_id", params.OrderID),
				slog.String("error", err.Error()),
				slog.Any("charge_attempt", rawAttempt),
			)

			return nil, ErrFailedToConstructChargeAttemptResponse
		}
	}

	return result, nil
}

func (cas *chargeAttemptService) CreateChargeAttempt(
	ctx context.Context,
	params CreateChargeAttempt,
) (*ChargeAttempt, error) {
	if params.OrderID == "" {
		return nil, fmt.Errorf("order id cannot be empty")
	}

	if params.PayerPhoneNumber == "" {
		return nil, fmt.Errorf("payer phone number cannot be empty")
	}

	if params.Amount <= 0 {
		return nil, fmt.Errorf("charge amount must be greater than zero")
	}

	id := params.ID

	if id == uuid.Nil {
		id = uuid.New()
	}

	rawAttempt, err := cas.querier.CreateChargeAttempt(
		ctx,
		repository.CreateChargeAttemptParams{
			ID:               id,
			OrderID:          params.OrderID,
			PayerPhoneNumber: params.PayerPhoneNumber,
			Amount:           params.Amount,
		},
	)
	if err != nil {
		cas.Logger.ErrorContext(
			ctx,
			"failed to create charge attempt",
			slog.String("id", id.String()),
			slog.String("order_id", params.OrderID),
			slog.String("error", err.Error()),
		)

		return nil, fmt.Errorf(
			"failed to create charge attempt: %w",
			err,
		)
	}

	var attempt ChargeAttempt

	if err := copier.Copy(&attempt, &rawAttempt); err != nil {
		cas.Logger.ErrorContext(
			ctx,
			"failed to map created charge attempt to dto",
			slog.String("id", id.String()),
			slog.String("error", err.Error()),
		)

		return nil, ErrFailedToConstructChargeAttemptResponse
	}

	cas.Logger.InfoContext(
		ctx,
		"charge attempt created successfully",
		slog.String("id", attempt.ID.String()),
		slog.String("order_id", attempt.OrderID),
	)

	return &attempt, nil
}

func (cas *chargeAttemptService) ResolveChargeAttempt(
	ctx context.Context,
	params ResolveChargeAttempt,
) (*ChargeAttempt, error) {
	if params.ID == uuid.Nil {
		return nil, fmt.Errorf("charge attempt id cannot be empty")
	}

	if params.Status != "success" && params.Status != "failure" {
		return nil, fmt.Errorf(
			"invalid charge attempt status: %s",
			params.Status,
		)
	}

	rawAttempt, err := cas.querier.ResolveChargeAttempt(
		ctx,
		repository.ResolveChargeAttemptParams{
			ID:     params.ID,
			Status: repository.ChargeAttemptStatus(params.Status),
			Notes:  params.Notes,
		},
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			cas.Logger.WarnContext(
				ctx,
				"charge attempt not found or already resolved",
				slog.String("id", params.ID.String()),
			)

			return nil, ErrNoChargeAttemptFound
		}

		cas.Logger.ErrorContext(
			ctx,
			"failed to resolve charge attempt",
			slog.String("id", params.ID.String()),
			slog.String("error", err.Error()),
		)

		return nil, fmt.Errorf(
			"failed to resolve charge attempt: %w",
			err,
		)
	}

	var attempt ChargeAttempt

	if err := copier.Copy(&attempt, &rawAttempt); err != nil {
		cas.Logger.ErrorContext(
			ctx,
			"failed to map resolved charge attempt to dto",
			slog.String("id", params.ID.String()),
			slog.String("error", err.Error()),
		)

		return nil, ErrFailedToConstructChargeAttemptResponse
	}

	cas.Logger.InfoContext(
		ctx,
		"charge attempt resolved successfully",
		slog.String("id", attempt.ID.String()),
		slog.String("status", attempt.Status),
	)

	return &attempt, nil
}
