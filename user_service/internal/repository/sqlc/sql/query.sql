-- name: CreateUserAndReturnId :one
INSERT INTO users (user_name, email, max_documents, hashed_password)
VALUES ($1, $2, $3, $4)
RETURNING id;

-- name: GetUserById :one
SELECT id, user_name, email, max_documents, hashed_password, is_active, created_at, last_modified 
FROM users 
WHERE id = $1;

-- name: GetUserByEmail :one
SELECT id, user_name, email, max_documents, hashed_password, is_active, created_at, last_modified
FROM users
WHERE email = $1;

-- name: DeactivateUser :one
UPDATE users
SET is_active = FALSE, last_modified = CURRENT_TIMESTAMP
WHERE id = $1
RETURNING id;

-- name: ChangeUserPassword :one
UPDATE users
SET hashed_password = $1, last_modified = CURRENT_TIMESTAMP
WHERE id = $2
RETURNING id;