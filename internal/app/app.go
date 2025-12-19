package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/opencrafts-io/verisafe/database"
	"github.com/opencrafts-io/verisafe/internal/config"
	"github.com/opencrafts-io/verisafe/internal/eventbus"
	"github.com/opencrafts-io/verisafe/internal/middleware"
)

type App struct {
	config               *config.Config
	logger               *slog.Logger
	pool                 *pgxpool.Pool
	userEventBus         *eventbus.UserEventBus
	notificationEventBus *eventbus.NotificationEventBus
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

	userEventBus, err := eventbus.NewUserEventBus(config, logger)
	if err != nil {
		return nil, err
	}

	notificationEventBus, err := eventbus.NewNotificationEventBus(config, logger)
	if err != nil {
		return nil, err
	}

	logger.Info(
		"Using the following secrets",
		slog.Any("secrets", config),
		slog.Any("API_SECRET", config.JWTConfig.ApiSecret),
	)

	return &App{
		config:               config,
		logger:               logger,
		pool:                 connPool,
		userEventBus:         userEventBus,
		notificationEventBus: notificationEventBus,
	}, nil
}

// Starts the application server
func (a *App) Start(ctx context.Context) error {

	database.RunGooseMigrations(a.logger, a.pool)

	allowedOrigins := []string{
		"http://localhost:1337",
		"https://academia.opencrafts.io",
	}

	middlewares := middleware.CreateStack(
		middleware.Logging(a.logger),
		middleware.WithDBConnection(a.logger, a.pool),
		middleware.CORSMiddleware(allowedOrigins),
	)
	router := a.loadRoutes()

	srv := &http.Server{
		Addr:    fmt.Sprintf("%s:%d", a.config.AppConfig.Address, a.config.AppConfig.Port),
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
		slog.String("Address", a.config.AppConfig.Address),
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
