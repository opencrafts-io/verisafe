-- name: CreateAccount :one
INSERT INTO accounts (email, name)
VALUES ($1, $2)
RETURNING *;
-- name: GetAllAccounts :many
SELECT * FROM accounts 
LIMIT $1
OFFSET $2;

-- name: GetAccountByID :one
SELECT * FROM accounts 
WHERE id = $1
LIMIT $1;

-- name: SearchAccountByEmail :many
SELECT * FROM accounts 
WHERE lower(email) LIKE '%' || lower($1) || '%'
LIMIT $1
OFFSET $2
;

-- name: GetAccountByEmail :one
SELECT * FROM accounts 
WHERE lower(email) = lower($1)
LIMIT 1
;


-- name: SearchAccountByName :many
SELECT * FROM accounts 
WHERE lower(name) LIKE '%' || lower($1) || '%'
LIMIT $2
OFFSET $3
;

