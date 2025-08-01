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


-- name: GetAllAccountSocials :many
-- Returns a list of oauth providers that they've granted
-- note that the results are not paginated since we dont support a 
-- whole lot of social oauth providers
SELECT * FROM socials
WHERE account_id = $1;

-- name: UpdateSocial :one
UPDATE socials
SET
    email = COALESCE(NULLIF(@email::varchar,''), email),
    name = COALESCE(NULLIF(@name::varchar,''), name),
    first_name = COALESCE(NULLIF(@first_name::varchar,''), first_name),
    last_name = COALESCE(NULLIF(@last_name::varchar,''), last_name),
    nick_name = COALESCE(NULLIF(@nick_name::varchar,''), nick_name),
    description = COALESCE(NULLIF(@description::text,''), description),
    avatar_url = COALESCE(NULLIF(@avatar_url::text,''), avatar_url),
    location = COALESCE(NULLIF(@location::varchar,''), location),
    refresh_token = COALESCE(NULLIF(@refresh_token::text,''), refresh_token),
    access_token = COALESCE(NULLIF(@access_token::text,''), access_token),
    access_token_secret = COALESCE(NULLIF(@access_token_secret::text,''), access_token_secret),
    expires_at = COALESCE($3, expires_at),
    updated_at = NOW()
WHERE user_id = $1 AND provider = $2
RETURNING *;
