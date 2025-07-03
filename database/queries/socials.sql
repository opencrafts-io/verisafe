-- name: CreateSocial :one
INSERT INTO socials (
    account_id,
    provider,
    email,
    name,
    first_name,
    last_name,
    nick_name,
    description,
    user_id,
    avatar_url,
    location,
    access_token,
    access_token_secret,
    refresh_token,
    expires_at
)
VALUES (
    $1, -- account_id UUID
    $2, -- provider VARCHAR
    $3, -- email VARCHAR
    $4, -- name VARCHAR
    $5, -- first_name VARCHAR
    $6, -- last_name VARCHAR
    $7, -- nick_name VARCHAR
    $8, -- description TEXT
    $9, -- user_id VARCHAR
    $10, -- avatar_url TEXT
    $11, -- location VARCHAR
    $12, -- access_token TEXT
    $13, -- access_token_secret TEXT
    $14, -- refresh_token TEXT
    $15  -- expires_at TIMESTAMP
)
RETURNING *;


-- name: GetSocialByExternalUserID :one
SELECT * FROM socials
WHERE user_id = $1;


-- name: GetAccountByProvider :many
-- Returns a list of all social accounts by provider
-- note that the results are paginated using the limit offset scheme
SELECT * FROM socials
WHERE lower(provider) = lower($1)
LIMIT $2
OFFSET $3;
