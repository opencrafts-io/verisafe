package app

import (
	"net/http"

	_ "github.com/opencrafts-io/verisafe/docs"
	"github.com/opencrafts-io/verisafe/internal/auth"
	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/handlers"
	httpSwagger "github.com/swaggo/http-swagger"
)

type VerisafeHandler interface {
	RegisterHandlers(router *http.ServeMux)
}

func (a *App) loadRoutes() http.Handler {
	router := http.NewServeMux()

	authenticator, err := auth.NewAuthenticator(
		a.config,
		a.logger,
		auth.GenerateAppleClientSecret,
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
		),
		&handlers.AccountHandler{
			DB:           db,
			Cacher:       a.cacher,
			Logger:       a.logger,
			UserEventBus: a.userEventBus,
			Cfg:          a.config,
		},
		&handlers.ServiceTokenHandler{
			DB:     db,
			Cacher: a.cacher,
			Logger: a.logger,
			Cfg:    a.config,
		},
		&handlers.SocialHandler{
			DB:     db,
			Cacher: a.cacher,
			Cfg:    a.config,
			Logger: a.logger,
		},
		&handlers.RoleHandler{
			DB:     db,
			Cacher: a.cacher,
			Cfg:    a.config,
			Logger: a.logger,
		},
		&handlers.PermissionHandler{
			DB:     db,
			Cacher: a.cacher,
			Cfg:    a.config,
			Logger: a.logger,
		},
		&handlers.InstitutionHandler{
			DB:                  db,
			Cacher:              a.cacher,
			Cfg:                 a.config,
			Logger:              a.logger,
			InstitutionEventBus: a.institutionEventBus,
		},
		&handlers.LeaderBoardHandler{
			DB:     db,
			Cacher: a.cacher,
			Cfg:    a.config,
			Logger: a.logger,
		},
		&handlers.ActivityHandler{
			DB:     db,
			Cacher: a.cacher,
			Cfg:    a.config,
			Logger: a.logger,
		},
		&handlers.StreakHandler{
			DB:                   db,
			Cacher:               a.cacher,
			Cfg:                  a.config,
			Logger:               a.logger,
			NotificationEventBus: a.notificationEventBus,
		},
		&handlers.DeviceHandler{
			DB:         db,
			Cacher:     a.cacher,
			Cfg:        a.config,
			GeoLocator: a.geoIPLocator,
			Logger:     a.logger,
		},
	}

	for _, handler := range verisafeHandlers {
		handler.RegisterHandlers(router)
	}

	router.HandleFunc("GET /ping", handlers.PingHandler)
	router.Handle("GET /docs/", httpSwagger.WrapHandler)

	return router
}
