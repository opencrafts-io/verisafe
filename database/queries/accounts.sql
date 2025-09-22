-- name: CreateAccount :one
INSERT INTO accounts (email, name, type, avatar_url)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetAllAccounts :many
SELECT * FROM accounts WHERE type = 'human' 
LIMIT $1
OFFSET $2;

-- name: GetAccountByID :one
SELECT * FROM accounts 
WHERE id = $1;

-- name: SearchAccountByEmail :many
SELECT * FROM accounts 
WHERE lower(email) LIKE '%' || lower($1) || '%'
LIMIT $2
OFFSET $3
;

-- name: GetAccountByEmail :one
SELECT * FROM accounts 
WHERE lower(email) = lower($1)
LIMIT 1
;


-- name: GetAccountByUsername :one
SELECT * FROM accounts WHERE lower(username) = lower($1);

-- name: SearchAccountByName :many
SELECT * FROM accounts 
WHERE lower(name) LIKE '%' || lower($1) || '%'
LIMIT $2
OFFSET $3
;

-- name: SearchAccountByUsername :many
SELECT * FROM accounts 
WHERE lower(username) LIKE '%' || lower($1) || '%'
LIMIT $2
OFFSET $3
;

-- name: UpdateAccountDetails :exec
UPDATE accounts
  SET
    username = COALESCE(NULLIF(@username::varchar,''), username),
    email = COALESCE(NULLIF(@email::varchar, ''), email),
    name = COALESCE(NULLIF(@name::varchar,''), name),
    terms_accepted = COALESCE(@terms_accepted::boolean, terms_accepted),
    onboarded = COALESCE(@onboarded::boolean, onboarded),
    national_id = COALESCE(NULLIF(@national_id::varchar,''), national_id),
    avatar_url = COALESCE(NULLIF(@avatar_url::text,''), avatar_url),
    bio = COALESCE(NULLIF(@bio::text,''), bio),
    updated_at = NOW()
  WHERE id = $1
  ;


-- name: UpdateAccountPhoneNumber :exec
-- Only updates the primary phone number for an account
UPDATE accounts
  SET
    phone = COALESCE(NULLIF(@phone::varchar,''), phone),
    updated_at = NOW()
  WHERE id = $1
  ;
