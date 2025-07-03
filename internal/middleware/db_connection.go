package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
)

const DBConnectionContextKey = "db.middlewares.connection"

type DBConnectionMiddleWare struct {
	Pool   *pgxpool.Pool
	Logger *slog.Logger
}

func WithDBConnection(logger *slog.Logger, pool *pgxpool.Pool) Middleware {
	return func(next http.Handler) http.Handler {
		return withDBConnection(logger, pool, next)
	}

}

func withDBConnection(logger *slog.Logger, pool *pgxpool.Pool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := pool.Acquire(r.Context())
		if err != nil {
			logger.Error("Failed to acquire database connection from pool ", slog.Any("error", err))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{
				"message": "We ran into an issue connecting to our database",
			})
			return
		}
		defer conn.Release()

		ctx := context.WithValue(r.Context(), DBConnectionContextKey, conn)
		next.ServeHTTP(w, r.WithContext(ctx))

	})
}

// GetDBConnFromContext retrieves the pgxpool.Conn from the request context.
// This helper function makes it easier for handlers to access the connection.
func GetDBConnFromContext(ctx context.Context) (*pgxpool.Conn, error) {
	conn, ok := ctx.Value(DBConnectionContextKey).(*pgxpool.Conn)
	if !ok || conn == nil {
		return nil, errors.New("database connection not found in context")
	}
	return conn, nil
}
