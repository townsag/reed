package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	sqlc "github.com/townsag/reed/document_service/internal/repository/sqlc/db"
	"github.com/townsag/reed/document_service/internal/service"
	// ^import the service package so that I can throw domain specific errors
	// instead of postgres errors. Also, import the
)

// define a document repository implementation struct
type DocumentRepository struct {
	queries *sqlc.Queries
	pool *pgxpool.Pool
}

// define a factory method for that struct
func NewDocumentRepository(pool *pgxpool.Pool) *DocumentRepository {
	return &DocumentRepository{
		queries: sqlc.New(pool),
		pool: pool,
	}
}

func repositoryToServiceDocument(repoDocument *sqlc.Document) *service.Document {
	serviceDocument := &service.Document{
		ID: repoDocument.ID.String(),
		CreatedAt: repoDocument.CreatedAt.Time,
		LastModifiedAt: repoDocument.LastModifiedAt.Time,
	}
	if repoDocument.Name.Valid {
		name := repoDocument.Name.String
		serviceDocument.Name = &name
	}
	if repoDocument.Description.Valid {
		description := repoDocument.Description.String
		serviceDocument.Description = &description
	}
	return serviceDocument
}

func serviceToRepoPermission(
	permissionService service.Permission,
) (sqlc.PermissionLevel, error) {
	switch permissionService {
	case service.Viewer:
		return sqlc.PermissionLevelViewer, nil
	case service.Editor:
		return sqlc.PermissionLevelEditor, nil
	case service.Owner:
		return sqlc.PermissionLevelOwner, nil
	default:
		return "", fmt.Errorf("failed to match any of the valid permissions")
	}
}

func repoToServicePermission(
	permissionRepo sqlc.PermissionLevel,
) (service.Permission, error) {
	switch permissionRepo {
	case sqlc.PermissionLevelViewer:
		return service.Viewer, nil
	case sqlc.PermissionLevelEditor:
		return service.Editor, nil
	case sqlc.PermissionLevelOwner:
		return service.Owner, nil
	default:
		return -1, fmt.Errorf("failed to match any of the valid permissions")
	}
}

// TODO: write some unit tests for this
// TODO: this should return an error if the input string does not match the uuid format
func stringToPgUUID(uuid string) pgtype.UUID {
	var out [16]byte
	copy(out[:], uuid)
	return pgtype.UUID{ Bytes: out, Valid: true }
}

var conflictErrorCode string = "23505"

// define methods on that struct that implement the document repository interface 
// defined in the service package. Inside those methods return domain errors defined
// in the service package

func (dr *DocumentRepository) CreateDocument(
	ctx context.Context,
	userId int32, 
	documentName *string,
	documentDescription *string,
) (documentId string, err error) {
	// start a transaction
	tx, err := dr.pool.Begin(ctx)
	if err != nil {
		return "", service.RepoImpl("failed to begin a database transaction", err)
	}
	defer tx.Rollback(ctx)
	txQueries := dr.queries.WithTx(tx)
	// generate a uuid for the document
	uuid := uuid.New()
	// create a record in the documents table for the new document
	params := sqlc.CreateDocumentParams{
		ID: pgtype.UUID{ Bytes: uuid, Valid: true },
	}
	if documentName != nil {
		params.Name = pgtype.Text{
			String: *documentName,
			Valid: true,
		}
	}
	if documentDescription != nil {
		params.Description = pgtype.Text{
			String: *documentDescription,
			Valid: true,
		}
	}
	err = txQueries.CreateDocument(ctx, params)
	if err != nil {
		return "", service.RepoImpl("unable to create a new document", err)
	}
	// create a record in the permissions table designating the user_id
	// as the owner of that document
	paramsPermission := sqlc.UpsertPermissionUserParams{
		RecipientID: string(userId),
		DocumentID: pgtype.UUID{ Bytes: uuid, Valid: true },
		PermissionLevel: sqlc.PermissionLevelOwner,
		CreatedBy: string(userId),
	}
	err = txQueries.UpsertPermissionUser(ctx, paramsPermission)
	if err != nil {
		return "", service.RepoImpl("unable to create permissions on new document for user", err)
	}
	// return the generated document id
	err = tx.Commit(ctx)
	if err != nil {
		return "", service.RepoImpl(
			"error encountered when creating document",
			err,
		)
	}
	return uuid.String(), nil
}

