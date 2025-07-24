package app

import (
	"net/http"

	"github.com/opencrafts-io/verisafe/internal/auth"
	"github.com/opencrafts-io/verisafe/internal/handlers"
)

func (a *App) loadRoutes() http.Handler {
	router := http.NewServeMux()

	auth := auth.NewAuthenticator(a.config, a.logger)
	accountHandler := handlers.AccountHandler{Logger: a.logger, Cfg: a.config}
	socialHandler := handlers.SocialHandler{Logger: a.logger}
	roleHandler := handlers.RoleHandler{Logger: a.logger}
	permHandler := handlers.PermissionHandler{Logger: a.logger}

	// ping handler
	router.HandleFunc("GET /ping", handlers.PingHandler)

	// Auth handlers
	auth.RegisterRoutes(router)
	accountHandler.RegisterHandlers(router)
	socialHandler.RegisterRoutes(a.config, router)
	// Roles
	roleHandler.RegisterRoutes(a.config, router)
	// Permissions
	permHandler.RegisterRoutes(a.config, router)
	return router
}
