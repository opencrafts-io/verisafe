package streak

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/opencrafts-io/verisafe/internal/config"
	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/eventbus"
	"github.com/opencrafts-io/verisafe/internal/middleware"
	"github.com/opencrafts-io/verisafe/internal/middleware/pagination"
	"github.com/opencrafts-io/verisafe/internal/repository"
	streaksvc "github.com/opencrafts-io/verisafe/internal/service/streak"
)

type StreakHandler struct {
	Cacher               core.Cacher
	DB                   core.IDBProvider
	Cfg                  *config.Config
	Logger               *slog.Logger
	NotificationEventBus eventbus.NotificationPublisher

	// Service builds a streak service bound to the caller's transaction. Left
	// nil it falls back to the real implementation; see the role handler for
	// why this field is the testing seam.
	Service func(repository.Querier) streaksvc.Service
}

func (sh *StreakHandler) svc(tx pgx.Tx) streaksvc.Service {
	if sh.Service != nil {
		return sh.Service(repository.New(tx))
	}
	return streaksvc.NewService(repository.New(tx))
}

// acquireAndRun is the two-message (Acquire vs Begin) helper shared with the
// leaderboard and activity handlers; see activity's acquireAndRun for the full
// rationale. Used by the one read-only method here, which -- like activity's
// list methods -- never distinguished a commit failure before this
// extraction, since it never committed.
func (sh *StreakHandler) acquireAndRun(
	r *http.Request,
	fn func(tx pgx.Tx) error,
) error {
	conn, err := sh.DB.Acquire(r.Context())
	if err != nil {
		sh.Logger.Error("Error while processing request", slog.Any("error", err))
		return core.Public(core.ErrInternal, msgInternalServer)
	}

	if err := core.WithTransaction(r.Context(), conn, fn); err != nil {
		return core.Fallback(err, core.ErrInternal, msgCannotProcess)
	}
	return nil
}

// acquireRunAndCommit is acquireAndRun for the three write methods, which
// distinguish a commit failure (msgGeneric) from a Begin or repo-call failure
// (both msgCannotProcess); see activity's identical helper for why the
// distinction needs tracking whether fn reached its own success return rather
// than inspecting WithTransaction's error alone.
func (sh *StreakHandler) acquireRunAndCommit(
	r *http.Request,
	fn func(tx pgx.Tx) error,
) error {
	conn, err := sh.DB.Acquire(r.Context())
	if err != nil {
		sh.Logger.Error("Error while processing request", slog.Any("error", err))
		return core.Public(core.ErrInternal, msgInternalServer)
	}

	var reachedSuccess bool
	err = core.WithTransaction(r.Context(), conn, func(tx pgx.Tx) error {
		if err := fn(tx); err != nil {
			return err
		}
		reachedSuccess = true
		return nil
	})
	if err == nil {
		return nil
	}
	if reachedSuccess {
		return core.Fallback(err, core.ErrInternal, msgGeneric)
	}
	return core.Fallback(err, core.ErrInternal, msgCannotProcess)
}

func (sh *StreakHandler) RegisterHandlers(router core.Router) {
	router.Handle("POST /users/activity/complete", middleware.CreateStack(
		middleware.IsAuthenticated(sh.Cfg, sh.DB, sh.Cacher, sh.Logger),
	)(core.AppHandler(sh.RecordUserActivity)))
	router.Handle("POST /rewards/activity-completions", middleware.CreateStack(
		middleware.IsAuthenticated(sh.Cfg, sh.DB, sh.Cacher, sh.Logger),
		middleware.HasPermission([]string{"award:activity:any"}),
	)(core.AppHandler(sh.AwardActivityForAccount)))
	router.Handle("GET /users/streaks/me", middleware.CreateStack(
		middleware.IsAuthenticated(sh.Cfg, sh.DB, sh.Cacher, sh.Logger),
	)(core.AppHandler(sh.GetMyStreaks)))
	router.Handle("POST /streaks/milestone/create", middleware.CreateStack(
		middleware.IsAuthenticated(sh.Cfg, sh.DB, sh.Cacher, sh.Logger),
		middleware.HasPermission([]string{"create:streak_milestone:any"}),
	)(core.AppHandler(sh.CreateStreakMilestone)))
	router.Handle("GET /streaks/milestone/active", middleware.CreateStack(
		middleware.IsAuthenticated(sh.Cfg, sh.DB, sh.Cacher, sh.Logger),
	)(core.AppHandler(sh.GetAllActiveStreakAchievements)))
	router.Handle("DELETE /streaks/milestone/{id}", middleware.CreateStack(
		middleware.IsAuthenticated(sh.Cfg, sh.DB, sh.Cacher, sh.Logger),
		middleware.HasPermission([]string{"delete:streak_milestone:any"}),
	)(core.AppHandler(sh.DeleteStreakMilestone)))
}

