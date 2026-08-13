package billing

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/opencrafts-io/verisafe/internal/config"
	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/middleware"
	"github.com/opencrafts-io/verisafe/internal/repository"
	billingSvc "github.com/opencrafts-io/verisafe/internal/service/billing"
)

type PlanHandler struct {
	DB      core.IDBProvider
	Logger  *slog.Logger
	Cfg     *config.Config
	Cacher  core.Cacher
	Service func(repository.Querier) billingSvc.PlanService
}

const (
	msgAuthRequired       = "Authentication required."
	msgFetchAccountFailed = "Failed to fetch account."
	msgInvalidBody        = "Invalid request body."
	msgCreatePlanFailed   = "Failed to create plan."
	msgGeneric            = "We couldn't satisfy your request at the moment try again later"
	msgPlanNotFound       = "Plan not found."
	msgFetchPlanFailed    = "Failed to fetch plan."
	msgUpdatePlanFailed   = "Failed to update plan."
	msgInvalidQueryParam  = "Invalid query parameter."
)

func (ph *PlanHandler) svc(db repository.DBTX) billingSvc.PlanService {
	if ph.Service != nil {
		return ph.Service(
			repository.New(db),
		)
	}
	return billingSvc.New(
		ph.Cfg, ph.Logger,
		repository.New(db),
	)
}

func (ph *PlanHandler) RegisterHandlers(router core.Router) {
	router.Handle(
		"POST /plans",
		middleware.CreateStack(
			middleware.IsAuthenticated(ph.Cfg, ph.DB, ph.Cacher, ph.Logger),
			middleware.HasPermission([]string{"create:plan:any"}),
		)(core.AppHandler(ph.CreatePlan)),
	)

	router.Handle(
		"GET /plans",
		middleware.CreateStack(
			middleware.IsAuthenticated(ph.Cfg, ph.DB, ph.Cacher, ph.Logger),
			middleware.HasPermission([]string{"read:plan:any"}),
		)(core.AppHandler(ph.ListPlans)),
	)

	router.Handle(
		"GET /plans/{code}",
		middleware.CreateStack(
			middleware.IsAuthenticated(ph.Cfg, ph.DB, ph.Cacher, ph.Logger),
			middleware.HasPermission([]string{"read:plan:any"}),
		)(core.AppHandler(ph.GetPlanByCode)),
	)

	router.Handle(
		"PATCH /plans/{code}",
		middleware.CreateStack(
			middleware.IsAuthenticated(ph.Cfg, ph.DB, ph.Cacher, ph.Logger),
			middleware.HasPermission([]string{"update:plan:any"}),
		)(core.AppHandler(ph.UpdatePlan)),
	)
}

// CreatePlan godoc
//
// @Summary      Create a billing plan
// @Description  Creates a new billing plan. The authenticated caller is recorded as the plan's creator.
// @Tags         plans
// @Accept       json
// @Produce      json
// @Param        plan  body      billing.CreatePlan  true  "Plan to create"
// @Success      201   {object}  billing.Plan
// @Failure      400   {object}  core.APIError  "Invalid request body"
// @Failure      401   {object}  core.APIError  "Missing or invalid claims"
// @Failure      409   {object}  core.APIError  "Plan with this code already exists"
// @Failure      500   {object}  core.APIError  "Failed to create plan"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /plans [post]
func (ph *PlanHandler) CreatePlan(
	w http.ResponseWriter,
	r *http.Request,
) error {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		return core.Public(core.ErrUnauthorized, msgAuthRequired)
	}

	createdBy, err := uuid.Parse(claims.Subject)
	if err != nil {
		ph.Logger.Error(
			"Error while parsing user id",
			slog.Any("error", err),
		)
		return core.Public(core.ErrInternal, msgFetchAccountFailed)
	}

	var req billingSvc.CreatePlan
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		ph.Logger.Error(
			"Error while decoding request body",
			slog.Any("error", err),
		)
		return core.Public(core.ErrInvalidInput, msgInvalidBody)
	}
	req.CreatedBy = createdBy

	plan, err := core.InTx(
		r.Context(),
		ph.DB,
		func(tx pgx.Tx) (*billingSvc.Plan, error) {
			plan, err := ph.svc(tx).CreatePlan(r.Context(), req)
			if err != nil {
				ph.Logger.Error(
					"Error while creating plan",
					slog.Any("error", err),
				)
				return nil, core.Public(core.ErrInternal, msgCreatePlanFailed)
			}
			return plan, nil
		},
	)
	if err != nil {
		return core.Fallback(err, core.ErrInternal, msgGeneric)
	}

	core.WriteJSON(w, http.StatusCreated, plan)
	return nil
}