func (dr *DocumentRepository) GetDocument(
	ctx context.Context,
	documentId string,
) (document *service.Document, err error) {
	var documentUuid [16]byte
	// documentUuid[:] creates a slice that references the entire underlying array
	// this allows copy to work on the byte array because copy only works on slices
	copy(documentUuid[:], documentId)
	repoDocument, err := dr.queries.GetDocument(
		ctx,
		pgtype.UUID{ Bytes: documentUuid, Valid: true },
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, service.NotFound(
				fmt.Sprintf("no document found with id %s", documentId),
				err,
			)
		} else {
			return nil, service.RepoImpl(
				fmt.Sprintf("error when trying to retrieve document with id: %s", documentId),
				err,
			)
		}
	}
	return repositoryToServiceDocument(&repoDocument), nil
}

func (dr *DocumentRepository) UpdateDocument(
	ctx context.Context,
	documentId string,
	documentName *string,
	documentDescription *string,
) error {
	if documentName == nil && documentDescription == nil {
		return nil
	}
	var documentUuid [16]byte
	copy(documentUuid[:], []byte(documentId))
	// don't use the generated sqlc code for this one, dynamically construct the
	// query using sting concatenation then execute the query using the pgxpool 
	// attribute of the document repository. This may not be the best approach 
	// but this is a simple way to evaluate the dynamic query building approach
	// ^didn't like this approach because it is not compile time type checked
	// arguments := []interface{}{documentId}
	// argumentIndex := 2
	// sql := "UPDATE documents SET last_modified_at = NOW()"
	// if documentName != nil {
	// 	sql += fmt.Sprintf(", name = $%d", argumentIndex)
	// 	argumentIndex++
	// 	arguments = append(arguments, *documentName)
	// }
	// if documentDescription != nil {
	// 	sql += fmt.Sprintf(", description = $%d", argumentIndex)
	// 	argumentIndex++
	// 	arguments = append(arguments, *documentDescription)
	// }
	// sql += " WHERE id = $1"
	// dr.pool.Exec(ctx, sql, arguments...)
	params := sqlc.UpdateDocumentParams{
		ID: pgtype.UUID{ Bytes: documentUuid, Valid: true },
	}
	if documentName != nil {
		params.Name = pgtype.Text{ String: *documentName, Valid: true }
	}
	if documentDescription != nil {
		params.Description = pgtype.Text{ String: *documentDescription, Valid: true }
	}
	countRows, err := dr.queries.UpdateDocument(ctx, params)
	if err != nil {
		return service.RepoImpl(
			fmt.Sprintf("error encountered when trying to update document with id: %s", documentId),
			err,
		)
	}
	if countRows < 1 {
		return service.NotFound(
			fmt.Sprintf("unable to update the document with id: %s", documentId),
			nil,
		)
	}
	return nil
}

// what does it mean for a document to be deleted: only support hard deletion
// - delete the document in the documents table and all permissions on the document
//	 in the permissions table
// - publish an event notifying other services that the document has been deleted
// decided to use hard deletion because it is simpler to implement and understand 
// by users
// decided not to use cascading deletes because of hidden potential for mistakes
func (dr *DocumentRepository) DeleteDocument(
	ctx context.Context,
	documentId string,
) error {
	// start a transaction
	tx, err := dr.pool.Begin(ctx)
	if err != nil {
		return service.RepoImpl("failed to begin a database transaction", err)
	}
	defer tx.Rollback(ctx)
	txQueries := dr.queries.WithTx(tx)
	// delete any rows in the permissions table that reference that document
	// this should use the index on the permissions table using the document column
	_, err = txQueries.DeletePermissionByDocument(
		ctx, stringToPgUUID(documentId),
	)
	if err != nil {
		return service.RepoImpl(
			fmt.Sprintf("failed to delete document with id %s", documentId),
			err,
		)
	}
	// delete the row from the documents table
	count, err := txQueries.DeleteDocument(ctx, stringToPgUUID(documentId))
	if err != nil {
		return service.RepoImpl(
			fmt.Sprintf("failed to delete document with id: %s", documentId),
			err,
		)
	}
	if count < 1 {
		return service.NotFound(
			fmt.Sprintf("no document found with id: %s", documentId),
			nil,
		)
	}
	err = tx.Commit(ctx)
	if err != nil {
		return service.RepoImpl(
			"failed to commit transaction",
			err,
		)
	}
	return nil
}

