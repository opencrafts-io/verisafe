package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/opencrafts-io/verisafe/internal/config"
	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/repository"
	"github.com/opencrafts-io/verisafe/internal/tokens"
)

const (
	AuthUserClaims            = "middleware.auth.claims"
	AuthUserPerms             = "middleware.auth.perms"
	AuthUserRoles             = "middleware.auth.roles"
	AuthUserIsPendingDeletion = "middleware.auth.pending_deletion"
)

// errAbort is returned inside the transaction closure when the HTTP response
// has already been written. The outer handler checks for non-nil and stops.
var errAbort = fmt.Errorf("abort")

// IsAuthenticated validates the incoming request using either a Bearer JWT
// or an X-API-Key header. On success it injects claims, roles, and permissions
// into the request context for downstream handlers.
func IsAuthenticated(
	cfg *config.Config,
	db core.IDBProvider,
	cacher core.Cacher,
	logger *slog.Logger,
) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Add("Content-Type", "application/json")

			authHeader := r.Header.Get("Authorization")
			apiKey := r.Header.Get("X-API-Key")

			conn, err := db.Acquire(r.Context())
			if err != nil {
				logger.Error(
					"failed to acquire db connection",
					slog.Any("error", err),
				)
				writeUnauthorized(w, "internal server error")
				return
			}

			var (
				claims *tokens.VerisafeClaims
				ctx    = r.Context()
			)

			err = core.WithTransaction(
				r.Context(),
				conn,
				func(tx pgx.Tx) error {
					repo := repository.New(tx)
					tokenSvc := tokens.NewTokenService(repo, cacher, cfg)

					switch {
					case strings.HasPrefix(authHeader, "Bearer "):
						rawToken := strings.TrimPrefix(authHeader, "Bearer ")

						parsedClaims, err := tokenSvc.ValidateAccessToken(
							r.Context(),
							rawToken,
						)
						if err != nil {
							writeUnauthorized(w, err.Error())
							return errAbort
						}
						claims = parsedClaims

					case apiKey != "":
						serviceToken, err := repo.GetServiceTokenByHash(
							r.Context(),
							tokens.HashToken(apiKey),
						)
						if err != nil {
							writeUnauthorized(w, "invalid or expired API key")
							return errAbort
						}

						if err := validateServiceToken(serviceToken, r); err != nil {
							writeUnauthorized(w, err.Error())
							return errAbort
						}

						if err := repo.UpdateServiceTokenLastUsed(r.Context(), serviceToken.ID); err != nil {
							logger.Error(
								"failed to update service token last used",
								slog.Any("error", err),
							)
						}

						account, err := repo.GetAccountByID(
							r.Context(),
							serviceToken.AccountID,
						)
						if err != nil {
							logger.Error(
								"failed to load account from API key",
								slog.Any("error", err),
							)
							writeUnauthorized(w, "unauthorized")
							return errAbort
						}

						if account.DeletedAt != nil {
							if time.Now().
								After(account.DeletedAt.Add(14 * 24 * time.Hour)) {
								writeUnauthorized(
									w,
									"account was permanently deleted",
								)
								return errAbort
							}
							ctx = context.WithValue(
								ctx,
								AuthUserIsPendingDeletion,
								true,
							)
						}

						if account.Type != repository.AccountTypeBot {
							logger.Error(
								"service token used by non-bot account",
								slog.String("account_id", account.ID.String()),
								slog.String(
									"account_type",
									string(account.Type),
								),
							)
							writeUnauthorized(
								w,
								"service tokens can only be used by bot accounts",
							)
							return errAbort
						}

						claims = &tokens.VerisafeClaims{
							RegisteredClaims: jwt.RegisteredClaims{
								Subject: account.ID.String(),
							},
						}

					default:
						writeUnauthorized(
							w,
							"missing Authorization or X-API-Key header",
						)
						return errAbort
					}

					subID, err := uuid.Parse(claims.Subject)
					if err != nil {
						logger.Error(
							"failed to parse subject from token",
							slog.Any("error", err),
						)
						writeUnauthorized(
							w,
							"could not decode token, please re-login",
						)
						return errAbort
					}

					roles, err := repo.GetAllUserRoleNames(r.Context(), subID)
					if err != nil {
						logger.Error(
							"failed to retrieve user roles",
							slog.Any("error", err),
						)
						writeUnauthorized(w, "could not retrieve your roles")
						return errAbort
					}

					perms, err := repo.GetUserPermissionNames(
						r.Context(),
						subID,
					)
					if err != nil {
						logger.Error(
							"failed to retrieve user permissions",
							slog.Any("error", err),
						)
						writeUnauthorized(
							w,
							"could not retrieve your permissions",
						)
						return errAbort
					}

					ctx = context.WithValue(ctx, AuthUserClaims, claims)
					ctx = context.WithValue(ctx, AuthUserRoles, roles)
					ctx = context.WithValue(ctx, AuthUserPerms, perms)

					return nil
				},
			)
			if err != nil {
				return
			}

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// HasPermission checks that the authenticated user has all required permissions.
// IsAuthenticated must run before this middleware to populate the context.
func HasPermission(permissions []string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var perms []string
			if v := r.Context().Value(AuthUserPerms); v != nil {
				perms = v.([]string)
			}

			for _, required := range permissions {
				if !slices.Contains(perms, required) {
					w.WriteHeader(http.StatusForbidden)
					json.NewEncoder(w).Encode(map[string]any{
						"error": "you do not have the necessary permissions to perform this action",
					})
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// --- helpers ---

func writeUnauthorized(w http.ResponseWriter, message string) {
	w.WriteHeader(http.StatusUnauthorized)
	json.NewEncoder(w).Encode(map[string]any{"error": message})
}

func validateServiceToken(
	token repository.ServiceToken,
	r *http.Request,
) error {
	if token.RevokedAt != nil {
		return fmt.Errorf("token has been revoked")
	}

	if token.ExpiresAt != nil && token.ExpiresAt.Before(time.Now()) {
		return fmt.Errorf("token has expired")
	}

	if token.MaxUses != nil && token.UseCount != nil &&
		*token.UseCount >= *token.MaxUses {
		return fmt.Errorf("token usage limit exceeded")
	}

	if len(token.IpWhitelist) > 0 {
		clientIP := getClientIP(r)
		allowed := false
		for _, ip := range token.IpWhitelist {
			if clientIP == ip {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("access denied from IP address")
		}
	}

	if token.UserAgentPattern != nil && *token.UserAgentPattern != "" {
		matched, err := regexp.MatchString(
			*token.UserAgentPattern,
			r.Header.Get("User-Agent"),
		)
		if err != nil {
			return fmt.Errorf("invalid user agent pattern configuration")
		}
		if !matched {
			return fmt.Errorf("user agent not allowed")
		}
	}

	return nil
}

func getClientIP(r *http.Request) string {
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		if i := strings.Index(ip, ","); i != -1 {
			return strings.TrimSpace(ip[:i])
		}
		return strings.TrimSpace(ip)
	}
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return strings.TrimSpace(ip)
	}
	if ip := r.Header.Get("X-Client-IP"); ip != "" {
		return strings.TrimSpace(ip)
	}
	if r.RemoteAddr != "" {
		if i := strings.LastIndex(r.RemoteAddr, ":"); i != -1 {
			return r.RemoteAddr[:i]
		}
		return r.RemoteAddr
	}
	return ""
}
