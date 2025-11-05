-- name: GetLeaderboard :many
-- Get top N users ranked by vibe points
SELECT * FROM account_vibepoint_rank
LIMIT $1 OFFSET $2;

-- name: GetGlobalLeaderBoardCount :one
SELECT COUNT(*) FROM account_vibepoint_rank;


-- name: GetLeaderBoardRankForUser :one
-- Get the rank for a certain user
SELECT * FROM account_vibepoint_rank
WHERE id = $1
LIMIT 1 OFFSET 0;
