-- name: WriteMessage :exec
INSERT into messages (topic_id, user_id, offset, content)
VALUES ($1, $2, $3, $4);

-- name: WriteMessages :copyfrom
INSERT into messages (topic_id, user_id, offset, content)
VALUES ($1, $2, $3, $4);