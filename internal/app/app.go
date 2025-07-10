package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/opencrafts-io/verisafe/internal/config"
	"github.com/opencrafts-io/verisafe/internal/middleware"
	"github.com/pressly/goose/v3"
)

type App struct {
	config *config.Config
	logger *slog.Logger
	pool   *pgxpool.Pool
}

// Returns a new instance of the application
// with a connection instance to the database pool
func New(logger *slog.Logger, config *config.Config) (*App, error) {

	dbConfig, err := pgxpool.ParseConfig(fmt.Sprintf(
		"postgresql://%s:%s@%s:%d/%s?sslmode=disable",
		config.DatabaseConfig.DatabaseUser,
		config.DatabaseConfig.DatabasePassword,
		config.DatabaseConfig.DatabaseHost,
		config.DatabaseConfig.DatabasePort,
		config.DatabaseConfig.DatabaseName,
	))
	if err != nil {
		return nil, err
	}

	dbConfig.MaxConns = config.DatabaseConfig.DatabasePoolMaxConnections
	dbConfig.MinConns = config.DatabaseConfig.DatabasePoolMinConnections
	dbConfig.MaxConnLifetime = time.Hour * time.Duration(config.DatabaseConfig.DatabasePoolMaxConnectionLifetime)

	connPool, err := pgxpool.NewWithConfig(context.Background(), dbConfig)
	if err != nil {
		return nil, err
	}

	return &App{
		config: config,
		logger: logger,
		pool:   connPool,
	}, nil
}

// Runs the all Goose migrations.
func (a *App) runMigrations() {

	migrationsDir := filepath.Join("database", "migrations")

	if err := goose.SetDialect(string(goose.DialectPostgres)); err != nil {
		panic(err)
	}

	db := stdlib.OpenDBFromPool(a.pool)

	// Run the migrations from the specified directory
	if err := goose.Up(db, migrationsDir); err != nil {
		panic(err)
	}

	a.logger.Info("Migrations ran successfully!")

}

// Starts the application server
func (a *App) Start(ctx context.Context) error {
	a.runMigrations()

	middlewares := middleware.CreateStack(
		middleware.Logging(a.logger),
		middleware.WithDBConnection(a.logger, a.pool),
	)
	router := a.loadRoutes()

	srv := &http.Server{
		Addr:    fmt.Sprintf("%s:%d", a.config.AppConfig.Addres, a.config.AppConfig.Port),
		Handler: middlewares(router),
	}

	errCh := make(chan error, 1)

	go func() {
		err := srv.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("failed to listen and serve: %w", err)
		}

		close(errCh)
	}()

	a.logger.Info("server running",
		slog.String("Address", a.config.AppConfig.Addres),
		slog.Int("port", a.config.AppConfig.Port),
	)

	select {
	// Wait until we receive SIGINT (ctrl+c on cli)
	case <-ctx.Done():
		break
	case err := <-errCh:
		return err
	}

	sCtx, cancel := context.WithTimeout(context.Background(), time.Second*15)
	defer cancel()

	srv.Shutdown(sCtx)

	return nil
}
