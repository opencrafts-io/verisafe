package servicetoken

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/opencrafts-io/verisafe/internal/config"
	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/middleware"
	"github.com/opencrafts-io/verisafe/internal/repository"
	servicetokensvc "github.com/opencrafts-io/verisafe/internal/service/servicetoken"
	"github.com/opencrafts-io/verisafe/internal/tokens"
)

type ServiceTokenHandler struct {
	Cacher core.Cacher
	DB     core.IDBProvider
	Cfg    *config.Config
	Logger *slog.Logger

	// Service builds a service-token service bound to the caller's connection
	// or transaction. Left nil it falls back to the real implementation; see
	// the role handler for why this field is the testing seam, and the
	// institution handler for why the parameter is repository.DBTX rather
	// than pgx.Tx -- several read methods here query the acquired connection
	// directly with no transaction at all.
	Service func(repository.Querier) servicetokensvc.Service
}

func (sth *ServiceTokenHandler) svc(
	db repository.DBTX,
) servicetokensvc.Service {
	if sth.Service != nil {
		return sth.Service(repository.New(db))
	}
	return servicetokensvc.NewService(repository.New(db))
}

// RotationPolicy defines token rotation behavior. It lives here rather than
// with the account handler that also accepts it, because rotation is a
// service-token concept; the bot-account endpoint embeds a service-token block
// and borrows this type for that field.
type RotationPolicy struct {
	AutoRotate           bool `json:"auto_rotate"`
	RotationIntervalDays int  `json:"rotation_interval_days" validate:"omitempty,min=1,max=365"`
	NotifyBeforeDays     int  `json:"notify_before_days"     validate:"omitempty,min=1,max=30"`
}

// ServiceTokenRequest represents the request to create a service token
type ServiceTokenRequest struct {
	Name             string                 `json:"name"               validate:"required,min=1,max=100"`
	Description      *string                `json:"description"`
	ExpiresInDays    *int                   `json:"expires_in_days"    validate:"omitempty,min=1,max=3650"` // Max 10 years
	Scopes           []string               `json:"scopes"`
	MaxUses          *int                   `json:"max_uses"           validate:"omitempty,min=1"`
	RotationPolicy   *RotationPolicy        `json:"rotation_policy"`
	IPWhitelist      []string               `json:"ip_whitelist"`
	UserAgentPattern *string                `json:"user_agent_pattern"`
	Metadata         map[string]interface{} `json:"metadata"`
}

// ServiceTokenResponse represents the response for service token operations
type ServiceTokenResponse struct {
	ID          uuid.UUID              `json:"id"`
	Name        string                 `json:"name"`
	Description *string                `json:"description"`
	Token       string                 `json:"token,omitempty"` // Only included on creation
	ExpiresAt   *time.Time             `json:"expires_at"`
	Scopes      []string               `json:"scopes"`
	MaxUses     *int                   `json:"max_uses"`
	UseCount    int                    `json:"use_count"`
	CreatedAt   time.Time              `json:"created_at"`
	LastUsedAt  *time.Time             `json:"last_used_at"`
	RotatedAt   *time.Time             `json:"rotated_at"`
	RevokedAt   *time.Time             `json:"revoked_at"`
	Metadata    map[string]interface{} `json:"metadata"`
}

// ServiceTokenUpdateRequest represents the request to update a service token
type ServiceTokenUpdateRequest struct {
	Name             *string                `json:"name"               validate:"omitempty,min=1,max=100"`
	Description      *string                `json:"description"`
	Scopes           []string               `json:"scopes"`
	MaxUses          *int                   `json:"max_uses"           validate:"omitempty,min=1"`
	RotationPolicy   *RotationPolicy        `json:"rotation_policy"`
	IPWhitelist      []string               `json:"ip_whitelist"`
	UserAgentPattern *string                `json:"user_agent_pattern"`
	Metadata         map[string]interface{} `json:"metadata"`
}

// ServiceTokenStats represents usage statistics for service tokens
type ServiceTokenStats struct {
	TotalTokens        int `json:"total_tokens"`
	ActiveTokens       int `json:"active_tokens"`
	RevokedTokens      int `json:"revoked_tokens"`
	ExpiredTokens      int `json:"expired_tokens"`
	RecentlyUsedTokens int `json:"recently_used_tokens"`
}