func (dr *DocumentRepository) ListDocumentsByPrincipal(
	ctx context.Context,
	principalId string, 
	permissions []service.Permission,
	cursor *service.Cursor,
	pageSize int32,
) (documentPermissions []service.DocumentPermission, cursorResp *service.Cursor, err error) {
	// determine the query parameters by parsing the cursor object
	// assume that a default cursor will be constructed on the client side
	// and we don't need to support the null cursor case
	if cursor == nil {
		// can return nil here for documents because nil is the zero value
		// for the slice type. Slice operations can be made on nil
		return nil, nil, service.ErrNilPointer
	}
	if len(permissions) < 1 {
		return nil, nil, service.InvalidInput("expected at least one permission", nil)
	}
	repoPermissionsList := make([]sqlc.PermissionLevel, 0)
	for _, permission := range permissions {
		repoPermission, err := serviceToRepoPermission(permission)
		if err != nil {
			return nil, nil, service.InvalidInput(
				fmt.Sprintf("input permission: %v does not map to any valid permissions", permission), nil,
			)
		}
		repoPermissionsList = append(repoPermissionsList, repoPermission)
	}
	documentPermissions = make([]service.DocumentPermission, 0)
	cursorResp = &service.Cursor{
		SortField: cursor.SortField,
	}
	switch cursor.SortField {
	case service.CreatedAt:
		params := sqlc.ListDocumentsByCreatedAtParams{
			RecipientID: principalId,
			CreatedAt: pgtype.Timestamptz{ Time: cursor.LastSeenTime, Valid: true },
			ID: stringToPgUUID(cursor.LastSeenDocument),
			Limit: pageSize,
			PermissionsList: repoPermissionsList,
		}
		rows, err := dr.queries.ListDocumentsByCreatedAt(ctx, params)
		if err != nil {
			return nil, nil, service.RepoImpl("failed to retired document by principal", err)
		}
		if len(rows) > 0 {
			cursorResp.LastSeenTime = rows[len(rows) - 1].Document.CreatedAt.Time
			cursorResp.LastSeenDocument = rows[len(rows) - 1].Document.ID.String()
		} else {
			cursorResp.LastSeenTime = cursor.LastSeenTime
			cursorResp.LastSeenDocument = cursor.LastSeenDocument
		}
		for _, row := range rows {
			permission, err := repoToServicePermission(row.PermissionLevel)
			if err != nil {
				// TODO: log the error
				// skip over the permission value with invalid data
				continue
			}
			documentPermissions = append(
				documentPermissions,
				service.DocumentPermission{ 
					Document: *repositoryToServiceDocument(&row.Document), 
					Permission: permission,
				},
			)
		}
	case service.LastModifiedAt:
		params := sqlc.ListDocumentsByLastModifiedAtParams{
			RecipientID: principalId,
			LastModifiedAt: pgtype.Timestamptz{ Time: cursor.LastSeenTime, Valid: true },
			ID: stringToPgUUID(cursor.LastSeenDocument),
			Limit: pageSize,
			PermissionsList: repoPermissionsList,
		}
		rows, err := dr.queries.ListDocumentsByLastModifiedAt(ctx, params)
		if err != nil {
			return nil, nil, service.RepoImpl("failed to retired document by principal", err)
		}
		if len(rows) > 0 {
			cursorResp.LastSeenTime = rows[len(rows) - 1].Document.LastModifiedAt.Time
			cursorResp.LastSeenDocument = rows[len(rows) - 1].Document.ID.String()
		} else {
			cursorResp.LastSeenTime = cursor.LastSeenTime
			cursorResp.LastSeenDocument = cursor.LastSeenDocument
		}
		for _, row := range rows {
			permission, err := repoToServicePermission(row.PermissionLevel)
			if err != nil {
				// TODO: log the error
				// skip over the permission value with invalid data
				continue
			}
			documentPermissions = append(
				documentPermissions,
				service.DocumentPermission{ 
					Document: *repositoryToServiceDocument(&row.Document), 
					Permission: permission,
				},
			)
		}
	}
	return documentPermissions, cursorResp, nil
}