// GetPlanByCode godoc
//
// @Summary      Get a billing plan by code
// @Description  Returns a single billing plan matching the given plan code.
// @Tags         plans
// @Produce      json
// @Param        code  path      string  true  "Plan code"
// @Success      200   {object}  billing.Plan
// @Failure      401   {object}  core.APIError  "Missing or invalid claims"
// @Failure      404   {object}  core.APIError  "Plan not found"
// @Failure      500   {object}  core.APIError  "Failed to fetch plan"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /plans/{code} [get]
func (ph *PlanHandler) GetPlanByCode(
	w http.ResponseWriter,
	r *http.Request,
) error {
	_, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		return core.Public(core.ErrUnauthorized, msgAuthRequired)
	}

	code := chi.URLParam(r, "code")

	plan, err := core.InTx(
		r.Context(),
		ph.DB,
		func(tx pgx.Tx) (*billingSvc.Plan, error) {
			plan, err := ph.svc(tx).GetPlanByCode(r.Context(), code)
			if errors.Is(err, billingSvc.ErrNoPlanFound) {
				ph.Logger.Error(
					"Error while fetching plan",
					slog.Any("error", err),
				)
				return nil, core.Public(core.ErrNotFound, msgPlanNotFound)
			}
			if err != nil {
				ph.Logger.Error(
					"Error while fetching plan",
					slog.Any("error", err),
				)
				return nil, core.Public(core.ErrInternal, msgFetchPlanFailed)
			}
			return plan, nil
		},
	)
	if err != nil {
		return core.Fallback(err, core.ErrInternal, msgGeneric)
	}

	core.WriteJSON(w, http.StatusOK, plan)
	return nil
}

// ListPlans godoc
//
// @Summary      List billing plans
// @Description  Returns billing plans, optionally filtered by visibility.
// @Tags         plans
// @Produce      json
// @Param        visible  query     bool  false  "Filter by visibility"
// @Success      200      {array}   billing.Plan
// @Failure      401      {object}  core.APIError  "Missing or invalid claims"
// @Failure      500      {object}  core.APIError  "Failed to fetch plans"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /plans [get]
func (ph *PlanHandler) ListPlans(
	w http.ResponseWriter,
	r *http.Request,
) error {
	_, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		return core.Public(core.ErrUnauthorized, msgAuthRequired)
	}

	var visible *bool
	if raw := r.URL.Query().Get("visible"); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			return core.Public(core.ErrInvalidInput, msgInvalidQueryParam)
		}
		visible = &parsed
	}

	plans, err := core.InTx(
		r.Context(),
		ph.DB,
		func(tx pgx.Tx) ([]billingSvc.Plan, error) {
			plans, err := ph.svc(tx).
				ListPlans(r.Context(), billingSvc.ListPlans{
					Visible: visible,
				})
			if errors.Is(err, billingSvc.ErrNoPlanFound) {
				return []billingSvc.Plan{}, nil
			}
			if err != nil {
				ph.Logger.Error(
					"Error while listing plans",
					slog.Any("error", err),
				)
				return nil, core.Public(core.ErrInternal, msgFetchPlanFailed)
			}
			return plans, nil
		},
	)
	if err != nil {
		return core.Fallback(err, core.ErrInternal, msgGeneric)
	}

	core.WriteJSON(w, http.StatusOK, plans)
	return nil
}

// UpdatePlan godoc
//
// @Summary      Update a billing plan
// @Description  Updates fields on an existing billing plan identified by its code. Only provided fields are updated.
// @Tags         plans
// @Accept       json
// @Produce      json
// @Param        code  path      string             true  "Plan code"
// @Param        plan  body      billing.UpdatePlan  true  "Fields to update"
// @Success      200   {object}  billing.Plan
// @Failure      400   {object}  core.APIError  "Invalid request body"
// @Failure      401   {object}  core.APIError  "Missing or invalid claims"
// @Failure      404   {object}  core.APIError  "Plan not found"
// @Failure      500   {object}  core.APIError  "Failed to update plan"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /plans/{code} [patch]
func (ph *PlanHandler) UpdatePlan(
	w http.ResponseWriter,
	r *http.Request,
) error {
	_, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		return core.Public(core.ErrUnauthorized, msgAuthRequired)
	}

	code := r.PathValue("code")

	var req billingSvc.UpdatePlan
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		ph.Logger.Error(
			"Error while decoding request body",
			slog.Any("error", err),
		)
		return core.Public(core.ErrInvalidInput, msgInvalidBody)
	}
	req.Code = code

	plan, err := core.InTx(
		r.Context(),
		ph.DB,
		func(tx pgx.Tx) (*billingSvc.Plan, error) {
			plan, err := ph.svc(tx).UpdatePlan(r.Context(), req)
			if errors.Is(err, billingSvc.ErrNoPlanFound) {
				ph.Logger.Error(
					"Error while updating plan",
					slog.Any("error", err),
				)
				return nil, core.Public(core.ErrNotFound, msgPlanNotFound)
			}
			if err != nil {
				ph.Logger.Error(
					"Error while updating plan",
					slog.Any("error", err),
				)
				return nil, core.Public(core.ErrInternal, msgUpdatePlanFailed)
			}
			return plan, nil
		},
	)
	if err != nil {
		return core.Fallback(err, core.ErrInternal, msgGeneric)
	}

	core.WriteJSON(w, http.StatusOK, plan)
	return nil
}
