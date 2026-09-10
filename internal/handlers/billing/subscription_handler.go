package billing

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/opencrafts-io/verisafe/internal/config"
	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/middleware"
	"github.com/opencrafts-io/verisafe/internal/repository"
	billingSvc "github.com/opencrafts-io/verisafe/internal/service/billing"
)

type SubscriptionHandler struct {
	DB      core.IDBProvider
	Logger  *slog.Logger
	Cfg     *config.Config
	Cacher  core.Cacher
	Service func(repository.Querier) billingSvc.SubscriptionService
}

func (h *SubscriptionHandler) svc(
	db repository.DBTX,
) billingSvc.SubscriptionService {
	if h.Service != nil {
		return h.Service(repository.New(db))
	}
	return billingSvc.NewSubscriptionService(repository.New(db), h.Logger)
}

func (h *SubscriptionHandler) RegisterHandlers(router core.Router) {
	router.Handle(
		"GET /subscriptions/me",
		middleware.CreateStack(
			middleware.IsAuthenticated(h.Cfg, h.DB, h.Cacher, h.Logger),
		)(core.AppHandler(h.GetMySubscriptionStatus)),
	)
}

// GetMySubscriptionStatus godoc
//
// @Summary      Get the authenticated user's subscription status
// @Description  Returns whether the caller has a currently active subscription and its plan details when present.
// @Tags         subscriptions
// @Produce      json
// @Success      200 {object} billingSvc.SubscriptionStatus
// @Failure      401 {object} core.APIError "Missing or invalid claims"
// @Failure      500 {object} core.APIError "Failed to fetch subscription status"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /subscriptions/me [get]
func (h *SubscriptionHandler) GetMySubscriptionStatus(
	w http.ResponseWriter,
	r *http.Request,
) error {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		return core.Public(core.ErrUnauthorized, msgAuthRequired)
	}

	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return core.Public(core.ErrInternal, msgFetchAccountFailed)
	}

	status, err := core.InTx(
		r.Context(),
		h.DB,
		func(tx pgx.Tx) (*billingSvc.SubscriptionStatus, error) {
			return h.svc(tx).GetStatus(r.Context(), userID)
		},
	)
	if err != nil {
		if errors.Is(err, core.ErrUnauthorized) {
			return err
		}
		h.Logger.ErrorContext(
			r.Context(),
			"failed to fetch subscription status",
			slog.String("user_id", userID.String()),
			slog.Any("error", err),
		)
		return core.Public(core.ErrInternal, "Failed to fetch subscription status.")
	}

	core.WriteJSON(w, http.StatusOK, status)
	return nil
}
