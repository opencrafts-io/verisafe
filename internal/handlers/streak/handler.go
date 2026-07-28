package streak

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/opencrafts-io/verisafe/internal/config"
	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/eventbus"
	"github.com/opencrafts-io/verisafe/internal/middleware"
	"github.com/opencrafts-io/verisafe/internal/middleware/pagination"
	"github.com/opencrafts-io/verisafe/internal/repository"
)

type StreakHandler struct {
	Cacher               core.Cacher
	DB                   core.IDBProvider
	Cfg                  *config.Config
	Logger               *slog.Logger
	NotificationEventBus eventbus.NotificationPublisher
}

func (sh *StreakHandler) RegisterHandlers(router core.Router) {
	router.Handle("POST /users/activity/complete", middleware.CreateStack(
		middleware.IsAuthenticated(sh.Cfg, sh.DB, sh.Cacher, sh.Logger),
	)(http.HandlerFunc(sh.RecordUserActivity)))
	router.Handle("POST /streaks/milestone/create", middleware.CreateStack(
		middleware.IsAuthenticated(sh.Cfg, sh.DB, sh.Cacher, sh.Logger),
	)(http.HandlerFunc(sh.CreateStreakMilestone)))
	router.Handle("GET /streaks/milestone/active", middleware.CreateStack(
		middleware.IsAuthenticated(sh.Cfg, sh.DB, sh.Cacher, sh.Logger),
	)(http.HandlerFunc(sh.GetAllActiveStreakAchievements)))
	router.Handle("DELETE /streaks/milestone/{id}", middleware.CreateStack(
		middleware.IsAuthenticated(sh.Cfg, sh.DB, sh.Cacher, sh.Logger),
	)(http.HandlerFunc(sh.DeleteStreakMilestone)))
}

// RecordUserActivity godoc
//
// @Summary      Record a completed activity for the authenticated user
// @Description  The request body's account_id must match the caller's own subject.
// @Tags         streaks
// @Accept       json
// @Produce      json
// @Param        request  body      repository.RecordActivityCompletionParams  true  "account_id must be the caller's own"
// @Success      200  {object}  map[string]any  "Confirmation message"
// @Failure      400  {object}  core.APIError  "Invalid request body"
// @Failure      403  {object}  core.APIError  "account_id does not match the caller"
// @Failure      500  {object}  core.APIError  "Failed to record completion"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /users/activity/complete [post]
func (sh *StreakHandler) RecordUserActivity(
	w http.ResponseWriter,
	r *http.Request,
) {
	w.Header().Set("Content-Type", "application/json")
	requestBody := repository.RecordActivityCompletionParams{}

	if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
		sh.Logger.Error("Failed to parse request body", slog.Any("error", err))
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Please check your request body and try again",
		})
		return
	}

	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok || requestBody.AccountID.String() != claims.Subject {
		core.WriteError(w, http.StatusForbidden, "you can only record activity completions for your own account")
		return
	}

	conn, err := sh.DB.Acquire(r.Context())
	if err != nil {
		sh.Logger.Error(
			"Error while processing request",
			slog.Any("error", err),
		)
		http.Error(
			w,
			`{"error":"internal server error"}`,
			http.StatusInternalServerError,
		)
		return
	}
	defer conn.Release()

	tx, err := conn.Begin(r.Context())
	if err != nil {
		sh.Logger.Error("Failed to start transaction", slog.Any("error", err))
		http.Error(
			w,
			`{"error":"Cannot process your request at the moment"}`,
			http.StatusInternalServerError,
		)
		return
	}
	defer tx.Rollback(r.Context())
	repo := repository.New(tx)

	completed, err := repo.RecordActivityCompletion(r.Context(), requestBody)
	if err != nil {
		sh.Logger.Error(
			"Failed to record user activity",
			slog.Any("error", err),
			slog.Any("activity", requestBody),
		)
		http.Error(
			w,
			`{"error":"Cannot process your request at the moment"}`,
			http.StatusInternalServerError,
		)
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		sh.Logger.Error(
			"Error while committing transaction",
			slog.Any("error", err),
			slog.Any("activity", requestBody),
		)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}

	go sh.sendActivityCompletionNotification(
		requestBody.AccountID.String(),
		&completed,
	)
	json.NewEncoder(w).
		Encode(map[string]any{"message": "Activity recorded successfully!"})
}