func (sth *ServiceTokenHandler) RegisterHandlers(router core.Router) {
	// Service token management routes
	router.Handle("POST /api/v1/service-tokens",
		middleware.CreateStack(
			middleware.IsAuthenticated(sth.Cfg, sth.DB, sth.Cacher, sth.Logger),
			middleware.HasPermission([]string{"create:service_token:own"}),
		)(core.AppHandler(sth.CreateServiceToken)))

	router.Handle("GET /api/v1/service-tokens",
		middleware.CreateStack(
			middleware.IsAuthenticated(sth.Cfg, sth.DB, sth.Cacher, sth.Logger),
			middleware.HasPermission([]string{"list:service_token:own"}),
		)(core.AppHandler(sth.ListServiceTokens)))

	router.Handle("GET /api/v1/service-tokens/{id}",
		middleware.CreateStack(
			middleware.IsAuthenticated(sth.Cfg, sth.DB, sth.Cacher, sth.Logger),
			middleware.HasPermission([]string{"read:service_token:own"}),
		)(core.AppHandler(sth.GetServiceToken)))

	router.Handle("PUT /api/v1/service-tokens/{id}",
		middleware.CreateStack(
			middleware.IsAuthenticated(sth.Cfg, sth.DB, sth.Cacher, sth.Logger),
			middleware.HasPermission([]string{"update:service_token:own"}),
		)(core.AppHandler(sth.UpdateServiceToken)))

	router.Handle("POST /api/v1/service-tokens/{id}/rotate",
		middleware.CreateStack(
			middleware.IsAuthenticated(sth.Cfg, sth.DB, sth.Cacher, sth.Logger),
			middleware.HasPermission([]string{"rotate:service_token:own"}),
		)(core.AppHandler(sth.RotateServiceToken)))

	router.Handle("DELETE /api/v1/service-tokens/{id}",
		middleware.CreateStack(
			middleware.IsAuthenticated(sth.Cfg, sth.DB, sth.Cacher, sth.Logger),
			middleware.HasPermission([]string{"revoke:service_token:own"}),
		)(core.AppHandler(sth.RevokeServiceToken)))

	router.Handle("GET /api/v1/service-tokens/stats",
		middleware.CreateStack(
			middleware.IsAuthenticated(sth.Cfg, sth.DB, sth.Cacher, sth.Logger),
			middleware.HasPermission([]string{"read:service_token:own"}),
		)(core.AppHandler(sth.GetServiceTokenStats)))

	// Admin routes for managing any service tokens
	router.Handle("GET /api/v1/admin/service-tokens",
		middleware.CreateStack(
			middleware.IsAuthenticated(sth.Cfg, sth.DB, sth.Cacher, sth.Logger),
			middleware.HasPermission([]string{"list:service_token:any"}),
		)(core.AppHandler(sth.ListAllServiceTokens)))

	router.Handle("POST /api/v1/admin/service-tokens/cleanup",
		middleware.CreateStack(
			middleware.IsAuthenticated(sth.Cfg, sth.DB, sth.Cacher, sth.Logger),
			middleware.HasPermission([]string{"update:service_token:any"}),
		)(core.AppHandler(sth.CleanupExpiredTokens)))
}

// callerAccountID extracts and parses the caller's account id from the JWT
// claims already loaded into the request context by IsAuthenticated. Shared
// by every endpoint here, which all did this identically: missing claims is
// 401, a subject that fails to parse as a UUID is 400.
func (sth *ServiceTokenHandler) callerAccountID(
	r *http.Request,
) (uuid.UUID, error) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		return uuid.Nil, core.Public(core.ErrUnauthorized, msgUnauthorized)
	}
	accountID, err := uuid.Parse(claims.Subject)
	if err != nil {
		sth.Logger.Error(
			"Failed to parse account ID from claims",
			slog.String("error", err.Error()),
		)
		return uuid.Nil, core.Public(core.ErrInvalidInput, msgInvalidToken)
	}
	return accountID, nil
}

// tokenIDFromPath extracts the {id} path segment the hard way, matching what
// this handler did before the extraction: splitting the raw URL path rather
// than using r.PathValue. minParts is 5 for a path ending .../{id}, 6 for the
// rotate route, which has one more segment after the id.
func tokenIDFromPath(r *http.Request, minParts int) (uuid.UUID, error) {
	pathParts := strings.Split(r.URL.Path, "/")
	if len(pathParts) < minParts {
		return uuid.Nil, core.Public(core.ErrInvalidInput, msgInvalidTokenID)
	}
	id, err := uuid.Parse(pathParts[4])
	if err != nil {
		return uuid.Nil, core.Public(core.ErrInvalidInput, msgInvalidTokenID)
	}
	return id, nil
}