// ActivityCompletionRequest describes a completed activity and optional
// source-event metadata. Trusted services must provide an idempotency key.
type ActivityCompletionRequest struct {
	AccountID      uuid.UUID       `json:"account_id"`
	ActivityID     uuid.UUID       `json:"activity_id"`
	Metadata       json.RawMessage `json:"metadata" swaggertype:"object"`
	IdempotencyKey string          `json:"idempotency_key"`
}

// GetMyStreaks godoc
//
// @Summary      Get the authenticated user's streaks
// @Description  Returns streaks for each streak-eligible activity. A streak is reported as zero after the database's current date passes the last completion date.
// @Tags         streaks
// @Produce      json
// @Success      200 {array} repository.GetUserStreaksRow
// @Failure      500 {object} core.APIError "Failed to fetch streaks"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /users/streaks/me [get]
func (sh *StreakHandler) GetMyStreaks(
	w http.ResponseWriter,
	r *http.Request,
) error {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		return core.Public(core.ErrUnauthorized, msgInternalServer)
	}
	accountID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return core.Public(core.ErrUnauthorized, msgInternalServer)
	}

	var rows []repository.GetUserStreaksRow
	if err := sh.acquireAndRun(r, func(tx pgx.Tx) error {
		var queryErr error
		rows, queryErr = sh.svc(tx).GetUserStreaks(r.Context(), accountID)
		if queryErr != nil {
			sh.Logger.Error("Failed to retrieve user streaks", slog.Any("error", queryErr))
			return core.Public(core.ErrInternal, msgCannotProcess)
		}
		return nil
	}); err != nil {
		return err
	}

	if rows == nil {
		rows = []repository.GetUserStreaksRow{}
	}
	core.WriteJSON(w, http.StatusOK, rows)
	return nil
}

