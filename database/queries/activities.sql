-- name: CreateActivity :one
-- Creates an activity.
-- An activity is basically an action that a user can 
-- take to be awarded vibe points
INSERT INTO activities (
  name, 
  description, 
  category,
  points_awarded, 
  max_daily_completions, 
  streak_eligible
) VALUES (
  $1, $2, $3, $4,
  COALESCE(sqlc.narg(max_daily_completions)::smallint, 1),
  COALESCE(sqlc.narg(streak_eligible)::boolean, true)
)
RETURNING *;


-- name: GetActivityByID :one
-- Returns an activity specified by its id
SELECT *  FROM activities WHERE id = $1
LIMIT 1;

-- name: GetAllActivities :many
-- Returns all the activities in the system paginated using the 
-- limit-offset schme
SELECT * FROM activities LIMIT $1 OFFSET $2;

-- name: GetAllActiveActivities :many
-- Returns all the active activities in the system paginated using the 
-- limit-offset schme
SELECT * FROM activities WHERE is_active = true LIMIT $1 OFFSET $2;


-- name: GetAllInactiveActivities :many
-- Returns all the inactive activities in the system paginated using the 
-- limit-offset schme
SELECT * FROM activities WHERE is_active = false LIMIT $1 OFFSET $2;

-- name: GetAllActivitiesCount :one
-- Returns all activities count regardless of activity status
SELECT COUNT(id) FROM activities;

-- name: GetAllActiveActivitiesCount :one
-- Returns all the active activities count in the system
SELECT COUNT(id) FROM activities WHERE is_active = true;

-- name: GetAllInactiveActivitiesCount :one
-- Returns all the inactive activities count in the system
SELECT COUNT(id) FROM activities WHERE is_active = false;

-- name: UpdateActivity :one
-- Updates an activity specified by its ID
UPDATE activities
  SET 
    name = COALESCE(NULLIF(@name::varchar,''), name),
    description = COALESCE(NULLIF(@description::varchar,''), description),
    category = COALESCE(NULLIF(@category::varchar,''), category),
    points_awarded = COALESCE(sqlc.narg(points_awarded)::smallint, points_awarded),
    max_daily_completions = COALESCE(sqlc.narg(max_daily_completions)::smallint, max_daily_completions),
    streak_eligible = COALESCE(sqlc.narg(streak_eligible)::boolean, streak_eligible),
    is_active = COALESCE(sqlc.narg(is_active)::boolean, is_active),
    updated_at = NOW()
  WHERE id = $1
RETURNING *;

-- name: DeleteActivity :exec
DELETE FROM activities
WHERE id = $1;


-- name: GetAllUserActivityCompletions :many
-- Returns activity a certain user specified by their id has completed ordered 
-- from the most recent to the oldest
SELECT
  id,
  account_id,
  activity_id,
  completed_at,
  completion_date,
  points_earned,
  metadata
FROM activity_completions
WHERE account_id = $1
LIMIT $2 OFFSET $3;


-- name: GetAllUserActivityCompletionsCount :one
-- Returns the number of record that have been done on the user's completed
-- activities
SELECT count(id) FROM activity_completions WHERE account_id = $1;