func (dr *DocumentRepository) GetPermissionOfPrincipalOnDocument(
	ctx context.Context,
	documentId string,
	principalId string,
) (permission service.Permission, err error) {
	// get the permission of a user or a guest on a document
	// return a not found error if that principal has no permissions on that document
	params := sqlc.GetPermissionOfPrincipalOnDocumentParams{
		DocumentID: stringToPgUUID(documentId),
		RecipientID: principalId,
	}
	row, err := dr.queries.GetPermissionOfPrincipalOnDocument(
		ctx,
		params,
	)
	if err != nil {
		// check for no rows found
		if errors.Is(err, pgx.ErrNoRows) {
			return -1, service.NotFound(
				fmt.Sprintf("no permissions found for principal: %s on document: %s", principalId, documentId),
				err,
			)
		} else {
			return -1, service.RepoImpl(
				fmt.Sprintf("failed to get permission for principal: %s on document: %s", principalId, documentId),
				err,
			)
		}
	}
	permission, err = repoToServicePermission(row.PermissionLevel)
	if err != nil {
		return -1, service.RepoImpl("failed to parse permission level", err)
	}
	return permission, nil
}

func (dr *DocumentRepository) ListPermissionsOnDocument(
	ctx context.Context,
	documentId string,
) (recipientPermissions []service.RecipientPermission, err error) {
	// get the recipient permission rows from the database
	repoRecipientPermissions, err := dr.queries.ListPermissionsOnDocument(
		ctx, stringToPgUUID(documentId),
	)
	// return errors if necessary
	if err != nil {
		return nil, service.RepoImpl(
			fmt.Sprintf("failed to read permissions on document: %s", documentId),
			err,
		)	
	}
	// reformat them from repo to service format
	recipientPermissions = make([]service.RecipientPermission, len(recipientPermissions))
	for i, elem := range repoRecipientPermissions {
		servicePermission, err := repoToServicePermission(elem.PermissionLevel)
		if err != nil {
			// TODO: log the error
			continue
		}
		recipientPermissions[i] = service.RecipientPermission{
			RecipientId: elem.RecipientID,
			Permission: servicePermission,
			CreatedAt: elem.CreatedAt.Time,
			LastModifiedAt: elem.LastModifiedAt.Time,
		}
	}
	return recipientPermissions, nil
}