// AwardActivityForAccount godoc
//
// @Summary      Award an activity reward to an account
// @Description  Trusted service-token endpoint. The service must hold award:activity:any and provide a stable source-event idempotency key. Replaying the same account/key returns the original completion without awarding again.
// @Tags         rewards
// @Accept       json
// @Produce      json
// @Param        request body ActivityCompletionRequest true "Verified activity completion"
// @Success      200 {object} repository.RecordActivityCompletionRow
// @Failure      400 {object} core.APIError "Invalid request body or idempotency key"
// @Failure      403 {object} core.APIError "Service token or reward permission required"
// @Failure      500 {object} core.APIError "Failed to award activity"
// @Security     ApiKey
// @Router       /rewards/activity-completions [post]
func (sh *StreakHandler) AwardActivityForAccount(
	w http.ResponseWriter,
	r *http.Request,
) error {
	if !middleware.IsServiceToken(r.Context()) {
		return core.Public(core.ErrForbidden, msgServiceTokenOnly)
	}

	var request ActivityCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		sh.Logger.Error("Failed to parse reward request body", slog.Any("error", err))
		return core.Public(core.ErrInvalidInput, msgCheckBody)
	}
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	if request.AccountID == uuid.Nil || request.ActivityID == uuid.Nil ||
		request.IdempotencyKey == "" || utf8.RuneCountInString(request.IdempotencyKey) > 200 {
		return core.Public(core.ErrInvalidInput, msgInvalidRewardRequest)
	}
	if len(request.Metadata) == 0 {
		request.Metadata = json.RawMessage("null")
	}
	if !json.Valid(request.Metadata) {
		return core.Public(core.ErrInvalidInput, msgInvalidRewardRequest)
	}
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		return core.Public(core.ErrUnauthorized, msgInternalServer)
	}

	key := request.IdempotencyKey
	params := repository.RecordActivityCompletionParams{
		AccountID:      request.AccountID,
		ActivityID:     request.ActivityID,
		Metadata:       []byte(request.Metadata),
		IdempotencyKey: &key,
		AwardedBy:      stringPointer(claims.Subject),
	}
	var completed repository.RecordActivityCompletionRow
	if err := sh.acquireRunAndCommit(r, func(tx pgx.Tx) error {
		var queryErr error
		completed, queryErr = sh.svc(tx).RecordActivity(r.Context(), params)
		if queryErr != nil {
			sh.Logger.Error(
				"Failed to award activity to account",
				slog.String("account_id", request.AccountID.String()),
				slog.String("activity_id", request.ActivityID.String()),
				slog.Any("error", queryErr),
			)
			return core.Public(core.ErrInternal, msgCannotProcess)
		}
		return nil
	}); err != nil {
		return err
	}

	core.WriteJSON(w, http.StatusOK, completed)
	return nil
}

// RecordUserActivity godoc
//
// @Summary      Record a completed activity for the authenticated user
// @Description  The request body's account_id must match the caller's own subject. An optional idempotency_key can make client retries safe.
// @Tags         streaks
// @Accept       json
// @Produce      json
// @Param        request  body      ActivityCompletionRequest  true  "account_id must be the caller's own"
// @Success      200  {object}  map[string]any  "Completion and reward details"
// @Failure      400  {object}  core.APIError  "Invalid request body"
// @Failure      403  {object}  core.APIError  "account_id does not match the caller"
// @Failure      500  {object}  core.APIError  "Failed to record completion"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /users/activity/complete [post]
func (sh *StreakHandler) RecordUserActivity(
	w http.ResponseWriter,
	r *http.Request,
) error {
	requestBody := ActivityCompletionRequest{}
	if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
		sh.Logger.Error("Failed to parse request body", slog.Any("error", err))
		return core.Public(core.ErrInvalidInput, msgCheckBody)
	}
	requestBody.IdempotencyKey = strings.TrimSpace(requestBody.IdempotencyKey)
	if requestBody.AccountID == uuid.Nil || requestBody.ActivityID == uuid.Nil ||
		utf8.RuneCountInString(requestBody.IdempotencyKey) > 200 {
		return core.Public(core.ErrInvalidInput, msgInvalidCompletionRequest)
	}
	if len(requestBody.Metadata) == 0 {
		requestBody.Metadata = json.RawMessage("null")
	}
	if !json.Valid(requestBody.Metadata) {
		return core.Public(core.ErrInvalidInput, msgInvalidCompletionRequest)
	}

	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok || requestBody.AccountID.String() != claims.Subject {
		return core.Public(core.ErrForbidden, msgOwnAccountOnly)
	}

	var completed repository.RecordActivityCompletionRow
	if err := sh.acquireRunAndCommit(r, func(tx pgx.Tx) error {
		var err error
		completed, err = sh.svc(tx).RecordActivity(r.Context(), repository.RecordActivityCompletionParams{
			AccountID:      requestBody.AccountID,
			ActivityID:     requestBody.ActivityID,
			Metadata:       []byte(requestBody.Metadata),
			IdempotencyKey: optionalIdempotencyKey(requestBody.IdempotencyKey),
		})
		if err != nil {
			sh.Logger.Error(
				"Failed to record user activity",
				slog.Any("error", err), slog.Any("activity", requestBody),
			)
			return core.Public(core.ErrInternal, msgCannotProcess)
		}
		return nil
	}); err != nil {
		return err
	}

	if !completed.AlreadyProcessed {
		go sh.sendActivityCompletionNotification(
			requestBody.AccountID.String(),
			&completed,
		)
	}

	core.WriteJSON(w, http.StatusOK, map[string]any{
		"message":            "Activity recorded successfully!",
		"completion_id":      completed.CompletionID,
		"points_earned":      completed.PointsEarned,
		"current_streak":     completed.CurrentStreak,
		"milestone_achieved": completed.MilestoneAchieved,
		"milestone_bonus":    completed.MilestoneBonus,
		"already_processed":  completed.AlreadyProcessed,
	})
	return nil
}