// isOwnerOrAdmin reports whether the caller may act on token: either they
// hold adminPerm, or the token belongs to their own account.
func isOwnerOrAdmin(
	perms []string,
	adminPerm string,
	token repository.ServiceToken,
	callerAccountID uuid.UUID,
) bool {
	return slices.Contains(perms, adminPerm) ||
		token.AccountID == callerAccountID
}

// acquireRunAndCommit calls Acquire and WithTransaction, distinguishing a
// commit failure from an Acquire or Begin failure. Unlike activity and
// streak's helper of the same name, Begin failure here shares its message
// with Acquire failure (msgInternalError, both meant "we couldn't even start
// working on this"), while a Commit failure shares ITS message with whatever
// repo-call failure the caller's closure would have reported -- passed in as
// commitFailureMsg, since it differs per endpoint (create, update, rotate,
// revoke each have their own wording). The distinction is made the same way
// as activity and streak: by tracking whether fn reached its own success
// return.
func (sth *ServiceTokenHandler) acquireRunAndCommit(
	r *http.Request,
	commitFailureMsg string,
	fn func(tx pgx.Tx) error,
) error {
	conn, err := sth.DB.Acquire(r.Context())
	if err != nil {
		sth.Logger.Error(
			"Failed to get database connection",
			slog.String("error", err.Error()),
		)
		return core.Public(core.ErrInternal, msgInternalError)
	}

	var reachedSuccess bool
	err = core.WithTransaction(r.Context(), conn, func(tx pgx.Tx) error {
		if err := fn(tx); err != nil {
			return err
		}
		reachedSuccess = true
		return nil
	})
	if err == nil {
		return nil
	}
	if reachedSuccess {
		return core.Fallback(err, core.ErrInternal, commitFailureMsg)
	}
	return core.Fallback(err, core.ErrInternal, msgInternalError)
}

// CreateServiceToken godoc
//
// @Summary      Create a service token for the authenticated account
// @Description  Mints a service token for machine-to-machine calls, presented as X-Api-Key rather than a Bearer JWT. The raw token value is returned only in this response and is not recoverable afterwards; only its hash is stored.
// @Tags         service-tokens
// @Accept       json
// @Produce      json
// @Param        request  body      servicetoken.ServiceTokenRequest  true  "Service token to create"
// @Success      201  {object}  servicetoken.ServiceTokenResponse  "Includes the raw token, only returned on creation"
// @Failure      400  {object}  core.APIError  "Invalid request body"
// @Failure      401  {object}  core.APIError  "Missing or invalid claims"
// @Failure      500  {object}  core.APIError  "Failed to create service token"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /api/v1/service-tokens [post]
func (sth *ServiceTokenHandler) CreateServiceToken(
	w http.ResponseWriter,
	r *http.Request,
) error {
	accountID, err := sth.callerAccountID(r)
	if err != nil {
		return err
	}

	var req ServiceTokenRequest
	var token string
	var serviceToken repository.ServiceToken

	if err := sth.acquireRunAndCommit(
		r,
		msgCreateFailed,
		func(tx pgx.Tx) error {
			svc := sth.svc(tx)

			// Verify the account is a bot account
			if _, err := svc.VerifyBotAccount(
				r.Context(),
				accountID,
			); err != nil {
				sth.Logger.Error(
					"Failed to get account",
					slog.String("error", err.Error()),
				)
				if errors.Is(err, servicetokensvc.ErrNotBotAccount) {
					return core.Public(core.ErrForbidden, msgNotBotAccount)
				}
				return core.Public(core.ErrNotFound, msgAccountNotFound)
			}

			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				return core.Public(core.ErrInvalidInput, msgInvalidBody)
			}

			if err := sth.validateServiceTokenRequest(&req); err != nil {
				return core.Public(core.ErrInvalidInput, err.Error())
			}

			newToken, err := sth.generateSecureToken()
			if err != nil {
				sth.Logger.Error(
					"Failed to generate secure token",
					slog.String("error", err.Error()),
				)
				return core.Public(core.ErrInternal, msgGenerateFailed)
			}
			token = newToken

			var expiresAt *time.Time
			if req.ExpiresInDays != nil {
				expiry := time.Now().AddDate(0, 0, *req.ExpiresInDays)
				expiresAt = &expiry
			} else {
				expiry := time.Now().AddDate(1, 0, 0)
				expiresAt = &expiry
			}

			var rotationPolicyJSON []byte
			if req.RotationPolicy != nil {
				rotationPolicyJSON, err = json.Marshal(req.RotationPolicy)
				if err != nil {
					sth.Logger.Error(
						"Failed to marshal rotation policy",
						slog.String("error", err.Error()),
					)
					return core.Public(core.ErrInvalidInput, msgInvalidRotation)
				}
			}

			var metadataJSON []byte
			if req.Metadata != nil {
				metadataJSON, err = json.Marshal(req.Metadata)
				if err != nil {
					sth.Logger.Error(
						"Failed to marshal metadata",
						slog.String("error", err.Error()),
					)
					return core.Public(core.ErrInvalidInput, msgInvalidMetadata)
				}
			}

			created, err := svc.Create(
				r.Context(),
				repository.CreateServiceTokenParams{
					AccountID:   accountID,
					Name:        req.Name,
					Description: req.Description,
					TokenHash:   tokens.HashToken(newToken),
					ExpiresAt:   expiresAt,
					Scopes:      req.Scopes,
					MaxUses: func() *int32 {
						if req.MaxUses == nil {
							return nil
						}
						val := int32(*req.MaxUses)
						return &val
					}(),
					RotationPolicy:   rotationPolicyJSON,
					IpWhitelist:      req.IPWhitelist,
					UserAgentPattern: req.UserAgentPattern,
					CreatedBy:        &accountID,
					Metadata:         metadataJSON,
				},
			)
			if err != nil {
				sth.Logger.Error(
					"Failed to create service token",
					slog.String("error", err.Error()),
				)
				return core.Public(core.ErrInternal, msgCreateFailed)
			}
			serviceToken = created
			return nil
		},
	); err != nil {
		return err
	}

	response := sth.convertToServiceTokenResponse(serviceToken)
	response.Token = token // Only include token on creation

	core.WriteJSON(w, http.StatusCreated, response)
	return nil
}

