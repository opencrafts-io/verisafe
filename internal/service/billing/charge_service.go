package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jinzhu/copier"

	"github.com/opencrafts-io/verisafe/internal/config"
	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/eventbus"
	"github.com/opencrafts-io/verisafe/internal/repository"
)

var (
	ErrOrderAlreadyPaid    = errors.New("order has already been paid")
	ErrOrderCancelled      = errors.New("order has been cancelled")
	ErrOrderNotChargeable  = errors.New("order cannot be charged")
	ErrInvalidChargeAmount = errors.New(
		"order total must be greater than zero",
	)
	ErrInvalidPayerPhone    = errors.New("payer phone number is required")
	ErrPendingChargeAttempt = errors.New(
		"a charge attempt is already pending for this order",
	)
	ErrChargePublishFailed = errors.New("failed to publish charge request")
	ErrInvalidChargeResult = errors.New("invalid charge result")
)

type ChargeService interface {
	ChargeOrder(ctx context.Context, params ChargeOrder) (*ChargeAttempt, error)
	HandleChargeResult(ctx context.Context, event []byte) error
}

type chargeService struct {
	cfg      *config.Config
	logger   *slog.Logger
	querier  repository.Querier
	db       core.IDBProvider
	eventBus eventbus.EventBus
}

func NewChargeService(
	cfg *config.Config,
	logger *slog.Logger,
	querier repository.Querier,
	db core.IDBProvider,
	eventBus eventbus.EventBus,
) ChargeService {
	return &chargeService{
		cfg:      cfg,
		logger:   logger,
		querier:  querier,
		db:       db,
		eventBus: eventBus,
	}
}

func (s *chargeService) ChargeOrder(
	ctx context.Context,
	params ChargeOrder,
) (*ChargeAttempt, error) {
	if params.OrderID == "" {
		return nil, ErrOrderNotChargeable
	}

	order, err := s.querier.GetOrder(ctx, params.OrderID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrOrderNotFound
		}

		s.logger.ErrorContext(
			ctx, "failed to retrieve order for charge",
			slog.String("order_id", params.OrderID),
			slog.Any("error", err),
		)
		return nil, fmt.Errorf("retrieve order for charge: %w", err)
	}

	switch order.Status {
	case repository.OrderStatusPaid:
		return nil, ErrOrderAlreadyPaid
	case repository.OrderStatusCancelled:
		return nil, ErrOrderCancelled
	case repository.OrderStatusPending, repository.OrderStatusFailed:
	default:
		return nil, ErrOrderNotChargeable
	}
	if order.ExpiresAt != nil && !order.ExpiresAt.After(time.Now()) {
		return nil, ErrOrderNotChargeable
	}

	if order.Total <= 0 {
		return nil, ErrInvalidChargeAmount
	}

	payerPhoneNumber := strings.TrimSpace(params.PayerPhoneNumber)
	if len(payerPhoneNumber) < 5 {
		return nil, ErrInvalidPayerPhone
	}

	pending, err := s.pendingAttempts(ctx, order.ID)
	if err != nil {
		return nil, err
	}
	if len(pending) > 0 {
		return nil, ErrPendingChargeAttempt
	}

	attemptID := uuid.New()
	rawAttempt, err := s.querier.CreateChargeAttempt(
		ctx,
		repository.CreateChargeAttemptParams{
			ID:               attemptID,
			OrderID:          order.ID,
			PayerPhoneNumber: payerPhoneNumber,
			Amount:           order.Total,
		},
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrOrderNotChargeable
		}

		if isPendingAttemptConflict(err) {
			if _, lookupErr := s.pendingAttempts(
				ctx,
				order.ID,
			); lookupErr != nil {
				s.logger.ErrorContext(
					ctx,
					"failed to retrieve concurrent pending charge attempt",
					slog.String("order_id", order.ID),
					slog.Any("error", lookupErr),
				)
			}
			return nil, ErrPendingChargeAttempt
		}

		s.logger.ErrorContext(
			ctx, "failed to create charge attempt",
			slog.String("order_id", order.ID),
			slog.String("charge_attempt_id", attemptID.String()),
			slog.Any("error", err),
		)
		return nil, fmt.Errorf("create charge attempt: %w", err)
	}

	var attempt ChargeAttempt
	if err := copier.Copy(&attempt, &rawAttempt); err != nil {
		s.logger.ErrorContext(
			ctx, "failed to map charge attempt to dto",
			slog.String("charge_attempt_id", attemptID.String()),
			slog.Any("error", err),
		)
		return nil, ErrFailedToConstructChargeAttemptResponse
	}

	event := VeribrokeSTKRequest{
		RequestID:   attempt.ID.String(),
		PhoneNumber: attempt.PayerPhoneNumber,
		TransAmount: attempt.Amount,
		TransDesc:   fmt.Sprintf("Verisafe order %s", attempt.OrderID),
		ServiceName: s.cfg.BillingServiceName(),
		ReplyTo:     s.cfg.VeribrokeReplyRoutingKey(),
		Metadata: map[string]string{
			"order_id": attempt.OrderID,
		},
	}

	if err := s.eventBus.Publish(
		ctx,
		VeribrokeSTKRoutingKey,
		event,
	); err != nil {
		notes := err.Error()
		if _, resolveErr := s.querier.ResolveChargeAttempt(
			ctx,
			repository.ResolveChargeAttemptParams{
				ID:     attempt.ID,
				Status: repository.ChargeAttemptStatusFailure,
				Notes:  &notes,
			},
		); resolveErr != nil {
			s.logger.ErrorContext(
				ctx,
				"failed to record charge publish failure",
				slog.String("charge_attempt_id", attempt.ID.String()),
				slog.Any("publish_error", err),
				slog.Any("resolve_error", resolveErr),
			)
			return nil, fmt.Errorf(
				"record failed charge publish: %w",
				resolveErr,
			)
		}

		s.logger.ErrorContext(
			ctx, "failed to publish charge request",
			slog.String("charge_attempt_id", attempt.ID.String()),
			slog.Any("error", err),
		)
		return nil, fmt.Errorf("%w: %v", ErrChargePublishFailed, err)
	}

	return &attempt, nil
}

