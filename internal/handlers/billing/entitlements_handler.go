package billing

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/opencrafts-io/verisafe/internal/config"
	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/middleware"
	"github.com/opencrafts-io/verisafe/internal/repository"

	billingSvc "github.com/opencrafts-io/verisafe/internal/service/billing"
)

type EntitlementHandler struct {
	DB      core.IDBProvider
	Logger  *slog.Logger
	Cfg     *config.Config
	Cacher  core.Cacher
	Service func(repository.Querier) billingSvc.EntitlementService
}

const msgEntitlementNotFound = "Requested entitlements not found."

func (eh *EntitlementHandler) svc(
	db repository.DBTX,
) billingSvc.EntitlementService {
	if eh.Service != nil {
		return eh.Service(
			repository.New(db),
		)
	}
	return billingSvc.NewEntitlementService(
		repository.New(db),
		eh.Logger,
	)
}

func (eh *EntitlementHandler) RegisterHandlers(router core.Router) {
	router.Handle(
		"POST /entitlements",
		middleware.CreateStack(
			middleware.IsAuthenticated(eh.Cfg, eh.DB, eh.Cacher, eh.Logger),
			middleware.HasPermission([]string{"create:plan:any"}),
		)(core.AppHandler(eh.CreateEntitlement)),
	)
	router.Handle(
		"GET /entitlements/{plan_code}",
		middleware.CreateStack(
			middleware.IsAuthenticated(eh.Cfg, eh.DB, eh.Cacher, eh.Logger),
			middleware.HasPermission([]string{"read:plan:any"}),
		)(core.AppHandler(eh.ListEntitlementsForPlan)),
	)
}

// ListEntitlementsForPlan godoc
//
// @Summary      List entitlements for a plan
// @Description  Returns entitlements for a plans
// @Tags         entitlements
// @Produce      json
// @Param        plan_code  path      string             true  "Plan code"
// @Success      200      {array}   billing.Entitlement
// @Failure      401      {object}  core.APIError  "Missing or invalid claims"
// @Failure      500      {object}  core.APIError  "Failed to fetch entitlements"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /entitlements [get]
func (eh *EntitlementHandler) ListEntitlementsForPlan(
	w http.ResponseWriter,
	r *http.Request,
) error {
	planCode := r.PathValue("plan_code")

	entitlements, err := core.InTx(
		r.Context(),
		eh.DB,
		func(tx pgx.Tx) ([]billingSvc.Entitlement, error) {
			entitlements, err := eh.svc(tx).
				ListEntitlementsByPlanCode(r.Context(), billingSvc.ListEntitlementsByPlanCode{
					PlanCode: planCode,
				})
			if err != nil {
				if errors.Is(err, billingSvc.ErrNoEntitlementFound) {
					eh.Logger.Error(
						"Error occurred while listing entitlements for plan",
						slog.Any("error", err),
						slog.Any("plan_code", planCode),
					)
					return nil, core.Public(
						core.ErrNotFound,
						msgEntitlementNotFound,
					)

				}

				eh.Logger.Error(
					"Error while fetching plan",
					slog.Any("error", err),
				)
				return nil, core.Public(core.ErrInternal, msgFetchPlanFailed)
			}
			return entitlements, nil
		},
	)
	if err != nil {
		return core.Fallback(err, core.ErrInternal, msgGeneric)
	}

	core.WriteJSON(w, http.StatusOK, entitlements)
	return nil
}

// CreateEntitlement godoc
//
// @Summary      Create an entitlement for a plan
// @Description  Creates a new entitlement for a plan
// @Tags         entitlements
// @Accept       json
// @Produce      json
// @Param        entitlement  body      billing.CreateEntitlement  true  "Entitlement to create"
// @Success      201   {object}  billing.Entitlement
// @Failure      400   {object}  core.APIError  "Invalid request body"
// @Failure      401   {object}  core.APIError  "Missing or invalid claims"
// @Failure      409   {object}  core.APIError  "Plan with this code already exists"
// @Failure      500   {object}  core.APIError  "Failed to create entitlement"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /entitlements [post]
func (eh *EntitlementHandler) CreateEntitlement(
	w http.ResponseWriter,
	r *http.Request,
) error {
	var req billingSvc.CreateEntitlement
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		eh.Logger.Error(
			"Error while decoding request body",
			slog.Any("error", err),
		)
		return core.Public(core.ErrInvalidInput, msgInvalidBody)
	}

	entitlement, err := core.InTx(
		r.Context(),
		eh.DB,
		func(tx pgx.Tx) (*billingSvc.Entitlement, error) {
			entitlement, err := eh.svc(tx).CreateEntitlement(r.Context(), req)
			if err != nil {
				eh.Logger.Error(
					"Error while creating plan",
					slog.Any("error", err),
				)
				return nil, core.Public(core.ErrInternal, msgCreatePlanFailed)
			}
			return entitlement, nil
		},
	)
	if err != nil {
		return core.Fallback(err, core.ErrInternal, msgGeneric)
	}
	core.WriteJSON(w, http.StatusCreated, entitlement)

	return err
}
