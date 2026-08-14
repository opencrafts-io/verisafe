package billing

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jinzhu/copier"
	"github.com/opencrafts-io/verisafe/internal/repository"
)

var ErrNoEntitlementFound error = errors.New("No plan(s) retrieved")

type EntitlementService interface {
	ListEntitlementsByPlanCode(
		ctx context.Context,
		req ListEntitlementsByPlanCode,
	) ([]Entitlement, error)
	GetEntitlement(
		ctx context.Context,
		req GetEntitlement,
	) (*Entitlement, error)
	CreateEntitlement(
		ctx context.Context,
		req CreateEntitlement,
	) (*Entitlement, error)
	UpdateEntitlement(
		ctx context.Context,
		req UpdateEntitlement,
	) (*Entitlement, error)
	DeleteEntitlement(ctx context.Context, req DeleteEntitlement) error
	DeleteEntitlementsByPlanCode(ctx context.Context, planCode string) error
}

type entitlementService struct {
	querier repository.Querier
	logger  *slog.Logger
}

func NewEntitlementService(
	querier repository.Querier,
	logger *slog.Logger,
) EntitlementService {
	return &entitlementService{
		querier: querier,
		logger:  logger,
	}
}

func (es *entitlementService) ListEntitlementsByPlanCode(
	ctx context.Context,
	req ListEntitlementsByPlanCode,
) ([]Entitlement, error) {
	dbEntitlements, err := es.querier.ListEntitlementsByPlanCode(
		ctx,
		req.PlanCode,
	)
	if err != nil {
		es.logger.ErrorContext(
			ctx,
			"failed to list entitlements",
			slog.String("plan_code", req.PlanCode),
			slog.String("error", err.Error()),
		)
		return nil, fmt.Errorf("failed to list entitlements: %w", err)
	}

	var entitlements []Entitlement
	if err := copier.Copy(&entitlements, &dbEntitlements); err != nil {
		es.logger.ErrorContext(
			ctx,
			"failed to map entitlements to DTO",
			slog.String("plan_code", req.PlanCode),
			slog.String("error", err.Error()),
		)
		return nil, fmt.Errorf("failed to construct entitlement DTOs: %w", err)
	}

	es.logger.InfoContext(
		ctx,
		"entitlements listed successfully",
		slog.String("plan_code", req.PlanCode),
		slog.Int("count", len(entitlements)),
	)
	return entitlements, nil
}

func (es *entitlementService) GetEntitlement(
	ctx context.Context,
	req GetEntitlement,
) (*Entitlement, error) {
	dbEntitlement, err := es.querier.GetEntitlement(
		ctx,
		repository.GetEntitlementParams{
			Code: req.PlanCode,
			Key:  req.Key,
		},
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			es.logger.WarnContext(
				ctx,
				"entitlement not found",
				slog.String("plan_code", req.PlanCode),
				slog.String("key", req.Key),
			)
			return nil, ErrNoEntitlementFound
		}
		es.logger.ErrorContext(
			ctx,
			"failed to get entitlement",
			slog.String("plan_code", req.PlanCode),
			slog.String("key", req.Key),
			slog.String("error", err.Error()),
		)
		return nil, fmt.Errorf("failed to get entitlement: %w", err)
	}

	var entitlement Entitlement
	if err := copier.Copy(&entitlement, &dbEntitlement); err != nil {
		es.logger.ErrorContext(
			ctx,
			"failed to map entitlement to DTO",
			slog.String("plan_code", req.PlanCode),
			slog.String("key", req.Key),
			slog.String("error", err.Error()),
		)
		return nil, fmt.Errorf("failed to construct entitlement DTO: %w", err)
	}

	es.logger.InfoContext(
		ctx,
		"entitlement retrieved successfully",
		slog.String("plan_code", req.PlanCode),
		slog.String("key", req.Key),
	)
	return &entitlement, nil
}