func optionalIdempotencyKey(key string) *string {
	if key == "" {
		return nil
	}
	return &key
}

func stringPointer(value string) *string {
	return &value
}

// CreateStreakMilestone godoc
//
// @Summary      Create a streak milestone
// @Description  Requires create:streak_milestone:any. activity_id and title are required; days_required must be positive and bonus_points must be 1–10.
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
) error {
	requestBody := repository.CreateStreakMilestoneParams{}
	if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
		sh.Logger.Error("Failed to parse request body", slog.Any("error", err))
		return core.Public(core.ErrInvalidInput, msgCheckBody)
	}
	if requestBody.ActivityID == nil || *requestBody.ActivityID == uuid.Nil ||
		requestBody.DaysRequired < 1 || requestBody.BonusPoints < 1 ||
		requestBody.BonusPoints > 10 || strings.TrimSpace(requestBody.Title) == "" {
		return core.Public(core.ErrInvalidInput, msgInvalidMilestone)
	}

	var milestone repository.StreakMilestone
	if err := sh.acquireRunAndCommit(r, func(tx pgx.Tx) error {
		var err error
		milestone, err = sh.svc(tx).CreateMilestone(r.Context(), requestBody)
		if err != nil {
			sh.Logger.Error(
				"Failed to create streak milestone",
				slog.Any("error", err), slog.Any("milestone", requestBody),
			)
			return core.Public(core.ErrInternal, msgCannotProcess)
		}
		return nil
	}); err != nil {
		return err
	}

	core.WriteJSON(w, http.StatusCreated, milestone)
	return nil
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
) error {
	pageParams := pagination.ParsePageParams(r)

	var total int64
	var rows []repository.StreakMilestone
	if err := sh.acquireAndRun(r, func(tx pgx.Tx) error {
		var err error
		total, rows, err = sh.svc(tx).ListActiveMilestones(
			r.Context(), int32(pageParams.PageSize), int32(pageParams.Offset),
		)
		if err != nil {
			sh.Logger.Error(
				"Failed to retrieve active streak milestones",
				slog.Any("error", err),
			)
			return core.Public(core.ErrInternal, msgActiveListFail)
		}
		return nil
	}); err != nil {
		return err
	}

	core.WriteJSON(
		w, http.StatusOK,
		pagination.BuildPaginatedResponse(r, total, rows, pageParams),
	)
	return nil
}

// DeleteStreakMilestone godoc
//
// @Summary      Delete a streak milestone
// @Description  Requires delete:streak_milestone:any.
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
) error {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		sh.Logger.Error("Failed to parse uuid from path", slog.Any("error", err))
		return core.Public(core.ErrInvalidInput, msgCheckBody)
	}

	if err := sh.acquireRunAndCommit(r, func(tx pgx.Tx) error {
		if err := sh.svc(tx).DeleteMilestone(r.Context(), id); err != nil {
			sh.Logger.Error(
				"Failed to delete streak milestone", slog.Any("error", err),
			)
			return core.Public(core.ErrInternal, msgCannotProcess)
		}
		return nil
	}); err != nil {
		return err
	}

	core.WriteJSON(w, http.StatusOK, map[string]any{
		"message": "streak milestone deleted successfully",
	})
	return nil
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
