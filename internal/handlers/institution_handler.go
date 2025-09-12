package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/opencrafts-io/verisafe/internal/config"
	"github.com/opencrafts-io/verisafe/internal/middleware"
	"github.com/opencrafts-io/verisafe/internal/repository"
)

type InstitutionHandler struct {
	Logger *slog.Logger
}

func (ih *InstitutionHandler) RegisterInstitutionHadlers(cfg *config.Config, router *http.ServeMux) {
	// Register endpoints using the new pattern
	router.Handle("POST /institutions/register", middleware.CreateStack(
		middleware.IsAuthenticated(cfg, ih.Logger),
		middleware.HasPermission([]string{"create:institutions:any"}),
	)(http.HandlerFunc(ih.RegisterInstitution)))

	router.Handle("PATCH /institutions/update/{id}",
		middleware.CreateStack(
			middleware.IsAuthenticated(cfg, ih.Logger),
			middleware.HasPermission([]string{"update:institutions:any"}),
		)(http.HandlerFunc(ih.UpdateInstitutionDetails)))

	router.Handle("GET /institutions/find/{id}",
		middleware.CreateStack(
			middleware.IsAuthenticated(cfg, ih.Logger),
		)(http.HandlerFunc(ih.GetInstitutionByID)))

	router.Handle("GET /institutions/all",
		middleware.CreateStack(
			middleware.IsAuthenticated(cfg, ih.Logger),
			middleware.HasPermission([]string{"list:institutions:any"}),
		)(http.HandlerFunc(ih.GetAllInstitutions)))

	router.Handle("GET /institutions/search",
		middleware.CreateStack(
			middleware.IsAuthenticated(cfg, ih.Logger),
		)(http.HandlerFunc(ih.SearchInstitutions)))

	router.Handle("DELETE /institutions/delete/{id}",
		middleware.CreateStack(
			middleware.IsAuthenticated(cfg, ih.Logger),
			middleware.HasPermission([]string{"delete:institutions:any"}),
		)(http.HandlerFunc(ih.DeleteInstitution)))

	// Institution account management
	// TODO: (erick) Add fine permissions for both admin and the user in question
	router.Handle("POST /institutions/account",
		middleware.CreateStack(
			middleware.IsAuthenticated(cfg, ih.Logger),
		)(http.HandlerFunc(ih.AddAcountInstitution)))

	router.Handle("DELETE /institutions/account",
		middleware.CreateStack(
			middleware.IsAuthenticated(cfg, ih.Logger),
		)(http.HandlerFunc(ih.RemoveAccountInstitution)))

	router.Handle("GET /institutions/for-account",
		middleware.CreateStack(
			middleware.IsAuthenticated(cfg, ih.Logger),
		)(http.HandlerFunc((ih.ListInstitutionForAccount))))

	router.Handle("GET /institutions/accounts",
		middleware.CreateStack(
			middleware.IsAuthenticated(cfg, ih.Logger),
		)(http.HandlerFunc(ih.ListAccountsForInstitution)))
}