// ListServiceTokens godoc
//
// @Summary      List the authenticated account's own service tokens
// @Description  Returns the caller's service tokens as metadata only. Raw token values are never returned here, only at creation and rotation.
// @Tags         service-tokens
// @Produce      json
// @Success      200  {array}   servicetoken.ServiceTokenResponse
// @Failure      401  {object}  core.APIError  "Missing or invalid claims"
// @Failure      500  {object}  core.APIError  "Failed to list service tokens"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /api/v1/service-tokens [get]
func (sth *ServiceTokenHandler) ListServiceTokens(
	w http.ResponseWriter,
	r *http.Request,
) error {
	accountID, err := sth.callerAccountID(r)
	if err != nil {
		return err
	}

	conn, err := sth.DB.Acquire(r.Context())
	if err != nil {
		sth.Logger.Error(
			"Failed to get database connection",
			slog.String("error", err.Error()),
		)
		return core.Public(core.ErrInternal, msgInternalError)
	}
	defer conn.Release()

	tokenList, err := sth.svc(conn).List(r.Context(), accountID)
	if err != nil {
		sth.Logger.Error(
			"Failed to list service tokens", slog.String("error", err.Error()),
		)
		return core.Public(core.ErrInternal, msgListFailed)
	}

	responses := make([]ServiceTokenResponse, len(tokenList))
	for i, token := range tokenList {
		responses[i] = sth.convertToServiceTokenResponse(token)
	}

	core.WriteJSON(w, http.StatusOK, responses)
	return nil
}

// GetServiceToken godoc
//
// @Summary      Get a service token by id
// @Description  Requires read:service_token:own for the caller's own tokens, or read:service_token:any to read any account's token.
// @Tags         service-tokens
// @Produce      json
// @Param        id  path  string  true  "Service token ID"
// @Success      200  {object}  servicetoken.ServiceTokenResponse
// @Failure      400  {object}  core.APIError  "Invalid token"
// @Failure      403  {object}  core.APIError  "Not the token owner and not an admin"
// @Failure      404  {object}  core.APIError  "Service token not found"
// @Failure      500  {object}  core.APIError  "Failed to fetch service token"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /api/v1/service-tokens/{id} [get]
func (sth *ServiceTokenHandler) GetServiceToken(
	w http.ResponseWriter,
	r *http.Request,
) error {
	tokenID, err := tokenIDFromPath(r, 5)
	if err != nil {
		return err
	}

	accountID, err := sth.callerAccountID(r)
	if err != nil {
		return err
	}

	conn, err := sth.DB.Acquire(r.Context())
	if err != nil {
		sth.Logger.Error(
			"Failed to get database connection",
			slog.String("error", err.Error()),
		)
		return core.Public(core.ErrInternal, msgInternalError)
	}
	defer conn.Release()

	token, err := sth.svc(conn).GetByID(r.Context(), tokenID)
	if err != nil {
		// Every error here becomes "token not found", not just a genuine
		// not-found -- that is what this endpoint did before the extraction
		// (no errors.Is check existed) and is preserved rather than tightened.
		return core.Public(core.ErrNotFound, msgTokenNotFound)
	}

	perms := middleware.PermissionsFromContext(r.Context())
	if !isOwnerOrAdmin(perms, "read:service_token:any", token, accountID) {
		return core.Public(core.ErrForbidden, msgAccessDenied)
	}

	core.WriteJSON(w, http.StatusOK, sth.convertToServiceTokenResponse(token))
	return nil
}