func (s *chargeService) HandleChargeResult(
	ctx context.Context,
	event []byte,
) error {
	var result VeribrokeChargeResult
	if err := json.Unmarshal(event, &result); err != nil {
		s.logger.WarnContext(
			ctx, "received malformed veribroke charge result",
			slog.Any("error", err),
		)
		return ErrInvalidChargeResult
	}

	attemptID, err := uuid.Parse(result.RequestID)
	if err != nil {
		s.logger.WarnContext(
			ctx,
			"received charge result with invalid request id",
			slog.String("request_id", result.RequestID),
			slog.Any("error", err),
		)
		return ErrInvalidChargeResult
	}

	if result.Status != string(repository.ChargeAttemptStatusSuccess) &&
		result.Status != string(repository.ChargeAttemptStatusFailure) {
		s.logger.WarnContext(
			ctx, "received charge result with invalid status",
			slog.String("charge_attempt_id", attemptID.String()),
			slog.String("status", result.Status),
		)
		return ErrInvalidChargeResult
	}

	attempt, err := s.querier.GetChargeAttempt(ctx, attemptID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			s.logger.WarnContext(
				ctx,
				"received charge result for unknown attempt",
				slog.String("charge_attempt_id", attemptID.String()),
			)
			return ErrNoChargeAttemptFound
		}

		s.logger.ErrorContext(
			ctx,
			"failed to retrieve charge attempt for result",
			slog.String("charge_attempt_id", attemptID.String()),
			slog.Any("error", err),
		)
		return fmt.Errorf("retrieve charge attempt for result: %w", err)
	}

	if attempt.Status != repository.ChargeAttemptStatusPending {
		s.logger.InfoContext(
			ctx, "ignoring duplicate charge result",
			slog.String("charge_attempt_id", attemptID.String()),
			slog.String("status", string(attempt.Status)),
		)
		return nil
	}

	notes := result.Message
	if notes == "" {
		notes = string(event)
	}

	if result.Status == string(repository.ChargeAttemptStatusFailure) {
		return s.resolveFailure(ctx, attemptID, notes)
	}

	return s.resolveSuccess(ctx, attemptID, notes)
}

func (s *chargeService) pendingAttempts(
	ctx context.Context,
	orderID string,
) ([]repository.ChargeAttempt, error) {
	attempts, err := s.querier.GetPendingChargeAttemptsByOrder(
		ctx,
		repository.GetPendingChargeAttemptsByOrderParams{
			OrderID:    orderID,
			PageSize:   1,
			PageOffset: 0,
		},
	)
	if err != nil {
		s.logger.ErrorContext(
			ctx, "failed to retrieve pending charge attempts",
			slog.String("order_id", orderID),
			slog.Any("error", err),
		)
		return nil, fmt.Errorf("retrieve pending charge attempts: %w", err)
	}

	return attempts, nil
}

func (s *chargeService) resolveFailure(
	ctx context.Context,
	id uuid.UUID,
	notes string,
) error {
	if _, err := s.querier.ResolveChargeAttempt(
		ctx,
		repository.ResolveChargeAttemptParams{
			ID:     id,
			Status: repository.ChargeAttemptStatusFailure,
			Notes:  &notes,
		},
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			s.logger.InfoContext(
				ctx, "ignoring duplicate charge result",
				slog.String("charge_attempt_id", id.String()),
			)
			return nil
		}

		s.logger.ErrorContext(
			ctx, "failed to resolve failed charge attempt",
			slog.String("charge_attempt_id", id.String()),
			slog.Any("error", err),
		)
		return fmt.Errorf("resolve failed charge attempt: %w", err)
	}

	return nil
}

func (s *chargeService) resolveSuccess(
	ctx context.Context,
	id uuid.UUID,
	notes string,
) error {
	if s.db == nil {
		return fmt.Errorf(
			"resolve successful charge attempt: database provider is required",
		)
	}

	applied := false
	_, err := core.InTx(ctx, s.db, func(tx pgx.Tx) (struct{}, error) {
		querier := repository.New(tx)
		attempt, err := querier.ResolveChargeAttempt(
			ctx,
			repository.ResolveChargeAttemptParams{
				ID:     id,
				Status: repository.ChargeAttemptStatusSuccess,
				Notes:  &notes,
			},
		)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return struct{}{}, nil
			}
			return struct{}{}, fmt.Errorf(
				"resolve successful charge attempt: %w",
				err,
			)
		}

		if _, err := querier.MarkOrderPaid(ctx, attempt.OrderID); err != nil {
			return struct{}{}, fmt.Errorf(
				"mark order paid after charge success: %w",
				err,
			)
		}

		applied = true
		return struct{}{}, nil
	})
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"failed to resolve successful charge attempt",
			slog.String("charge_attempt_id", id.String()),
			slog.Any("error", err),
		)
		return err
	}

	if !applied {
		s.logger.InfoContext(
			ctx, "ignoring duplicate charge result",
			slog.String("charge_attempt_id", id.String()),
		)
	}

	return nil
}

func isPendingAttemptConflict(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
