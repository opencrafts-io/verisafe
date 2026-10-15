-- name: RecordActivityCompletion :one
-- Completions can be retried by source event using an optional idempotency key.
SELECT 
  (result).completion_id::bigint as completion_id,
  (result).points_earned::smallint as points_earned,
  (result).current_streak::smallint as current_streak,
  (result).milestone_achieved::boolean as milestone_achieved,
  COALESCE((result).milestone_bonus::smallint,0)::smallint as milestone_bonus,
  (result).already_processed::boolean as already_processed
FROM record_activity_completion(
  @account_id::uuid,
  @activity_id::uuid,
  @metadata::jsonb,
  sqlc.narg(idempotency_key)::text,
  sqlc.narg(awarded_by)::text
) AS result;

-- name: GetUserStreaks :many
SELECT
  a.name AS activity_name,
  CASE
    WHEN us.last_completion_date < CURRENT_DATE THEN 0::smallint
    ELSE us.current_streak
    END::smallint AS current_streak,
  us.longest_streak,
  us.total_completions,
  us.last_completion_date::text AS last_completion_date,
  COALESCE((
    SELECT MIN(sm.days_required -
      CASE
        WHEN us.last_completion_date < CURRENT_DATE THEN 0
        ELSE us.current_streak
      END)
    FROM streak_milestones sm
    LEFT JOIN user_streak_achievements usa
      ON usa.streak_milestone_id = sm.id AND usa.account_id = us.account_id
    WHERE sm.activity_id = a.id
      AND sm.is_active = true
      AND sm.days_required >
        CASE
          WHEN us.last_completion_date < CURRENT_DATE THEN 0
          ELSE us.current_streak
        END
      AND usa.id IS NULL
  )::smallint, 0)::smallint AS days_until_next_milestone
FROM user_streaks us
JOIN activities a ON a.id = us.activity_id
WHERE us.account_id = @account_id::uuid
ORDER BY current_streak DESC, a.name ASC;


-- name: CreateStreakMilestone :one
-- Creates a streak milestone.
INSERT INTO streak_milestones (
  activity_id, days_required, bonus_points, title, description, is_active
) VALUES ( $1, $2, $3, $4, $5, COALESCE(sqlc.narg(is_active)::boolean, true) )
RETURNING *;

-- name: GetAllActiveStreakMilestoneCount :one
-- Returns all active streak milestones count
SELECT count(id) FROM streak_milestones WHERE is_active = true;

-- name: GetAllInactiveStreakMilestoneCount :one
-- Returns all inactive streak milestones count
SELECT count(id) FROM streak_milestones WHERE is_active = false;

-- name: GetAllStreaksMilestoneByActive :many
-- Returns all streaks by activity status
SELECT * FROM streak_milestones WHERE is_active = $1 
LIMIT $2
OFFSET $3;


-- name: DeleteStreakMilestoneByID :exec
-- Deletes streak milestone by ID
DELETE FROM streak_milestones WHERE id = $1;
