-- name: RecordActivityCompletion :one
SELECT *
FROM record_activity_completion(@account_id::uuid, @activity_id::uuid, @metadata::jsonb);

-- name: GetUserStreaks :many
SELECT *
FROM get_user_streaks(@account_id::uuid);


-- name: CreateStreakMilestone :one
-- Creates a streak milestone.
INSERT INTO streak_milestones (
  activity_id, days_required, bonus_points, title, description, is_active
) VALUES ( $1, $2, $3, $4, $5, $6 )
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