// UpdateServiceToken godoc
//
// @Summary      Update a service token
// @Description  Requires update:service_token:own for the caller's own tokens, or update:service_token:any to update any account's token.
// @Tags         service-tokens
// @Accept       json
// @Produce      json
// @Param        id       path  string                                true  "Service token ID"
// @Param        request  body  servicetoken.ServiceTokenUpdateRequest  true  "Fields to update"
// @Success      200  {object}  servicetoken.ServiceTokenResponse
// @Failure      400  {object}  core.APIError  "Invalid id or request body"
// @Failure      403  {object}  core.APIError  "Not the token owner and not an admin"
// @Failure      404  {object}  core.APIError  "Service token not found"
// @Failure      500  {object}  core.APIError  "Failed to update service token"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /api/v1/service-tokens/{id} [put]
func (sth *ServiceTokenHandler) UpdateServiceToken(
	w http.ResponseWriter,
	r *http.Request,
) error {
	tokenID, err := tokenIDFromPath(r, 5)
	if err != nil {
		return err
	}

	accountID, err := sth.callerAccountID(r)
	if err != nil {
		return err
	}

	var req ServiceTokenUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return core.Public(core.ErrInvalidInput, msgInvalidBody)
	}

	var updatedToken repository.ServiceToken

	if err := sth.acquireRunAndCommit(
		r,
		msgUpdateFailed,
		func(tx pgx.Tx) error {
			svc := sth.svc(tx)

			token, err := svc.GetByID(r.Context(), tokenID)
			if err != nil {
				return core.Public(core.ErrNotFound, msgTokenNotFound)
			}

			perms := middleware.PermissionsFromContext(r.Context())
			if !isOwnerOrAdmin(
				perms,
				"update:service_token:any",
				token,
				accountID,
			) {
				return core.Public(core.ErrForbidden, msgAccessDenied)
			}

			var rotationPolicyJSON []byte
			if req.RotationPolicy != nil {
				rotationPolicyJSON, err = json.Marshal(req.RotationPolicy)
				if err != nil {
					sth.Logger.Error(
						"Failed to marshal rotation policy",
						slog.String("error", err.Error()),
					)
					return core.Public(core.ErrInvalidInput, msgInvalidRotation)
				}
			}

			var metadataJSON []byte
			if req.Metadata != nil {
				metadataJSON, err = json.Marshal(req.Metadata)
				if err != nil {
					sth.Logger.Error(
						"Failed to marshal metadata",
						slog.String("error", err.Error()),
					)
					return core.Public(core.ErrInvalidInput, msgInvalidMetadata)
				}
			}

			if err := svc.Update(
				r.Context(),
				repository.UpdateServiceTokenParams{
					ID:          tokenID,
					Name:        *req.Name,
					Description: req.Description,
					Scopes:      req.Scopes,
					MaxUses: func() *int32 {
						if req.MaxUses == nil {
							return nil
						}
						val := int32(*req.MaxUses)
						return &val
					}(),
					RotationPolicy:   rotationPolicyJSON,
					IpWhitelist:      req.IPWhitelist,
					UserAgentPattern: req.UserAgentPattern,
					Metadata:         metadataJSON,
				},
			); err != nil {
				sth.Logger.Error(
					"Failed to update service token",
					slog.String("error", err.Error()),
				)
				return core.Public(core.ErrInternal, msgUpdateFailed)
			}

			// Fetched inside the same transaction, before commit -- see the
			// decision recorded in ADR 0009 for why: the original code re-fetched
			// this AFTER tx.Commit() on the same, by-then-closed transaction,
			// which pgx rejects with ErrTxClosed. That almost certainly meant
			// this endpoint returned 500 on every call despite the update having
			// already succeeded. Fetching before commit is the natural way to
			// write this against core.InTx and is the one deliberate behaviour
			// change in this migration.
			updated, err := svc.GetByID(r.Context(), tokenID)
			if err != nil {
				sth.Logger.Error(
					"Failed to get updated token",
					slog.String("error", err.Error()),
				)
				return core.Public(core.ErrInternal, msgRetrieveFailed)
			}
			updatedToken = updated
			return nil
		},
	); err != nil {
		return err
	}

	core.WriteJSON(
		w,
		http.StatusOK,
		sth.convertToServiceTokenResponse(updatedToken),
	)
	return nil
}