// CreateStreakMilestone godoc
//
// @Summary      Create a streak milestone
// @Description  Note: this route only checks IsAuthenticated today, with no admin-style permission gate (see ADR 0006 for the planned fix).
// @Tags         streaks
// @Accept       json
// @Produce      json
// @Param        request  body      repository.CreateStreakMilestoneParams  true  "Milestone to create"
// @Success      201  {object}  repository.StreakMilestone
// @Failure      400  {object}  core.APIError  "Invalid request body"
// @Failure      500  {object}  core.APIError  "Failed to create milestone"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /streaks/milestone/create [post]
func (sh *StreakHandler) CreateStreakMilestone(
	w http.ResponseWriter,
	r *http.Request,
) {
	w.Header().Set("Content-Type", "application/json")
	requestBody := repository.CreateStreakMilestoneParams{}

	if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
		sh.Logger.Error("Failed to parse request body", slog.Any("error", err))
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Please check your request body and try again",
		})
		return
	}

	conn, err := sh.DB.Acquire(r.Context())
	if err != nil {
		sh.Logger.Error(
			"Error while processing request",
			slog.Any("error", err),
		)
		http.Error(
			w,
			`{"error":"internal server error"}`,
			http.StatusInternalServerError,
		)
		return
	}
	defer conn.Release()

	tx, err := conn.Begin(r.Context())
	if err != nil {
		sh.Logger.Error("Failed to start transaction", slog.Any("error", err))
		http.Error(
			w,
			`{"error":"Cannot process your request at the moment"}`,
			http.StatusInternalServerError,
		)
		return
	}
	defer tx.Rollback(r.Context())
	repo := repository.New(tx)

	milestone, err := repo.CreateStreakMilestone(r.Context(), requestBody)
	if err != nil {
		sh.Logger.Error(
			"Failed to create streak milestone",
			slog.Any("error", err),
			slog.Any("milestone", requestBody),
		)
		http.Error(
			w,
			`{"error":"Cannot process your request at the moment"}`,
			http.StatusInternalServerError,
		)
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		sh.Logger.Error(
			"Error while committing transaction",
			slog.Any("error", err),
			slog.Any("activity", requestBody),
		)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(milestone)
}

// GetAllActiveStreakAchievements godoc
//
// @Summary      List active streak milestones
// @Description  Returns the streak milestones users can currently reach.
// @Description  Paginated with page/page_size; the response is a
// @Description  count/next/previous/results envelope.
// @Tags         streaks
// @Produce      json
// @Param        page       query  int  false  "Page number (default 1)"
// @Param        page_size  query  int  false  "Page size (default 10, max 100)"
// @Success      200  {object}  pagination.PaginatedResponse
// @Failure      500  {object}  core.APIError  "Failed to fetch milestones"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /streaks/milestone/active [get]
func (sh *StreakHandler) GetAllActiveStreakAchievements(
	w http.ResponseWriter,
	r *http.Request,
) {
	w.Header().Set("Content-Type", "application/json")
	conn, err := sh.DB.Acquire(r.Context())
	if err != nil {
		sh.Logger.Error(
			"Error while processing request",
			slog.Any("error", err),
		)
		http.Error(
			w,
			`{"error":"internal server error"}`,
			http.StatusInternalServerError,
		)
		return
	}
	defer conn.Release()

	tx, err := conn.Begin(r.Context())
	if err != nil {
		sh.Logger.Error("Failed to start transaction", slog.Any("error", err))
		http.Error(
			w,
			`{"error":"Cannot process your request at the moment"}`,
			http.StatusInternalServerError,
		)
		return
	}

	defer tx.Rollback(r.Context())
	repo := repository.New(tx)

	// Parse pagination params
	pageParams := pagination.ParsePageParams(r)

	totalCount, err := repo.GetAllActiveStreakMilestoneCount(r.Context())
	if err != nil {
		sh.Logger.Error(
			"Failed to get all active streak milestones count",
			slog.Any("error", err),
		)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "We couldn't fetch active streak milestone count at the moment",
		})
		return
	}

	active := true

	milestones, err := repo.GetAllStreaksMilestoneByActive(
		r.Context(),
		repository.GetAllStreaksMilestoneByActiveParams{
			Limit:    int32(pageParams.PageSize),
			Offset:   int32(pageParams.Offset),
			IsActive: &active,
		},
	)
	if err != nil {
		sh.Logger.Error(
			"Failed to retrieve active streak milestones",
			slog.Any("error", err),
		)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "We couldn't provide active streak milestones at the moment",
		})
		return
	}

	response := pagination.BuildPaginatedResponse(
		r,
		totalCount,
		milestones,
		pageParams,
	)
	json.NewEncoder(w).Encode(response)
}

