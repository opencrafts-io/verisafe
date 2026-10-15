-- name: GetLeaderboard :many
-- Get top N users ranked by vibe points
SELECT * FROM account_vibepoint_rank
ORDER BY vibe_points DESC, id ASC
LIMIT $1 OFFSET $2;

-- name: GetGlobalLeaderBoardCount :one
SELECT COUNT(*) FROM account_vibepoint_rank;


-- name: GetLeaderBoardRankForUser :one
-- Get the rank for a certain user
SELECT * FROM account_vibepoint_rank
WHERE id = $1
LIMIT 1 OFFSET 0;


-- name: GetLeaderboardAroundUser :many
-- Returns a stable, centered leaderboard window around one human account.
WITH ranked AS (
  SELECT
    id,
    email,
    name,
    username,
    vibe_points,
    avatar_url,
    created_at,
    updated_at,
    vibe_rank,
    ROW_NUMBER() OVER (ORDER BY vibe_points DESC, id ASC) AS position,
    COUNT(*) OVER () AS total_users
  FROM account_vibepoint_rank
), target AS (
  SELECT position, total_users
  FROM ranked
  WHERE id = @user_id::uuid
), window_start AS (
  SELECT LEAST(
    GREATEST(target.position - ((sqlc.arg(window_size)::bigint - 1) / 2), 1),
    GREATEST(target.total_users - sqlc.arg(window_size)::bigint + 1, 1)
  ) AS position
  FROM target
)
SELECT ranked.*
FROM ranked, window_start
WHERE ranked.position BETWEEN window_start.position
  AND window_start.position + sqlc.arg(window_size)::bigint - 1
ORDER BY ranked.position;
