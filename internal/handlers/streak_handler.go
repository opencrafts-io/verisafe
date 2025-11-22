package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/opencrafts-io/verisafe/internal/config"
	"github.com/opencrafts-io/verisafe/internal/eventbus"
	"github.com/opencrafts-io/verisafe/internal/middleware"
	"github.com/opencrafts-io/verisafe/internal/middleware/pagination"
	"github.com/opencrafts-io/verisafe/internal/repository"
)

type StreakHandler struct {
	Logger               *slog.Logger
	NotificationEventBus *eventbus.NotificationEventBus
}

func (sh *StreakHandler) RegisterRoutes(cfg *config.Config, router *http.ServeMux) {
	router.Handle("POST /users/activity/complete", middleware.CreateStack(
		middleware.IsAuthenticated(cfg, sh.Logger),
	)(http.HandlerFunc(sh.RecordUserActivity)))
	router.Handle("POST /streaks/milestone/create", middleware.CreateStack(
		middleware.IsAuthenticated(cfg, sh.Logger),
	)(http.HandlerFunc(sh.CreateStreakMilestone)))
	router.Handle("GET /streaks/milestone/active", middleware.CreateStack(
		middleware.IsAuthenticated(cfg, sh.Logger),
	)(http.HandlerFunc(sh.GetAllActiveStreakAchievements)))
	router.Handle("DELETE /streaks/milestone/{id}", middleware.CreateStack(
		middleware.IsAuthenticated(cfg, sh.Logger),
	)(http.HandlerFunc(sh.DeleteStreakMilestone)))

}
func (sh *StreakHandler) RecordUserActivity(w http.ResponseWriter, r *http.Request) {
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

	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		sh.Logger.Error("Error while processing request", slog.Any("error", err))
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	tx, err := conn.Begin(r.Context())
	if err != nil {
		sh.Logger.Error("Failed to start transaction", slog.Any("error", err))
		http.Error(w, `{"error":"Cannot process your request at the moment"}`, http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(r.Context())
	repo := repository.New(tx)

	completed, err := repo.RecordActivityCompletion(r.Context(), requestBody)
	if err != nil {
		sh.Logger.Error("Failed to record user activity", slog.Any("error", err), slog.Any("activity", requestBody))
		http.Error(w, `{"error":"Cannot process your request at the moment"}`, http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		sh.Logger.Error("Error while committing transaction", slog.Any("error", err), slog.Any("activity", requestBody))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}

	go sh.sendActivityCompletionNotification(requestBody.AccountID.String(), &completed)
	json.NewEncoder(w).Encode(map[string]any{"message": "Activity recorded successfully!"})
}

func (sh *StreakHandler) CreateStreakMilestone(w http.ResponseWriter, r *http.Request) {
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

	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		sh.Logger.Error("Error while processing request", slog.Any("error", err))
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	tx, err := conn.Begin(r.Context())
	if err != nil {
		sh.Logger.Error("Failed to start transaction", slog.Any("error", err))
		http.Error(w, `{"error":"Cannot process your request at the moment"}`, http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(r.Context())
	repo := repository.New(tx)

	milestone, err := repo.CreateStreakMilestone(r.Context(), requestBody)
	if err != nil {
		sh.Logger.Error("Failed to create streak milestone", slog.Any("error", err), slog.Any("milestone", requestBody))
		http.Error(w, `{"error":"Cannot process your request at the moment"}`, http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		sh.Logger.Error("Error while committing transaction", slog.Any("error", err), slog.Any("activity", requestBody))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(milestone)
}

func (sh *StreakHandler) GetAllActiveStreakAchievements(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		sh.Logger.Error("Error while processing request", slog.Any("error", err))
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	tx, err := conn.Begin(r.Context())
	if err != nil {
		sh.Logger.Error("Failed to start transaction", slog.Any("error", err))
		http.Error(w, `{"error":"Cannot process your request at the moment"}`, http.StatusInternalServerError)
		return
	}

	defer tx.Rollback(r.Context())
	repo := repository.New(tx)

	// Parse pagination params
	pageParams := pagination.ParsePageParams(r)

	totalCount, err := repo.GetAllActiveStreakMilestoneCount(r.Context())
	if err != nil {
		sh.Logger.Error("Failed to get all active streak milestones count", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "We couldn't fetch active streak milestone count at the moment",
		})
		return
	}

	active := true

	milestones, err := repo.GetAllStreaksMilestoneByActive(r.Context(), repository.GetAllStreaksMilestoneByActiveParams{
		Limit:    int32(pageParams.PageSize),
		Offset:   int32(pageParams.Offset),
		IsActive: &active,
	})

	if err != nil {
		sh.Logger.Error("Failed to retrieve active streak milestones", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "We couldn't provide active streak milestones at the moment",
		})
		return
	}

	response := pagination.BuildPaginatedResponse(r, totalCount, milestones, pageParams)
	json.NewEncoder(w).Encode(response)

}

func (sh *StreakHandler) DeleteStreakMilestone(w http.ResponseWriter, r *http.Request) {
	rawID := r.PathValue("id")
	id, err := uuid.Parse(rawID)
	if err != nil {
		sh.Logger.Error("Failed to parse uuid from path", slog.Any("error", err))
		http.Error(w, `{"error":"Please check your request body and try again"}`, http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		sh.Logger.Error("Error while processing request", slog.Any("error", err))
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	tx, err := conn.Begin(r.Context())
	if err != nil {
		sh.Logger.Error("Failed to start transaction", slog.Any("error", err))
		http.Error(w, `{"error":"Cannot process your request at the moment"}`, http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(r.Context())
	repo := repository.New(tx)

	err = repo.DeleteStreakMilestoneByID(r.Context(), id)
	if err != nil {
		sh.Logger.Error("Failed to delete streak milestone", slog.Any("error", err))
		http.Error(w, `{"error":"Cannot process your request at the moment"}`, http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		sh.Logger.Error("Error while committing transaction", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]any{"message": "streak milestone deleted successfully"})
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
		content += fmt.Sprintf("\n⭐ Milestone bonus: +%d points!", result.MilestoneBonus)
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

	sh.NotificationEventBus.PublishPushNotificationRequested(ctx, notification, eventbus.GenerateRequestID())
}
