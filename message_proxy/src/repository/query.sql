-- name: WriteMessage :exec
INSERT into messages (topic_id, user_id, offset, content)
VALUES ($1, $2, $3, $4);

-- name: WriteMessages :copyfrom
INSERT into messages (topic_id, user_id, offset, content)
VALUES ($1, $2, $3, $4);

-- name: WriteOperation :copyfrom
INSERT INTO operations (topic_id, user_id, client_id, operation_offset, payload)
VALUES ($1, $2, $3, $4, $5);

-- name: GetLastReceivedOffset :one
SELECT MAX(operation_offset) FROM operations
WHERE client_id = $1;

-- name: GetOperationsAfter :many
WITH version_vector AS(
    SELECT * FROM UNNEST($1::bigint[], $2::bigint[])
    AS t(client_id, min_offset)
)
SELECT o.* 
FROM operations o 
LEFT JOIN version_vector
    ON o.client_id = version_vector.client_id
WHERE (
    version_vector.client_id IS NULL
    OR o.operation_offset > version_vector.min_offset
) AND o.topic_id = $3;