// RotateServiceToken godoc
//
// @Summary      Rotate a service token
// @Description  Issues a new raw token value for the same service token record. Requires rotate:service_token:own for the caller's own tokens, or rotate:service_token:any for any account's token.
// @Tags         service-tokens
// @Produce      json
// @Param        id  path  string  true  "Service token ID"
// @Success      200  {object}  servicetoken.ServiceTokenResponse  "Includes the new raw token"
// @Failure      400  {object}  core.APIError  "Invalid id"
// @Failure      403  {object}  core.APIError  "Not the token owner and not an admin"
// @Failure      404  {object}  core.APIError  "Service token not found"
// @Failure      500  {object}  core.APIError  "Failed to rotate service token"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /api/v1/service-tokens/{id}/rotate [post]
func (sth *ServiceTokenHandler) RotateServiceToken(
	w http.ResponseWriter,
	r *http.Request,
) error {
	tokenID, err := tokenIDFromPath(r, 6)
	if err != nil {
		return err
	}

	accountID, err := sth.callerAccountID(r)
	if err != nil {
		return err
	}

	var newToken string
	var updatedToken repository.ServiceToken

	if err := sth.acquireRunAndCommit(
		r,
		msgRotateFailed,
		func(tx pgx.Tx) error {
			svc := sth.svc(tx)

			token, err := svc.GetByID(r.Context(), tokenID)
			if err != nil {
				return core.Public(core.ErrNotFound, msgTokenNotFound)
			}

			perms := middleware.PermissionsFromContext(r.Context())
			if !isOwnerOrAdmin(
				perms,
				"rotate:service_token:any",
				token,
				accountID,
			) {
				return core.Public(core.ErrForbidden, msgAccessDenied)
			}

			generated, err := sth.generateSecureToken()
			if err != nil {
				sth.Logger.Error(
					"Failed to generate secure token",
					slog.String("error", err.Error()),
				)
				return core.Public(core.ErrInternal, msgGenerateNewFailed)
			}
			newToken = generated

			if err := svc.Rotate(
				r.Context(),
				repository.RotateServiceTokenParams{
					ID:        tokenID,
					TokenHash: tokens.HashToken(newToken),
					ExpiresAt: token.ExpiresAt,
				},
			); err != nil {
				sth.Logger.Error(
					"Failed to rotate service token",
					slog.String("error", err.Error()),
				)
				return core.Public(core.ErrInternal, msgRotateFailed)
			}

			// See UpdateServiceToken for why this is fetched before commit rather
			// than after, unlike the pre-extraction code.
			updated, err := svc.GetByID(r.Context(), tokenID)
			if err != nil {
				sth.Logger.Error(
					"Failed to get updated token",
					slog.String("error", err.Error()),
				)
				return core.Public(core.ErrInternal, msgRetrieveFailed)
			}
			updatedToken = updated
			return nil
		},
	); err != nil {
		return err
	}

	response := sth.convertToServiceTokenResponse(updatedToken)
	response.Token = newToken // Include new token in response

	core.WriteJSON(w, http.StatusOK, response)
	return nil
}

// RevokeServiceToken godoc
//
// @Summary      Revoke a service token
// @Produce      json
// @Description  Requires revoke:service_token:own for the caller's own tokens, or revoke:service_token:any for any account's token.
// @Tags         service-tokens
// @Param        id  path  string  true  "Service token ID"
// @Success      204  "No Content"
// @Failure      400  {object}  core.APIError  "Invalid id"
// @Failure      403  {object}  core.APIError  "Not the token owner and not an admin"
// @Failure      404  {object}  core.APIError  "Service token not found"
// @Failure      500  {object}  core.APIError  "Failed to revoke service token"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /api/v1/service-tokens/{id} [delete]
func (sth *ServiceTokenHandler) RevokeServiceToken(
	w http.ResponseWriter,
	r *http.Request,
) error {
	tokenID, err := tokenIDFromPath(r, 5)
	if err != nil {
		return err
	}

	accountID, err := sth.callerAccountID(r)
	if err != nil {
		return err
	}

	if err := sth.acquireRunAndCommit(
		r,
		msgRevokeFailed,
		func(tx pgx.Tx) error {
			svc := sth.svc(tx)

			token, err := svc.GetByID(r.Context(), tokenID)
			if err != nil {
				return core.Public(core.ErrNotFound, msgTokenNotFound)
			}

			perms := middleware.PermissionsFromContext(r.Context())
			if !isOwnerOrAdmin(
				perms,
				"revoke:service_token:any",
				token,
				accountID,
			) {
				return core.Public(core.ErrForbidden, msgAccessDenied)
			}

			if err := svc.Revoke(r.Context(), tokenID); err != nil {
				sth.Logger.Error(
					"Failed to revoke service token",
					slog.String("error", err.Error()),
				)
				return core.Public(core.ErrInternal, msgRevokeFailed)
			}
			return nil
		},
	); err != nil {
		return err
	}

	w.WriteHeader(http.StatusNoContent)
	return nil
}

