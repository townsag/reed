-- name: CreateDocument :exec
INSERT INTO documents (id, name, description) 
VALUES ($1, $2, $3);

-- name: GetDocument :one
SELECT * FROM documents 
WHERE id = $1;

-- name: UpdateDocument :execrows
UPDATE documents SET
name = COALESCE($2, name),
description = COALESCE($3, description)
WHERE id = $1;

-- name: DeleteDocument :execrows
DELETE FROM documents 
WHERE id = $1;

-- name: DeletePermissionByDocument :execrows
DELETE FROM permissions
WHERE document_id = $1;

-- this query uses cursor based pagination to list documents 
-- name: ListDocumentsByCreatedAt :many
SELECT sqlc.embed(documents), permissions.permission_level
FROM documents JOIN permissions
ON documents.id = permissions.document_id
WHERE (documents.created_at < $2 OR (documents.created_at = $2 AND documents.id < $3))
AND permissions.permission_level = ANY(@permissions_list::permission_level[])
AND permissions.recipient_id = $1
ORDER BY documents.created_at DESC, documents.id DESC
LIMIT $4;

-- this query is very similar, only it orders by the last modified at field instead
-- of the created at field
-- name: ListDocumentsByLastModifiedAt :many
SELECT sqlc.embed(documents), permissions.permission_level
FROM documents JOIN permissions
ON documents.id = permissions.document_id
WHERE (documents.last_modified_at < $2 OR (documents.last_modified_at = $2 AND documents.id < $3))
AND permissions.permission_level = ANY(@permissions_list::permission_level[])
AND permissions.recipient_id = $1
ORDER BY documents.last_modified_at DESC, documents.id DESC
LIMIT $4;

-- name: GetPermissionOfPrincipalOnDocument :one
SELECT permission_level, created_by, created_at, last_modified_at
FROM permissions 
WHERE document_id = $1 AND recipient_id = $2;

-- name: ListPermissionsOnDocument :many
SELECT recipient_id, recipient_type, permission_level, created_by, created_at, last_modified_at
FROM permissions
WHERE document_id = $1;

-- name: UpsertPermissionUser :exec
INSERT INTO permissions (
    recipient_id, recipient_type, document_id, permission_level, created_by
) VALUES ($1, 'user', $2, $3, $4)
ON CONFLICT (recipient_id, document_id)
DO UPDATE SET 
    last_modified_at = NOW(),
    permission_level = $3;
-- we dont have to check that the recipient id and the document
-- id match in the where clause of the do update set because they
-- have to match for there to have been a conflict

-- name: InsertPermissionGuest :exec
INSERT INTO permissions (
    recipient_id, recipient_type, document_id, permission_level, created_by
) VALUES ($1, 'guest', $2, $3, $4);


-- name: UpdatePermissionGuest :execrows
UPDATE permissions SET
permission_level = $3,
last_modified_at = NOW()
WHERE recipient_id = $1
AND document_id = $2
AND recipient_type = 'guest';

-- when adding a guest, use CreateGuest to create the record in the guest
-- table and UpdatePermissionPrincipal to create the record in the permissions
-- table, package these two operations using a transaction
-- name: CreateGuest :exec
INSERT INTO guests (
    id, document_id, created_by
) VALUES ($1, $2, $3);

-- name: DeletePermissionPrincipal :execrows
DELETE FROM permissions 
WHERE recipient_id = $1
AND document_id = $2;