package billing

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jinzhu/copier"
	"github.com/opencrafts-io/verisafe/internal/config"
	"github.com/opencrafts-io/verisafe/internal/repository"
)

var (
	ErrNoPlanFound               error = errors.New("No plan(s) retrieved")
	ErrFailedToConstructResponse error = errors.New(
		"Failed to copy plan to dto",
	)
)

type PlanService interface {
	ListPlans(ctx context.Context, params ListPlans) ([]Plan, error)
	GetPlanByCode(ctx context.Context, code string) (*Plan, error)
	CreatePlan(ctx context.Context, params CreatePlan) (*Plan, error)
	UpdatePlan(ctx context.Context, params UpdatePlan) (*Plan, error)
}

type planService struct {
	Cfg     *config.Config
	Logger  *slog.Logger
	querier repository.Querier
}

func (p *planService) New(
	cfg *config.Config,
	logger *slog.Logger,
	querier repository.Querier,
) *planService {
	return &planService{
		Cfg:     cfg,
		Logger:  logger,
		querier: querier,
	}
}

// ListPlans retrieves a list of plans from the database filtered by visibility status.
// It queries all plans based on the visibility parameter and maps the database models
// to the Plan DTO for the response. Returns an error if the query fails or no plans are found.
func (ps *planService) ListPlans(
	ctx context.Context,
	params ListPlans,
) ([]Plan, error) {
	plans, err := ps.querier.ListPlans(ctx, params.Visible)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNoPlanFound
		}
		ps.Logger.ErrorContext(
			ctx,
			"failed to retrieve plans",
			slog.String("error", err.Error()),
		)
		return nil, fmt.Errorf("failed to retrieve plans: %w", err)
	}

	result := make([]Plan, len(plans))

	for i, plan := range plans {
		if err := copier.Copy(&result[i], &plan); err != nil {
			ps.Logger.ErrorContext(
				ctx,
				"failed to map plan to dto",
				slog.String("error", err.Error()),
				slog.Any("plan", plan),
			)
			return nil, ErrFailedToConstructResponse
		}
	}

	return result, nil
}

// GetPlanByCode retrieves a single plan from the database by its unique code identifier.
// It maps the database model to the Plan DTO for the response. Returns an error if the
// plan is not found or if the mapping operation fails.
func (ps *planService) GetPlanByCode(
	ctx context.Context,
	code string,
) (*Plan, error) {
	rawPlan, err := ps.querier.GetPlanByCode(ctx, code)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			ps.Logger.WarnContext(
				ctx,
				"plan not found by code",
				slog.String("code", code),
			)
			return nil, ErrNoPlanFound
		}
		ps.Logger.ErrorContext(
			ctx,
			"failed to retrieve plan by code",
			slog.String("code", code),
			slog.String("error", err.Error()),
		)
		return nil, fmt.Errorf("failed to retrieve plan by code: %w", err)
	}

	var plan Plan
	if err := copier.Copy(&plan, &rawPlan); err != nil {
		ps.Logger.ErrorContext(
			ctx,
			"failed to map plan to dto",
			slog.String("code", code),
			slog.String("error", err.Error()),
		)
		return nil, ErrFailedToConstructResponse
	}
	return &plan, nil
}

// CreatePlan creates a new plan in the database with the provided parameters.
// It validates the input, persists the plan to the database, and maps the database model
// to the Plan DTO for the response. Returns the created plan or an error if creation fails.
func (ps *planService) CreatePlan(
	ctx context.Context,
	params CreatePlan,
) (*Plan, error) {
	rawPlan, err := ps.querier.CreatePlan(ctx, repository.CreatePlanParams{
		Code:            params.Code,
		Currency:        params.Currency,
		Price:           params.Price,
		BillingInterval: params.BillingInterval,
		Active:          &params.Active,
		Visible:         &params.Visible,
		CreatedBy:       &params.CreatedBy,
		Description:     params.Description,
	})
	if err != nil {
		ps.Logger.ErrorContext(
			ctx,
			"failed to create plan",
			slog.String("code", params.Code),
			slog.String("error", err.Error()),
		)
		return nil, fmt.Errorf("failed to create plan: %w", err)
	}

	var plan Plan
	if err := copier.Copy(&plan, &rawPlan); err != nil {
		ps.Logger.ErrorContext(
			ctx,
			"failed to map created plan to dto",
			slog.String("code", params.Code),
			slog.String("error", err.Error()),
		)
		return nil, ErrFailedToConstructResponse
	}

	ps.Logger.InfoContext(
		ctx,
		"plan created successfully",
		slog.String("code", plan.Code),
	)

	return &plan, nil
}

// UpdatePlan updates an existing plan in the database with the provided parameters.
// It validates the input, persists the changes to the database, and maps the updated
// database model to the Plan DTO for the response. Returns the updated plan or an error if update fails.
func (ps *planService) UpdatePlan(
	ctx context.Context,
	params UpdatePlan,
) (*Plan, error) {
	rawPlan, err := ps.querier.UpdatePlan(ctx, repository.UpdatePlanParams{
		Code:            params.Code,
		Currency:        params.Currency,
		Price:           params.Price,
		BillingInterval: params.BillingInterval,
		Active:          params.Active,
		Visible:         params.Visible,
		Description:     params.Description,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			ps.Logger.WarnContext(
				ctx,
				"plan not found for update",
				slog.String("code", params.Code),
			)
			return nil, ErrNoPlanFound
		}
		ps.Logger.ErrorContext(
			ctx,
			"failed to update plan",
			slog.String("code", params.Code),
			slog.String("error", err.Error()),
		)
		return nil, fmt.Errorf("failed to update plan: %w", err)
	}

	var plan Plan
	if err := copier.Copy(&plan, &rawPlan); err != nil {
		ps.Logger.ErrorContext(
			ctx,
			"failed to map updated plan to dto",
			slog.String("code", params.Code),
			slog.String("error", err.Error()),
		)
		return nil, ErrFailedToConstructResponse
	}

	ps.Logger.InfoContext(
		ctx,
		"plan updated successfully",
		slog.String("code", plan.Code),
	)

	return &plan, nil
}