// GetServiceTokenStats godoc
//
// @Summary      Get usage statistics for the authenticated account's service tokens
// @Description  Returns aggregate counts over the caller's own service tokens, such as how many are active, expired, or revoked.
// @Tags         service-tokens
// @Produce      json
// @Success      200  {object}  servicetoken.ServiceTokenStats
// @Failure      401  {object}  core.APIError  "Missing or invalid claims"
// @Failure      500  {object}  core.APIError  "Failed to fetch stats"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /api/v1/service-tokens/stats [get]
func (sth *ServiceTokenHandler) GetServiceTokenStats(
	w http.ResponseWriter,
	r *http.Request,
) error {
	accountID, err := sth.callerAccountID(r)
	if err != nil {
		return err
	}

	conn, err := sth.DB.Acquire(r.Context())
	if err != nil {
		sth.Logger.Error(
			"Failed to get database connection",
			slog.String("error", err.Error()),
		)
		return core.Public(core.ErrInternal, msgInternalError)
	}
	defer conn.Release()

	stats, err := sth.svc(conn).Stats(r.Context(), accountID)
	if err != nil {
		sth.Logger.Error(
			"Failed to get service token stats",
			slog.String("error", err.Error()),
		)
		return core.Public(core.ErrInternal, msgStatsFailed)
	}

	core.WriteJSON(w, http.StatusOK, ServiceTokenStats{
		TotalTokens:        int(stats.TotalTokens),
		ActiveTokens:       int(stats.ActiveTokens),
		RevokedTokens:      int(stats.RevokedTokens),
		ExpiredTokens:      int(stats.ExpiredTokens),
		RecentlyUsedTokens: int(stats.RecentlyUsedTokens),
	})
	return nil
}

// ListAllServiceTokens godoc
//
// @Summary      List all active service tokens (admin)
// @Description  Returns active service tokens across every account, as metadata only. Unlike the per-account listing this is not scoped to the caller, so it requires an admin permission.
// @Tags         service-tokens
// @Produce      json
// @Success      200  {array}   servicetoken.ServiceTokenResponse
// @Failure      500  {object}  core.APIError  "Failed to list service tokens"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /api/v1/admin/service-tokens [get]
func (sth *ServiceTokenHandler) ListAllServiceTokens(
	w http.ResponseWriter,
	r *http.Request,
) error {
	conn, err := sth.DB.Acquire(r.Context())
	if err != nil {
		sth.Logger.Error(
			"Failed to get database connection",
			slog.String("error", err.Error()),
		)
		return core.Public(core.ErrInternal, msgInternalError)
	}
	defer conn.Release()

	tokenList, err := sth.svc(conn).ListAllActive(r.Context())
	if err != nil {
		sth.Logger.Error(
			"Failed to list all service tokens",
			slog.String("error", err.Error()),
		)
		return core.Public(core.ErrInternal, msgListFailed)
	}

	responses := make([]ServiceTokenResponse, len(tokenList))
	for i, token := range tokenList {
		responses[i] = sth.convertActiveServiceTokenToResponse(token)
	}

	core.WriteJSON(w, http.StatusOK, responses)
	return nil
}

