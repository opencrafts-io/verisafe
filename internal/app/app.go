package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/opencrafts-io/verisafe/database"
	"github.com/opencrafts-io/verisafe/internal/broker"
	"github.com/opencrafts-io/verisafe/internal/config"
	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/eventbus"
	"github.com/opencrafts-io/verisafe/internal/geo"
	"github.com/opencrafts-io/verisafe/internal/middleware"
	"github.com/opencrafts-io/verisafe/internal/providers"
	"github.com/opencrafts-io/verisafe/internal/secrets"
	"github.com/opencrafts-io/verisafe/internal/service/grants"
	"github.com/redis/go-redis/v9"
)

type App struct {
	config               *config.Config
	logger               *slog.Logger
	pool                 *pgxpool.Pool
	userEventBus         *eventbus.UserEventBus
	notificationEventBus *eventbus.NotificationEventBus
	institutionEventBus  *eventbus.InstitutionEventBus
	geoIPLocator         *geo.GeoIPLocater
	cacher               core.Cacher

	// Third-party OAuth plumbing: the provider registry, the AES-GCM sealer
	// that protects stored provider tokens, and the token endpoint client.
	oauthRegistry  *providers.Registry
	tokenSealer    *secrets.Sealer
	tokenExchanger providers.TokenExchanger

	// reconciler drains grants whose scopes are still presumed rather than
	// provider-verified. Nil when OAUTH_RECONCILE_ENABLED is off.
	reconciler   *grants.OAuthReconciler
	reconcilerWG sync.WaitGroup

	rabbitMQConn broker.Connection
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
	dbConfig.MaxConnLifetime = time.Hour * time.Duration(
		config.DatabaseConfig.DatabasePoolMaxConnectionLifetime,
	)

	connPool, err := pgxpool.NewWithConfig(context.Background(), dbConfig)
	if err != nil {
		return nil, err
	}

	rdb := redis.NewClient(&redis.Options{
		Addr:         config.RedisConfig.RedisAddress,
		Password:     config.RedisConfig.RedisPassword,
		DB:           config.RedisConfig.RedisDB,
		ClientName:   "io.opencrats.verisafe",
		MinIdleConns: 10,
		OnConnect: func(ctx context.Context, cn *redis.Conn) error {
			logger.Info("Connected to RedisDB")
			return nil
		},
	})
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to redis: %w", err)
	}
	cache := core.NewRedisCacher(rdb)

	rabbitMQConnString := fmt.Sprintf(
		"amqp://%s:%s@%s:%d/",
		config.RabbitMQConfig.RabbitMQUser,
		config.RabbitMQConfig.RabbitMQPass,
		config.RabbitMQConfig.RabbitMQAddress,
		config.RabbitMQConfig.RabbitMQPort,
	)

	rabbitMQConn, err := broker.NewRabbitMQConnection(
		context.Background(),
		rabbitMQConnString,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to connect to rabbit mq  event bus %w",
			err,
		)
	}

	userEventBus, err := eventbus.NewUserEventBus(config, logger)
	if err != nil {
		return nil, err
	}

	institutionEventBus, err := eventbus.NewInstitutionEventBus(config, logger)
	if err != nil {
		return nil, err
	}

	notificationEventBus, err := eventbus.NewNotificationEventBus(
		config,
		logger,
	)
	if err != nil {
		return nil, err
	}

	gil, err := geo.NewGeoIPLocater(
		"",
		"",
	)
	if err != nil {
		return nil, err
	}

	// Built eagerly so a malformed PROVIDER_TOKEN_ENC_KEYS stops the process
	// at startup rather than surfacing at the first token write.
	tokenSealer, err := secrets.NewSealer(
		config.ProviderTokensConfig.EncryptionKeys,
		config.ProviderTokensConfig.ActiveKeyVersion,
	)
	if err != nil {
		logger.Error("failed to initialise provider token sealer", "error", err)
		return nil, fmt.Errorf("provider token encryption: %w", err)
	}

	return &App{
		config:               config,
		logger:               logger,
		pool:                 connPool,
		userEventBus:         userEventBus,
		notificationEventBus: notificationEventBus,
		institutionEventBus:  institutionEventBus,
		geoIPLocator:         gil,
		cacher:               cache,
		oauthRegistry:        providers.NewRegistry(config),
		tokenSealer:          tokenSealer,
		tokenExchanger:       providers.NewOAuth2Exchanger(config, nil),
		rabbitMQConn:         rabbitMQConn,
	}, nil
}

// Starts the application server
func (a *App) Start(ctx context.Context) error {
	database.RunGooseMigrations(a.logger, a.pool)

	allowedOrigins := []string{
		"*",
		"http://localhost:1337",
		"https://academia.opencrafts.io",
	}

	middlewares := middleware.CreateStack(
		middleware.Logging(a.logger),
		middleware.CORS(allowedOrigins),
	)
	router := a.loadRoutes()

	srv := &http.Server{
		Addr: fmt.Sprintf(
			"%s:%d",
			a.config.AppConfig.Address,
			a.config.AppConfig.Port,
		),
		Handler: middlewares(router),
	}

	a.startReconciler(ctx)

	errCh := make(chan error, 1)

	go func() {
		err := srv.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("failed to listen and serve: %w", err)
		}

		close(errCh)
	}()

	a.logger.Info(
		"server running",
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
	// The reconciler selects on the same ctx that just fired, so this only
	// waits for an in-flight batch to notice and unwind.
	a.reconcilerWG.Wait()
	a.Shutdown()
	a.geoIPLocator.Close()
	a.userEventBus.Close()
	a.institutionEventBus.Close()
	a.notificationEventBus.Close()
	return nil
}

// startReconciler launches the background worker that converts presumed OAuth
// scope grants into provider-verified ones. Off by default: it is a migration
// aid with a finite job, not steady-state infrastructure.
func (a *App) startReconciler(ctx context.Context) {
	if !a.config.ProviderTokensConfig.ReconcileEnabled {
		a.logger.Info("oauth reconciler disabled")
		return
	}

	a.reconciler = grants.NewOAuthReconciler(
		&core.PgxPoolAdapter{Pool: a.pool},
		a.cacher,
		a.oauthRegistry,
		a.tokenSealer,
		a.tokenExchanger,
		a.config,
		a.logger,
	)

	a.reconcilerWG.Add(1)
	go func() {
		defer a.reconcilerWG.Done()
		a.reconciler.Run(ctx)
	}()
}

func (a *App) Shutdown() {
	if err := a.rabbitMQConn.Close(); err != nil {
		panic(err)
	}
}
