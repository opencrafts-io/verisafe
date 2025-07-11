package app

import (
	"net/http"

	"github.com/opencrafts-io/verisafe/internal/auth"
	"github.com/opencrafts-io/verisafe/internal/handlers"
	"github.com/opencrafts-io/verisafe/internal/middleware"
)

func (a *App) loadRoutes() http.Handler {
	router := http.NewServeMux()

	auth := auth.NewAuthenticator(a.config, a.logger)
	roleHandler := handlers.RoleHandler{Logger: a.logger}
	permHandler := handlers.PermissionHandler{Logger: a.logger}

	// ping handler
	router.HandleFunc("GET /ping", handlers.PingHandler)

	// Auth handlers
	router.HandleFunc("GET /auth/{provider}", auth.LoginHandler)
	router.HandleFunc("GET /auth/{provider}/callback", auth.CallbackHandler)
	router.HandleFunc("GET /auth/{provider}/logout", auth.LogoutHandler)

	// roles
	router.HandleFunc("POST /roles/create", roleHandler.CreateRole)
	router.Handle("GET /roles", middleware.PaginationMiddleware(10, 100)(
		http.HandlerFunc(roleHandler.GetAllRoles),
	))
	router.HandleFunc("GET /roles/{id}", roleHandler.GetRoleByID)
	router.HandleFunc("GET /roles/user/{id}", roleHandler.GetAllUserRoles)
	router.HandleFunc("GET /roles/permissions/{id}", roleHandler.GetRolePermissions)
	router.HandleFunc("PATCH /roles/{id}", roleHandler.UpdateRole)
	router.HandleFunc("GET /roles/assign/{user_id}/{role_id}", roleHandler.AssignUserRole)
	router.HandleFunc("DELETE /roles/revoke/{user_id}/{role_id}", roleHandler.RevokeUserRole)

	// Permissions
	router.HandleFunc("POST /permissions/create", permHandler.CreatePermission)
	router.HandleFunc("GET /permissions/{id}", permHandler.GetPermissionByID)
	router.Handle("GET /permissions", middleware.PaginationMiddleware(10, 100)(
		http.HandlerFunc(permHandler.GetAllPermissions),
	))
	router.HandleFunc("GET /permissions/user/{id}", permHandler.GetAllUserPermissions)
	router.HandleFunc("PATCH /permissions/", permHandler.UpdatePermission)
	router.HandleFunc("GET /permissions/assign/{perm_id}/{role_id}", permHandler.AssignRolePermission)
	router.HandleFunc("DELETE /permissions/revoke/{perm_id}/{role_id}", permHandler.RevokeRolePermission)

	return router
}