// CleanupExpiredTokens godoc
//
// @Summary      Delete expired service tokens (admin)
// @Description  Sweeps expired service tokens across all accounts. Despite the name this is a soft delete: rows are retained and stamped with revoked_at, not removed. Housekeeping only, since an expired token already fails authentication before this runs. Returns 204 with no body on success.
// @Tags         service-tokens
// @Produce      json
// @Success      204  "No Content"
// @Failure      500  {object}  core.APIError  "Failed to clean up expired tokens"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /api/v1/admin/service-tokens/cleanup [post]
func (sth *ServiceTokenHandler) CleanupExpiredTokens(
	w http.ResponseWriter,
	r *http.Request,
) error {
	conn, err := sth.DB.Acquire(r.Context())
	if err != nil {
		sth.Logger.Error(
			"Failed to get database connection",
			slog.String("error", err.Error()),
		)
		return core.Public(core.ErrInternal, msgInternalError)
	}
	defer conn.Release()

	if err := sth.svc(conn).CleanupExpired(r.Context()); err != nil {
		sth.Logger.Error(
			"Failed to cleanup expired tokens",
			slog.String("error", err.Error()),
		)
		return core.Public(core.ErrInternal, msgCleanupFailed)
	}

	w.WriteHeader(http.StatusNoContent)
	return nil
}

// Helper methods

// generateSecureToken generates a cryptographically secure token
func (sth *ServiceTokenHandler) generateSecureToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	token := "vst_" + base64.URLEncoding.EncodeToString(bytes)
	return token, nil
}

// validateServiceTokenRequest validates the service token request
func (sth *ServiceTokenHandler) validateServiceTokenRequest(
	req *ServiceTokenRequest,
) error {
	if strings.TrimSpace(req.Name) == "" {
		return fmt.Errorf("name is required")
	}

	for _, scope := range req.Scopes {
		if !sth.isValidScope(scope) {
			return fmt.Errorf("invalid scope: %s", scope)
		}
	}

	for _, ip := range req.IPWhitelist {
		if !sth.isValidIP(ip) {
			return fmt.Errorf("invalid IP address: %s", ip)
		}
	}

	if req.UserAgentPattern != nil {
		if _, err := regexp.Compile(*req.UserAgentPattern); err != nil {
			return fmt.Errorf("invalid user agent pattern: %s", err.Error())
		}
	}

	return nil
}

// isValidScope validates if a scope is valid
func (sth *ServiceTokenHandler) isValidScope(scope string) bool {
	if strings.TrimSpace(scope) == "" {
		return false
	}
	matched, _ := regexp.MatchString(`^[a-zA-Z0-9:._-]+$`, scope)
	return matched
}

// isValidIP validates if an IP address is valid
func (sth *ServiceTokenHandler) isValidIP(ip string) bool {
	matched, _ := regexp.MatchString(`^(\d{1,3}\.){3}\d{1,3}$`, ip)
	if !matched {
		return false
	}

	parts := strings.Split(ip, ".")
	for _, part := range parts {
		if len(part) > 3 || len(part) == 0 {
			return false
		}
		if part[0] == '0' && len(part) > 1 {
			return false
		}
	}

	return true
}

// convertToServiceTokenResponse converts a repository ServiceToken to ServiceTokenResponse
func (sth *ServiceTokenHandler) convertToServiceTokenResponse(
	token repository.ServiceToken,
) ServiceTokenResponse {
	response := ServiceTokenResponse{
		ID:          token.ID,
		Name:        token.Name,
		Description: token.Description,
		ExpiresAt:   token.ExpiresAt,
		Scopes:      token.Scopes,
		MaxUses: func() *int {
			if token.MaxUses == nil {
				return nil
			}
			val := int(*token.MaxUses)
			return &val
		}(),
		UseCount:   int(*token.UseCount),
		CreatedAt:  token.CreatedAt,
		LastUsedAt: token.LastUsedAt,
		RotatedAt:  token.RotatedAt,
		RevokedAt:  token.RevokedAt,
	}

	if token.Metadata != nil {
		json.Unmarshal(token.Metadata, &response.Metadata)
	}

	return response
}

// convertActiveServiceTokenToResponse converts a repository ActiveServiceToken to ServiceTokenResponse
func (sth *ServiceTokenHandler) convertActiveServiceTokenToResponse(
	token repository.ActiveServiceToken,
) ServiceTokenResponse {
	response := ServiceTokenResponse{
		ID:          token.ID,
		Name:        token.Name,
		Description: token.Description,
		ExpiresAt:   token.ExpiresAt,
		Scopes:      token.Scopes,
		MaxUses: func() *int {
			if token.MaxUses == nil {
				return nil
			}
			val := int(*token.MaxUses)
			return &val
		}(),
		UseCount:   int(*token.UseCount),
		CreatedAt:  token.CreatedAt,
		LastUsedAt: token.LastUsedAt,
		RotatedAt:  token.RotatedAt,
		RevokedAt:  token.RevokedAt,
	}

	if token.Metadata != nil {
		json.Unmarshal(token.Metadata, &response.Metadata)
	}

	return response
}
