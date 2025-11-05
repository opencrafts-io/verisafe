package app

import (
	"net/http"

	"github.com/opencrafts-io/verisafe/internal/auth"
	"github.com/opencrafts-io/verisafe/internal/handlers"
)

func (a *App) loadRoutes() http.Handler {
	router := http.NewServeMux()

	auth, err := auth.NewAuthenticator(a.config, a.logger)
	if err != nil {
		a.logger.Error("Failed to initialize authenticator", "error", err)
		// Return a simple error handler if auth initialization fails
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
		})
	}
	accountHandler := handlers.AccountHandler{Logger: a.logger, Cfg: a.config}
	serviceTokenHandler := handlers.ServiceTokenHandler{Logger: a.logger, Cfg: a.config}
	socialHandler := handlers.SocialHandler{Logger: a.logger}
	roleHandler := handlers.RoleHandler{Logger: a.logger}
	permHandler := handlers.PermissionHandler{Logger: a.logger}
	institutionHandler := handlers.InstitutionHandler{Logger: a.logger}
	leaderboardHandler := handlers.LeaderBoardHandler{Logger: a.logger}

	// ping handler
	router.HandleFunc("GET /ping", handlers.PingHandler)

	// Auth handlers
	auth.RegisterRoutes(router)
	accountHandler.RegisterHandlers(router)
	serviceTokenHandler.RegisterHandlers(router)
	socialHandler.RegisterRoutes(a.config, router)
	// Roles
	roleHandler.RegisterRoutes(a.config, router)
	// Permissions
	permHandler.RegisterRoutes(a.config, router)
	institutionHandler.RegisterInstitutionHadlers(a.config, router)
	leaderboardHandler.RegisterLeaderBoardHandlers(a.config, router)
	return router
}