func (dr *DocumentRepository) CreateGuest(
	ctx context.Context, 
	creatorId string,
	documentId string,
	permission service.Permission,
) (guestId string, err error) {
	// generate a new uuid for the guest
	uuid := uuid.New()
	repoPermission, err := serviceToRepoPermission(permission)
	if err != nil {
		return "", service.InvalidInput(
			fmt.Sprintf("invalid input for permission: %v", permission),
			err,
		)
	}
	// get a transaction
	tx, err := dr.pool.Begin(ctx)
	if err != nil {
		return "", service.RepoImpl("failed to create a transaction when creating a guest", err)
	}
	defer tx.Rollback(ctx)
	txQueries := dr.queries.WithTx(tx)
	// add a new guest to the guests table
	params := sqlc.CreateGuestParams{
		ID: stringToPgUUID(uuid.String()),
		DocumentID: stringToPgUUID(documentId),
		CreatedBy: creatorId,
	}
	err = txQueries.CreateGuest(ctx, params)
	if err != nil {
		var pgError *pgconn.PgError
		if errors.As(err, &pgError) {
			if pgError.Code == conflictErrorCode {
				return "", service.UniqueConflict(
					fmt.Sprintf("unique conflict encountered when creating guest with id: %s", uuid.String()),
					err,
				)
			} else {
				return "", service.RepoImpl("encountered a postgres error when trying to create a user", err)
			}
		} else {
			return "", service.RepoImpl("encountered an unexpected error when creating a user", err)
		}
	}
	// add a new permission record to the permissions table associated with that guest
	paramsPermission := sqlc.InsertPermissionGuestParams{
		RecipientID: uuid.String(),
		DocumentID: stringToPgUUID(documentId),
		PermissionLevel: repoPermission,
		CreatedBy: creatorId,
	}
	err = txQueries.InsertPermissionGuest(ctx, paramsPermission)
	if err != nil {
		var pgError *pgconn.PgError
		if errors.As(err, &pgError) {
			if pgError.Code == conflictErrorCode {
				return "", service.UniqueConflict(
					fmt.Sprintf("unique conflict encountered when creating permission on document: %s, for guest with id: %s", documentId, uuid.String()),
					err,
				)
			} else {
				return "", service.RepoImpl("encountered a postgres error when trying to create a permission", err)
			}
		} else {
			return "", service.RepoImpl("encountered an unexpected error when creating a permission", err)
		}
	}
	// commit the transaction
	err = tx.Commit(ctx)
	if err != nil {
		return "", service.RepoImpl("failed to commit transaction", err)
	}
	return uuid.String(), nil
}

func (dr *DocumentRepository) UpsertPermissionsUser(
	ctx context.Context, 
	userId string, 
	documentId string, 
	permission service.Permission,
) (err error) {
	repoPermission, err := serviceToRepoPermission(permission)
	if err != nil {
		return service.InvalidInput(
			fmt.Sprintf("invalid input for permission: %d", permission),
			err,
		)
	}
	params := sqlc.UpsertPermissionUserParams{
		RecipientID: userId,
		DocumentID: stringToPgUUID(documentId),
		PermissionLevel: repoPermission,
		CreatedBy: userId,
	}
	err = dr.queries.UpsertPermissionUser(ctx, params)
	if err != nil {
		return service.RepoImpl("failed to update user permission", err)
	}
	return nil
}

func (dr *DocumentRepository) UpdatePermissionGuest(
	ctx context.Context,
	guestId string,
	documentId string,
	permission service.Permission,
) (err error) {
	permissionRepo, err := serviceToRepoPermission(permission)
	if err != nil {
		return service.InvalidInput(
			fmt.Sprintf("invalid input received for permission: %d", permission),
			err,
		)
	}
	params := sqlc.UpdatePermissionGuestParams{
		RecipientID: guestId,
		DocumentID: stringToPgUUID(documentId),
		PermissionLevel: permissionRepo,
	}
	count, err := dr.queries.UpdatePermissionGuest(ctx, params)
	if err != nil {
		return service.RepoImpl("failed to update guest permissions", err)
	}
	if count < 1 {
		return service.NotFound(
			fmt.Sprintf("unable to find permission of guest: %s on document: %s", guestId, documentId),
			nil,
		)
	}
	return nil
}

func (dr *DocumentRepository) DeletePermissionsPrincipal(
	ctx context.Context,
	recipientId string,
	documentId string,
) (err error) {
	// let the code at the service level decide if we should be able to delete the owner of 
	// a documents permissions on that document. This business logic does not need to be
	// enforced in two places
	params := sqlc.DeletePermissionPrincipalParams{
		RecipientID: recipientId,
		DocumentID: stringToPgUUID(documentId),
	}
	count, err := dr.queries.DeletePermissionPrincipal(ctx, params)
	if err != nil {
		return service.RepoImpl(
			fmt.Sprintf("error encountered when deleting permissions of %s on document %s", recipientId, documentId),
			err,
		)
	}
	if count < 1 {
		return service.NotFound(
			fmt.Sprintf("no permission found when deleting permission with recipient: %s and document %s", recipientId, documentId),
			nil,
		)
	}
	return nil
}