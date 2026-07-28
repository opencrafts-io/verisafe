package social

import (
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/opencrafts-io/verisafe/internal/config"
	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/middleware"
	"github.com/opencrafts-io/verisafe/internal/repository"
	socialsvc "github.com/opencrafts-io/verisafe/internal/service/social"
)

type SocialHandler struct {
	Cacher core.Cacher
	DB     core.IDBProvider
	Cfg    *config.Config
	Logger *slog.Logger

	// Service builds a social service bound to the caller's transaction. Left
	// nil it falls back to the real implementation; see the role handler for
	// why this field is the testing seam.
	Service func(repository.Querier) socialsvc.Service
}

func (sh *SocialHandler) svc(tx pgx.Tx) socialsvc.Service {
	if sh.Service != nil {
		return sh.Service(repository.New(tx))
	}
	return socialsvc.NewService(repository.New(tx))
}

func (sh *SocialHandler) RegisterHandlers(router core.Router) {
	router.Handle("GET /socials/me",
		middleware.CreateStack(
			middleware.IsAuthenticated(sh.Cfg, sh.DB, sh.Cacher, sh.Logger),
			middleware.HasPermission([]string{"read:account:own"}),
		)(core.AppHandler(sh.GetAllUserSocials)),
	)
	router.Handle("GET /socials/user/{user_id}",
		middleware.CreateStack(
			middleware.IsAuthenticated(sh.Cfg, sh.DB, sh.Cacher, sh.Logger),
			middleware.HasPermission([]string{"read:account:any"}),
		)(core.AppHandler(sh.GetUserIDSocials)),
	)
}

// GetUserIDSocials godoc
//
// @Summary      List a given user's linked social accounts
// @Tags         socials
// @Produce      json
// @Description  Provider access and refresh tokens are never included; those are brokered through /oauth/{provider}/token instead.
// @Param        user_id  path  string  true  "Account ID"
// @Success      200  {array}   social.socialResponse
// @Failure      400  {object}  core.APIError  "Invalid user_id"
// @Failure      500  {object}  core.APIError  "Failed to fetch social connections"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /socials/user/{user_id} [get]
func (sh *SocialHandler) GetUserIDSocials(
	w http.ResponseWriter,
	r *http.Request,
) error {
	id, err := uuid.Parse(r.PathValue("user_id"))
	if err != nil {
		sh.Logger.Error("Failed to parse request body", slog.Any("error", err))
		return core.Public(core.ErrInvalidInput, msgCheckBody)
	}

	socials, err := core.InTx(
		r.Context(),
		sh.DB,
		func(tx pgx.Tx) ([]repository.Social, error) {
			socials, err := sh.svc(tx).ListForAccount(r.Context(), id)
			if err != nil {
				sh.Logger.Error(
					"Error while processing request", slog.Any("error", err),
				)
				return nil, core.Public(core.ErrInternal, msgFetchFailed)
			}
			return socials, nil
		},
	)
	if err != nil {
		return core.Fallback(err, core.ErrInternal, msgGeneric)
	}

	core.WriteJSON(w, http.StatusOK, sanitizeSocials(socials))
	return nil
}

// GetAllUserSocials godoc
//
// @Summary      List the authenticated user's own linked social accounts
// @Description  Provider access and refresh tokens are never included; those are brokered through /oauth/{provider}/token instead.
// @Tags         socials
// @Produce      json
// @Success      200  {array}   social.socialResponse
// @Failure      401  {object}  core.APIError  "Missing or invalid claims"
// @Failure      500  {object}  core.APIError  "Failed to fetch social connections"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /socials/me [get]
func (sh *SocialHandler) GetAllUserSocials(
	w http.ResponseWriter,
	r *http.Request,
) error {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		return core.Public(core.ErrUnauthorized, msgAuthRequired)
	}

	id, err := uuid.Parse(claims.Subject)
	if err != nil {
		sh.Logger.Error("Error while processing request", slog.Any("error", err))
		// The subject failing to parse is a 500 here, not a 400, matching
		// this endpoint's behaviour before the extraction: a malformed
		// subject in an otherwise-valid token is treated as our failure, not
		// the caller's bad input.
		return core.Public(core.ErrInternal, msgCheckToken)
	}

	socials, err := core.InTx(
		r.Context(),
		sh.DB,
		func(tx pgx.Tx) ([]repository.Social, error) {
			socials, err := sh.svc(tx).ListForAccount(r.Context(), id)
			if err != nil {
				sh.Logger.Error(
					"Error while processing request", slog.Any("error", err),
				)
				return nil, core.Public(core.ErrInternal, msgFetchFailed)
			}
			return socials, nil
		},
	)
	if err != nil {
		return core.Fallback(err, core.ErrInternal, msgGeneric)
	}

	core.WriteJSON(w, http.StatusOK, sanitizeSocials(socials))
	return nil
}