// DeleteStreakMilestone godoc
//
// @Summary      Delete a streak milestone
// @Description  Note: this route only checks IsAuthenticated today, with no admin-style permission gate (see ADR 0006 for the planned fix).
// @Tags         streaks
// @Produce      json
// @Param        id  path  string  true  "Milestone ID"
// @Success      200  {object}  map[string]any  "Confirmation message"
// @Failure      400  {object}  core.APIError  "Invalid id"
// @Failure      500  {object}  core.APIError  "Failed to delete milestone"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /streaks/milestone/{id} [delete]
func (sh *StreakHandler) DeleteStreakMilestone(
	w http.ResponseWriter,
	r *http.Request,
) {
	rawID := r.PathValue("id")
	id, err := uuid.Parse(rawID)
	if err != nil {
		sh.Logger.Error(
			"Failed to parse uuid from path",
			slog.Any("error", err),
		)
		http.Error(
			w,
			`{"error":"Please check your request body and try again"}`,
			http.StatusBadRequest,
		)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	conn, err := sh.DB.Acquire(r.Context())
	if err != nil {
		sh.Logger.Error(
			"Error while processing request",
			slog.Any("error", err),
		)
		http.Error(
			w,
			`{"error":"internal server error"}`,
			http.StatusInternalServerError,
		)
		return
	}
	defer conn.Release()

	tx, err := conn.Begin(r.Context())
	if err != nil {
		sh.Logger.Error("Failed to start transaction", slog.Any("error", err))
		http.Error(
			w,
			`{"error":"Cannot process your request at the moment"}`,
			http.StatusInternalServerError,
		)
		return
	}
	defer tx.Rollback(r.Context())
	repo := repository.New(tx)

	err = repo.DeleteStreakMilestoneByID(r.Context(), id)
	if err != nil {
		sh.Logger.Error(
			"Failed to delete streak milestone",
			slog.Any("error", err),
		)
		http.Error(
			w,
			`{"error":"Cannot process your request at the moment"}`,
			http.StatusInternalServerError,
		)
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		sh.Logger.Error(
			"Error while committing transaction",
			slog.Any("error", err),
		)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).
		Encode(map[string]any{"message": "streak milestone deleted successfully"})
}

func (sh *StreakHandler) sendActivityCompletionNotification(
	accountID string,
	result *repository.RecordActivityCompletionRow,
) {
	// Create notification content based on result
	heading := "🎉 Activity Completed!"
	var content string
	var buttons []eventbus.NotificationButton

	// Base points notification
	content = fmt.Sprintf("You earned %d vibepoints!", result.PointsEarned)

	// Add streak information if applicable
	if result.CurrentStreak > 0 {
		content += fmt.Sprintf("\n🔥 Streak: %d days", result.CurrentStreak)
	}

	// Add milestone bonus if achieved
	if result.MilestoneAchieved {
		content += fmt.Sprintf(
			"\n⭐ Milestone bonus: +%d points!",
			result.MilestoneBonus,
		)
		buttons = append(buttons, eventbus.NotificationButton{
			ID:   "view-achievements",
			Text: "View Achievements",
			Icon: "ic_trophy",
		})
	}

	// Add action button
	buttons = append(buttons, eventbus.NotificationButton{
		ID:   "view-profile",
		Text: "View Profile",
		Icon: "ic_profile",
	})

	notification := eventbus.NotificationPayload{
		AppID: "88ca0bb7-c0d7-4e36-b9e6-ea0e29213593",
		Headings: map[string]string{
			"en": heading,
		},
		Contents: map[string]string{
			"en": content,
		},
		TargetUserID: accountID,
		Subtitle: map[string]string{
			"en": "Keep up the good work!",
		},
		AndroidChannelID: "60023d0b-dcd4-41ae-8e58-7eabbf382c8c",
		IosSound:         "default",
		SmallIcon:        "ic_notification",
		URL:              "https://opencrafts.io/profile",
		Buttons:          buttons,
	}

	// Send notification (with timeout to prevent blocking)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sh.NotificationEventBus.PublishPushNotificationRequested(
		ctx,
		notification,
		eventbus.GenerateRequestID(),
	)
}
