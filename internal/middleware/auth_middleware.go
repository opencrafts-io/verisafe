package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/opencrafts-io/verisafe/internal/utils"
)

const AuthUserClaims = "middleware.auth.claims"

func IsAuthenticated(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization := r.Header.Get("Authorization")

		w.Header().Add("Content-Type","application/json")

		// Check that the header begins with a prefix of Bearer
		if !strings.HasPrefix(authorization, "Bearer ") {
			json.NewEncoder(w).Encode(map[string]any{
				"error": "Please provide your authorization  token",
			})
			return

		}

		// Pull out the token
		token := strings.TrimPrefix(authorization, "Bearer ")
		claims, err := utils.ValidateJWT(token, "")
		if err != nil {
			json.NewEncoder(w).Encode(map[string]any{
				"error": err.Error(),
			})
			return
		}

		ctx := context.WithValue(r.Context(), AuthUserClaims, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func ConditionalAuthMiddleware(publicPaths []string, authMiddleware func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for _, path := range publicPaths {
				if strings.HasPrefix(r.URL.Path, path) {
					// Skip authentication for public paths
					next.ServeHTTP(w, r)
					return
				}
			}
			// Apply auth middleware
			authMiddleware(next).ServeHTTP(w, r)
		})
	}
}