func (es *entitlementService) CreateEntitlement(
	ctx context.Context,
	req CreateEntitlement,
) (*Entitlement, error) {
	dbEntitlement, err := es.querier.CreateEntitlement(
		ctx,
		repository.CreateEntitlementParams{
			PlanCode:    req.PlanCode,
			Key:         req.Key,
			Value:       req.Value,
			Unit:        repository.EntitlementUnit(req.Unit),
			Description: req.Description,
		},
	)
	if err != nil {
		es.logger.ErrorContext(
			ctx,
			"failed to create entitlement",
			slog.String("plan_code", req.PlanCode),
			slog.String("key", req.Key),
			slog.String("error", err.Error()),
		)
		return nil, fmt.Errorf("failed to create entitlement: %w", err)
	}

	var entitlement Entitlement
	if err := copier.Copy(&entitlement, &dbEntitlement); err != nil {
		es.logger.ErrorContext(
			ctx,
			"failed to map entitlement to DTO",
			slog.String("plan_code", req.PlanCode),
			slog.String("key", req.Key),
			slog.String("error", err.Error()),
		)
		return nil, fmt.Errorf("failed to construct entitlement DTO: %w", err)
	}

	es.logger.InfoContext(
		ctx,
		"entitlement created successfully",
		slog.String("plan_code", req.PlanCode),
		slog.String("key", req.Key),
	)
	return &entitlement, nil
}

func (es *entitlementService) UpdateEntitlement(
	ctx context.Context,
	req UpdateEntitlement,
) (*Entitlement, error) {
	dbEntitlement, err := es.querier.UpdateEntitlement(
		ctx,
		repository.UpdateEntitlementParams{
			PlanCode:    req.PlanCode,
			Key:         req.Key,
			Value:       req.Value,
			Unit:        repository.EntitlementUnit(req.Unit),
			Description: req.Description,
		},
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			es.logger.WarnContext(
				ctx,
				"entitlement not found for update",
				slog.String("plan_code", req.PlanCode),
				slog.String("key", req.Key),
			)
			return nil, ErrNoEntitlementFound
		}
		es.logger.ErrorContext(
			ctx,
			"failed to update entitlement",
			slog.String("plan_code", req.PlanCode),
			slog.String("key", req.Key),
			slog.String("error", err.Error()),
		)
		return nil, fmt.Errorf("failed to update entitlement: %w", err)
	}

	var entitlement Entitlement
	if err := copier.Copy(&entitlement, &dbEntitlement); err != nil {
		es.logger.ErrorContext(
			ctx,
			"failed to map entitlement to DTO",
			slog.String("plan_code", req.PlanCode),
			slog.String("key", req.Key),
			slog.String("error", err.Error()),
		)
		return nil, fmt.Errorf("failed to construct entitlement DTO: %w", err)
	}

	es.logger.InfoContext(
		ctx,
		"entitlement updated successfully",
		slog.String("plan_code", req.PlanCode),
		slog.String("key", req.Key),
	)
	return &entitlement, nil
}

func (es *entitlementService) DeleteEntitlement(
	ctx context.Context,
	req DeleteEntitlement,
) error {
	if err := es.querier.DeleteEntitlement(
		ctx,
		repository.DeleteEntitlementParams{
			Code: req.PlanCode,
			Key:  req.Key,
		},
	); err != nil {
		es.logger.ErrorContext(
			ctx,
			"failed to delete entitlement",
			slog.String("plan_code", req.PlanCode),
			slog.String("key", req.Key),
			slog.String("error", err.Error()),
		)
		return fmt.Errorf("failed to delete entitlement: %w", err)
	}

	es.logger.InfoContext(
		ctx,
		"entitlement deleted successfully",
		slog.String("plan_code", req.PlanCode),
		slog.String("key", req.Key),
	)
	return nil
}

func (es *entitlementService) DeleteEntitlementsByPlanCode(
	ctx context.Context,
	planCode string,
) error {
	if err := es.querier.DeleteEntitlementsByPlanCode(
		ctx,
		planCode,
	); err != nil {
		es.logger.ErrorContext(
			ctx,
			"failed to delete entitlements by plan code",
			slog.String("plan_code", planCode),
			slog.String("error", err.Error()),
		)
		return fmt.Errorf("failed to delete entitlements by plan code: %w", err)
	}

	es.logger.InfoContext(
		ctx,
		"entitlements deleted successfully",
		slog.String("plan_code", planCode),
	)
	return nil
}
