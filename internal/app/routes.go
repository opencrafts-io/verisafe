package app

import (
	"net/http"

	_ "github.com/opencrafts-io/verisafe/docs"
	"github.com/opencrafts-io/verisafe/internal/auth"
	"github.com/opencrafts-io/verisafe/internal/broker"
	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/handlers/account"
	"github.com/opencrafts-io/verisafe/internal/handlers/activity"
	"github.com/opencrafts-io/verisafe/internal/handlers/device"
	"github.com/opencrafts-io/verisafe/internal/handlers/health"
	"github.com/opencrafts-io/verisafe/internal/handlers/institution"
	"github.com/opencrafts-io/verisafe/internal/handlers/leaderboard"
	"github.com/opencrafts-io/verisafe/internal/handlers/oauth"
	"github.com/opencrafts-io/verisafe/internal/handlers/permission"
	"github.com/opencrafts-io/verisafe/internal/handlers/role"
	"github.com/opencrafts-io/verisafe/internal/handlers/servicetoken"
	"github.com/opencrafts-io/verisafe/internal/handlers/social"
	"github.com/opencrafts-io/verisafe/internal/handlers/streak"
	httpSwagger "github.com/swaggo/http-swagger"
)

type VerisafeHandler interface {
	RegisterHandlers(router core.Router)
}

func (a *App) loadRoutes() http.Handler {
	authenticator, err := auth.NewAuthenticator(
		a.config,
		a.logger,
		auth.GenerateAppleClientSecret,
		a.oauthRegistry,
	)
	if err != nil {
		a.logger.Error("Failed to initialize authenticator", "error", err)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
		})
	}

	db := &core.PgxPoolAdapter{Pool: a.pool}

	verisafeHandlers := []VerisafeHandler{
		auth.NewAuthHandler(
			authenticator,
			db,
			a.cacher,
			a.userEventBus,
			a.logger,
			a.geoIPLocator,
		).WithGrantRecording(a.oauthRegistry, a.tokenSealer, a.tokenExchanger),
		&account.AccountHandler{
			DB:           db,
			Cacher:       a.cacher,
			Logger:       a.logger,
			UserEventBus: a.userEventBus,
			Cfg:          a.config,
		},
		&servicetoken.ServiceTokenHandler{
			DB:     db,
			Cacher: a.cacher,
			Logger: a.logger,
			Cfg:    a.config,
		},
		&social.SocialHandler{
			DB:     db,
			Cacher: a.cacher,
			Cfg:    a.config,
			Logger: a.logger,
		},
		&role.RoleHandler{
			DB:     db,
			Cacher: a.cacher,
			Cfg:    a.config,
			Logger: a.logger,
		},
		&permission.PermissionHandler{
			DB:     db,
			Cacher: a.cacher,
			Cfg:    a.config,
			Logger: a.logger,
		},
		&institution.InstitutionHandler{
			DB:                  db,
			Pool:                a.pool,
			Cacher:              a.cacher,
			Cfg:                 a.config,
			Logger:              a.logger,
			InstitutionEventBus: a.institutionEventBus,
			Publisher:           broker.NewPublisher(a.rabbitMQConn, a.logger),
		},
		&leaderboard.LeaderBoardHandler{
			DB:     db,
			Cacher: a.cacher,
			Cfg:    a.config,
			Logger: a.logger,
		},
		&activity.ActivityHandler{
			DB:     db,
			Cacher: a.cacher,
			Cfg:    a.config,
			Logger: a.logger,
		},
		&streak.StreakHandler{
			DB:                   db,
			Cacher:               a.cacher,
			Cfg:                  a.config,
			Logger:               a.logger,
			NotificationEventBus: a.notificationEventBus,
		},
		&device.DeviceHandler{
			DB:         db,
			Cacher:     a.cacher,
			Cfg:        a.config,
			GeoLocator: a.geoIPLocator,
			Logger:     a.logger,
		},
		&oauth.OAuthBrokerHandler{
			DB:        db,
			Cacher:    a.cacher,
			Cfg:       a.config,
			Logger:    a.logger,
			Registry:  a.oauthRegistry,
			Exchanger: a.tokenExchanger,
			Sealer:    a.tokenSealer,
		},
		&oauth.OAuthScopeHandler{
			DB:        db,
			Cacher:    a.cacher,
			Cfg:       a.config,
			Logger:    a.logger,
			Registry:  a.oauthRegistry,
			Exchanger: a.tokenExchanger,
			Sealer:    a.tokenSealer,
		},
	}

	return buildRouter(verisafeHandlers)
}

// buildRouter is the part of route loading that depends only on the handler
// set, split out from loadRoutes so a test can drive it with zero-valued
// handlers and assert the full route table without a live pool, Redis or
// RabbitMQ. Registration only closes over its dependencies, so nil ones are
// fine here — nothing is dereferenced until a request arrives.
func buildRouter(handlers []VerisafeHandler) *http.ServeMux {
	router := http.NewServeMux()
	registerAll(router, handlers)
	return router
}

// registerAll is every registration the service performs, expressed against
// core.Router so a test can replay it into a recorder and read back the
// complete route table.
func registerAll(router core.Router, handlers []VerisafeHandler) {
	for _, handler := range handlers {
		handler.RegisterHandlers(router)
	}

	router.HandleFunc("GET /ping", health.PingHandler)
	router.Handle("GET /docs/", httpSwagger.WrapHandler)
}