// POST /institutions/register
func (ih *InstitutionHandler) RegisterInstitution(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		ih.Logger.Error("Error while processing request", slog.Any("error", err))
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	tx, _ := conn.Begin(r.Context())
	defer tx.Rollback(r.Context())
	repo := repository.New(tx)

	var req repository.CreateInstitutionParams
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		ih.Logger.Error("Failed to parse request body", slog.Any("error", err))
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	created, err := repo.CreateInstitution(r.Context(), req)
	if err != nil {
		ih.Logger.Error("Failed to create institution", slog.Any("error", err))
		http.Error(w, `{"error":"failed to create institution"}`, http.StatusInternalServerError)
		return
	}

	if err = tx.Commit(r.Context()); err != nil {
		ih.Logger.Error("Error committing transaction", slog.Any("error", err))
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(created)
}

// PATCH /institutions/update/{id}
func (ih *InstitutionHandler) UpdateInstitutionDetails(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		ih.Logger.Error("Error while processing request", slog.Any("error", err))
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	tx, _ := conn.Begin(r.Context())
	defer tx.Rollback(r.Context())
	repo := repository.New(tx)

	// Extract ID from URL
	idStr := r.PathValue("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, `{"error":"invalid institution id"}`, http.StatusBadRequest)
		return
	}

	var req repository.UpdateInstitutionParams
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		ih.Logger.Error("Failed to parse request body", slog.Any("error", err))
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	req.InstitutionID = int32(id)

	updated, err := repo.UpdateInstitution(r.Context(), req)
	if err != nil {
		ih.Logger.Error("Failed to update institution", slog.Any("error", err))
		http.Error(w, `{"error":"failed to update institution"}`, http.StatusInternalServerError)
		return
	}

	if err = tx.Commit(r.Context()); err != nil {
		ih.Logger.Error("Error committing transaction", slog.Any("error", err))
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(updated)
}

// GET /institutions/find/{id}
func (ih *InstitutionHandler) GetInstitutionByID(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		ih.Logger.Error("Error while processing request", slog.Any("error", err))
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}
	repo := repository.New(conn)

	idStr := r.PathValue("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, `{"error":"invalid institution id"}`, http.StatusBadRequest)
		return
	}

	institution, err := repo.GetInstitution(r.Context(), int32(id))
	if err != nil {
		ih.Logger.Error("Failed to get institution", slog.Any("error", err))
		http.Error(w, `{"error":"institution not found"}`, http.StatusNotFound)
		return
	}

	json.NewEncoder(w).Encode(institution)
}

// GET /institutions/all
func (ih *InstitutionHandler) GetAllInstitutions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		ih.Logger.Error("Error while processing request", slog.Any("error", err))
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}
	repo := repository.New(conn)

	p := middleware.GetPagination(r.Context())

	institutions, err := repo.ListInstitutions(r.Context(), repository.ListInstitutionsParams{
		Limit:  int32(p.Limit),
		Offset: int32(p.Offset),
	})

	if err != nil {
		ih.Logger.Error("Failed to list institutions", slog.Any("error", err))
		http.Error(w, `{"error":"failed to fetch institutions"}`, http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(institutions)
}

// DELETE /institutions/delete/{id}
func (ih *InstitutionHandler) DeleteInstitution(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		ih.Logger.Error("Error while processing request", slog.Any("error", err))
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	tx, _ := conn.Begin(r.Context())
	defer tx.Rollback(r.Context())
	repo := repository.New(tx)

	idStr := r.PathValue("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, `{"error":"invalid institution id"}`, http.StatusBadRequest)
		return
	}

	if err := repo.DeleteInstitution(r.Context(), int32(id)); err != nil {
		ih.Logger.Error("Failed to delete institution", slog.Any("error", err))
		http.Error(w, `{"error":"failed to delete institution"}`, http.StatusInternalServerError)
		return
	}

	if err = tx.Commit(r.Context()); err != nil {
		ih.Logger.Error("Error committing transaction", slog.Any("error", err))
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (ih *InstitutionHandler) SearchInstitutions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		ih.Logger.Error("DB connection missing", slog.Any("error", err))
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}
	repo := repository.New(conn)

	// Extract query param `q`
	q := r.URL.Query().Get("q")
	if q == "" {
		http.Error(w, `{"error":"missing search query param 'q'"}`, http.StatusBadRequest)
		return
	}

	// Get pagination values from middleware
	p := middleware.GetPagination(r.Context())

	institutions, err := repo.SearchInstitutionsByName(r.Context(), repository.SearchInstitutionsByNameParams{
		Name:   q,
		Limit:  int32(p.Limit),
		Offset: int32(p.Offset),
	})
	if err != nil {
		ih.Logger.Error("Search failed", slog.Any("error", err))
		http.Error(w, `{"error":"failed to search institutions"}`, http.StatusInternalServerError)
		return
	}

	if err := json.NewEncoder(w).Encode(institutions); err != nil {
		ih.Logger.Error("Failed to encode response", slog.Any("error", err))
	}
}

// Links an account to institution
func (ih *InstitutionHandler) AddAcountInstitution(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		ih.Logger.Error("Error while processing request", slog.Any("error", err))
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	tx, _ := conn.Begin(r.Context())
	defer tx.Rollback(r.Context())
	repo := repository.New(tx)

	var req repository.AddAccountInstitutionParams
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		ih.Logger.Error("Failed to parse request body", slog.Any("error", err))
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	created, err := repo.AddAccountInstitution(r.Context(), req)
	if err != nil {
		ih.Logger.Error("Failed to create institution", slog.Any("error", err))
		http.Error(w, `{"error":"failed to link you to that organization"}`, http.StatusInternalServerError)
		return
	}

	if err = tx.Commit(r.Context()); err != nil {
		ih.Logger.Error("Error committing transaction", slog.Any("error", err))
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(created)
}

func (ih *InstitutionHandler) ListInstitutionForAccount(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		ih.Logger.Error("Error while processing request", slog.Any("error", err))
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}
	repo := repository.New(conn)

	// Extract query param `q`
	q := r.URL.Query().Get("account_id")
	if q == "" {
		http.Error(w, `{"error":"missing search query param 'q'"}`, http.StatusBadRequest)
		return
	}

	// parse the uuid
	id, err := uuid.Parse(q)
	if err != nil {
		http.Error(w, `{"error":"Could not parse the uuid parameter"}`, http.StatusBadRequest)
		return
	}

	p := middleware.GetPagination(r.Context())
	institutions, err := repo.ListInstitutionsForAccount(r.Context(), repository.ListInstitutionsForAccountParams{
		AccountID: id,
		Limit:     int32(p.Limit),
		Offset:    int32(p.Offset),
	})

	if err != nil {
		ih.Logger.Error("Failed to list institutions", slog.Any("error", err))
		http.Error(w, `{"error":"failed to fetch institutions"}`, http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(institutions)
}

// Get accounts that are registered to an institution
func (ih *InstitutionHandler) ListAccountsForInstitution(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		ih.Logger.Error("Error while processing request", slog.Any("error", err))
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}
	repo := repository.New(conn)

	// Extract query param `q`
	q := r.URL.Query().Get("institution_id")
	if q == "" {
		http.Error(w, `{"error":"missing search query param 'q'"}`, http.StatusBadRequest)
		return
	}

	// parse the uuid
	id, err := strconv.Atoi(q)
	if err != nil {
		http.Error(w, `{"error":"Could not parse the institution id parameter"}`, http.StatusBadRequest)
		return
	}

	p := middleware.GetPagination(r.Context())
	institutions, err := repo.ListAccountsForInstitution(r.Context(), repository.ListAccountsForInstitutionParams{
		InstitutionID: int32(id),
		Limit:         int32(p.Limit),
		Offset:        int32(p.Offset),
	})

	if err != nil {
		ih.Logger.Error("Failed to list institutions", slog.Any("error", err))
		http.Error(w, `{"error":"failed to fetch institutions"}`, http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(institutions)
}

// Remove an account from an institution
func (ih *InstitutionHandler) RemoveAccountInstitution(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		ih.Logger.Error("Error while processing request", slog.Any("error", err))
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	tx, _ := conn.Begin(r.Context())
	defer tx.Rollback(r.Context())
	repo := repository.New(tx)

	var req repository.RemoveAccountInstitutionParams
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		ih.Logger.Error("Failed to parse request body", slog.Any("error", err))
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	err = repo.RemoveAccountInstitution(r.Context(), req)
	if err != nil {
		ih.Logger.Error("Failed to create institution", slog.Any("error", err))
		http.Error(w, `{"error":"failed to create institution"}`, http.StatusInternalServerError)
		return
	}

	if err = tx.Commit(r.Context()); err != nil {
		ih.Logger.Error("Error committing transaction", slog.Any("error", err))
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]any{"message": "Successfully removed from institution"})
}
