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

func repositoryToServiceDocument(repoDocument *sqlc.Document) (*service.Document, error) {
	documentId, err := uuid.FromBytes(repoDocument.ID.Bytes[:])
	if err != nil {
		return nil, err
	}
	serviceDocument := &service.Document{
		ID: documentId,
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
	return serviceDocument, nil
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

var conflictErrorCode string = "23505"

// define methods on that struct that implement the document repository interface 
// defined in the service package. Inside those methods return domain errors defined
// in the service package

func (dr *DocumentRepository) CreateDocument(
	ctx context.Context,
	userId uuid.UUID, 
	documentName *string,
	documentDescription *string,
) (documentId uuid.UUID, err error) {
	// start a transaction
	tx, err := dr.pool.Begin(ctx)
	if err != nil {
		return uuid.Nil, service.RepoImpl("failed to begin a database transaction", err)
	}
	defer tx.Rollback(ctx)
	txQueries := dr.queries.WithTx(tx)
	// generate a uuid for the document
	documentId = uuid.New()
	// create a record in the documents table for the new document
	params := sqlc.CreateDocumentParams{
		ID: pgtype.UUID{ Bytes: documentId, Valid: true },
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
		return uuid.Nil, service.RepoImpl("unable to create a new document", err)
	}
	// create a record in the permissions table designating the user_id
	// as the owner of that document
	paramsPermission := sqlc.UpsertPermissionUserParams{
		RecipientID: pgtype.UUID{ Bytes: userId, Valid: true },
		DocumentID: pgtype.UUID{ Bytes: documentId, Valid: true },
		PermissionLevel: sqlc.PermissionLevelOwner,
		CreatedBy: pgtype.UUID{ Bytes: userId, Valid: true },
	}
	err = txQueries.UpsertPermissionUser(ctx, paramsPermission)
	if err != nil {
		return uuid.Nil, service.RepoImpl("unable to create permissions on new document for user", err)
	}
	// return the generated document id
	err = tx.Commit(ctx)
	if err != nil {
		return uuid.Nil, service.RepoImpl(
			"error encountered when creating document",
			err,
		)
	}
	return documentId, nil
}

func (dr *DocumentRepository) GetDocument(
	ctx context.Context,
	documentId uuid.UUID,
) (document *service.Document, err error) {
	repoDocument, err := dr.queries.GetDocument(
		ctx,
		pgtype.UUID{ Bytes: documentId, Valid: true },
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, service.NotFound(
				fmt.Sprintf("no document found with id %s", documentId.String()),
				err,
			)
		} else {
			return nil, service.RepoImpl(
				fmt.Sprintf("error when trying to retrieve document with id: %s", documentId.String()),
				err,
			)
		}
	}

	document, err = repositoryToServiceDocument(&repoDocument)
	if err != nil {
		return nil, service.RepoImpl("failed to parse the returned document", err)
	}
	return document, nil
}

func (dr *DocumentRepository) UpdateDocument(
	ctx context.Context,
	documentId uuid.UUID,
	documentName *string,
	documentDescription *string,
) error {
	if documentName == nil && documentDescription == nil {
		return nil
	}
	params := sqlc.UpdateDocumentParams{
		ID: pgtype.UUID{ Bytes: documentId, Valid: true },
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
			fmt.Sprintf("error encountered when trying to update document with id: %v", documentId.String()),
			err,
		)
	}
	if countRows < 1 {
		return service.NotFound(
			fmt.Sprintf("unable to update the document with id: %v", documentId.String()),
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
	documentId uuid.UUID,
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
		ctx, pgtype.UUID{ Bytes: documentId, Valid: true },
	)
	if err != nil {
		return service.RepoImpl(
			fmt.Sprintf("failed to delete document with id %s", documentId.String()),
			err,
		)
	}
	// delete any guests from the guests table that are linked to that document
	_, err = txQueries.DeleteGuestsByDocument(
		ctx, pgtype.UUID{ Bytes: documentId, Valid: true },
	)
	if err != nil {
		return service.RepoImpl(
			fmt.Sprintf("failed to delete guests with document id: %s", documentId.String()),
			err,
		)
	}
	// delete the row from the documents table
	count, err := txQueries.DeleteDocument(ctx, pgtype.UUID{ Bytes: documentId, Valid: true })
	if err != nil {
		return service.RepoImpl(
			fmt.Sprintf("failed to delete document with id: %s", documentId.String()),
			err,
		)
	}
	if count < 1 {
		return service.NotFound(
			fmt.Sprintf("no document found with id: %s", documentId.String()),
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

func parseDocumentPermission(
	document sqlc.Document,
	permissionLevel sqlc.PermissionLevel,
) (*service.DocumentPermission, error) {
	permission, err := repoToServicePermission(permissionLevel)
	if err != nil {
		// TODO: log the error
		return nil, service.RepoImpl(
			fmt.Sprintf(
				"failed to parse permission for documentId: %s", 
				document.ID.String(), 
			),
			err,
		)
	}
	serviceDocument, err := repositoryToServiceDocument(&document)
	if err != nil {
		return nil, service.RepoImpl(
			fmt.Sprintf(
				"failed to parse document with documentId: %s", document.ID.String(),
			),
			err,
		)
	}
	return &service.DocumentPermission{
		Document: *serviceDocument,
		Permission: permission,
	}, nil
}

func (dr *DocumentRepository) readDocuments(
	ctx context.Context,
	principalId uuid.UUID, 
	repoPermissionList []sqlc.PermissionLevel,
	cursor *service.Cursor,
	pageSize int32,
) (
	documentPermissionList []service.DocumentPermission,
	err error,
) {
	switch cursor.SortField {
	case service.CreatedAt:
		params := sqlc.ListDocumentsByCreatedAtParams{
			RecipientID: pgtype.UUID{ Bytes: principalId, Valid: true },
			CreatedAt: pgtype.Timestamptz{ Time: cursor.LastSeenTime, Valid: true },
			ID: pgtype.UUID{ Bytes: cursor.LastSeenDocument, Valid: true },
			Limit: pageSize,
			PermissionsList: repoPermissionList,
		}
		rows, err := dr.queries.ListDocumentsByCreatedAt(ctx, params)
		if err != nil {
			return nil, service.RepoImpl("failed to retrieve document by principal", err)
		}
		for _, row := range rows {
			documentPermission, err := parseDocumentPermission(row.Document, row.PermissionLevel)
			if err != nil {
				return nil, err
			} else {
				documentPermissionList = append(documentPermissionList, *documentPermission)
			}
		}
	case service.LastModifiedAt:
		params := sqlc.ListDocumentsByLastModifiedAtParams{
			RecipientID: pgtype.UUID{ Bytes: principalId, Valid: true},
			LastModifiedAt: pgtype.Timestamptz{ Time: cursor.LastSeenTime, Valid: true },
			ID: pgtype.UUID{ Bytes: cursor.LastSeenDocument, Valid: true },
			Limit: pageSize,
			PermissionsList: repoPermissionList,
		}
		rows, err := dr.queries.ListDocumentsByLastModifiedAt(ctx, params)
		if err != nil {
			return nil, service.RepoImpl("failed to retrieve document by principal", err)
		}
		for _, row := range rows {
			documentPermission, err := parseDocumentPermission(row.Document, row.PermissionLevel)
			if err != nil {
				return nil, err
			} else {
				documentPermissionList = append(documentPermissionList, *documentPermission)
			}
		}
	}
	return documentPermissionList, nil
}

/*
What does this function do:
- parse the user input:
	- cursor
	- list of permissions
- read from the database based on the contents of the cursor
- parse the returned values into a new format
- construct a new cursor
- return the parsed documents or any errors
*/
func (dr *DocumentRepository) ListDocumentsByPrincipal(
	ctx context.Context,
	principalId uuid.UUID, 
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
	cursorResp = &service.Cursor{
		SortField: cursor.SortField,
	}
	// read from the database
	documentPermissions, err = dr.readDocuments(ctx, principalId, repoPermissionsList, cursor, pageSize)
	if err != nil {
		return nil, nil, err
	}
	// populate the new
	if len(documentPermissions) > 0 {
		if cursorResp.SortField == service.CreatedAt {
			cursorResp.LastSeenTime = documentPermissions[len(documentPermissions) - 1].Document.CreatedAt
		} else {
			cursorResp.LastSeenTime = documentPermissions[len(documentPermissions) - 1].Document.LastModifiedAt
		}
		cursorResp.LastSeenDocument = documentPermissions[len(documentPermissions) - 1].Document.ID
	} else {
		cursorResp.LastSeenTime = cursor.LastSeenTime
		cursorResp.LastSeenDocument = cursor.LastSeenDocument
	}

	return documentPermissions, cursorResp, nil
}

func (dr *DocumentRepository) GetPermissionOfPrincipalOnDocument(
	ctx context.Context,
	documentId uuid.UUID,
	principalId uuid.UUID,
) (permission service.Permission, err error) {
	// get the permission of a user or a guest on a document
	// return a not found error if that principal has no permissions on that document
	params := sqlc.GetPermissionOfPrincipalOnDocumentParams{
		DocumentID: pgtype.UUID{ Bytes: documentId, Valid: true },
		RecipientID: pgtype.UUID{ Bytes: principalId, Valid: true },
	}
	row, err := dr.queries.GetPermissionOfPrincipalOnDocument(
		ctx,
		params,
	)
	if err != nil {
		// check for no rows found
		if errors.Is(err, pgx.ErrNoRows) {
			return -1, service.NotFound(
				fmt.Sprintf(
					"no permissions found for principal: %s on document: %s",
					principalId.String(),
					documentId.String(),
				),
				err,
			)
		} else {
			return -1, service.RepoImpl(
				fmt.Sprintf(
					"failed to get permission for principal: %s on document: %s",
					principalId.String(),
					documentId.String(),
				),
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

// TODO: this function should be paginated using a cursor
func (dr *DocumentRepository) ListPermissionsOnDocument(
	ctx context.Context,
	documentId uuid.UUID,
) (recipientPermissions []service.RecipientPermission, err error) {
	// get the recipient permission rows from the database
	repoRecipientPermissions, err := dr.queries.ListPermissionsOnDocument(
		ctx, pgtype.UUID{ Bytes: documentId, Valid: true},
	)
	// return errors if necessary
	if err != nil {
		return nil, service.RepoImpl(
			fmt.Sprintf("failed to read permissions on document: %s", documentId.String()),
			err,
		)	
	}
	// reformat them from repo to service format
	recipientPermissions = make([]service.RecipientPermission, len(recipientPermissions))
	for i, elem := range repoRecipientPermissions {
		servicePermission, err := repoToServicePermission(elem.PermissionLevel)
		if err != nil {
			// TODO: log the error
			// no partial failures, we should always fail when one of the elements in a list is invalid
			// this makes failures visible to the calling code
			return nil, service.RepoImpl(
				fmt.Sprintf(
					"failed to parse the permission stored in the database for document: %s, principal: %s", 
					documentId.String(),
					elem.RecipientID.String(),
				),
				err,
			)
		}
		recipientId, err := uuid.FromBytes(elem.RecipientID.Bytes[:])
		if err != nil {
			// TODO: log the error
			return nil, service.RepoImpl(
				fmt.Sprintf(
					"failed to parse the recipient id returned by the database: %s",
					elem.RecipientID.String(),
				),
				err,
			)
		}
		recipientPermissions[i] = service.RecipientPermission{
			RecipientId: recipientId,
			Permission: servicePermission,
			CreatedAt: elem.CreatedAt.Time,
			LastModifiedAt: elem.LastModifiedAt.Time,
		}
	}
	return recipientPermissions, nil
}

func (dr *DocumentRepository) CreateGuest(
	ctx context.Context, 
	creatorId uuid.UUID,
	documentId uuid.UUID,
	permission service.Permission,
) (guestId uuid.UUID, err error) {
	// generate a new uuid for the guest
	guestId = uuid.New()
	repoPermission, err := serviceToRepoPermission(permission)
	if err != nil {
		return uuid.Nil, service.InvalidInput(
			fmt.Sprintf("invalid input for permission: %v", permission),
			err,
		)
	}
	// get a transaction
	tx, err := dr.pool.Begin(ctx)
	if err != nil {
		return uuid.Nil, service.RepoImpl("failed to create a transaction when creating a guest", err)
	}
	defer tx.Rollback(ctx)
	txQueries := dr.queries.WithTx(tx)
	// add a new guest to the guests table
	params := sqlc.CreateGuestParams{
		ID: pgtype.UUID{ Bytes: guestId, Valid: true },
		DocumentID: pgtype.UUID{ Bytes: documentId, Valid: true },
		CreatedBy: pgtype.UUID{ Bytes: creatorId, Valid: true },
	}
	err = txQueries.CreateGuest(ctx, params)
	if err != nil {
		var pgError *pgconn.PgError
		if errors.As(err, &pgError) {
			if pgError.Code == conflictErrorCode {
				return uuid.Nil, service.UniqueConflict(
					fmt.Sprintf("unique conflict encountered when creating guest with id: %s", guestId.String()),
					err,
				)
			} else {
				return uuid.Nil, service.RepoImpl("encountered a postgres error when trying to create a user", err)
			}
		} else {
			return uuid.Nil, service.RepoImpl("encountered an unexpected error when creating a user", err)
		}
	}
	// add a new permission record to the permissions table associated with that guest
	paramsPermission := sqlc.InsertPermissionGuestParams{
		RecipientID: pgtype.UUID{ Bytes: guestId, Valid: true },
		DocumentID: pgtype.UUID{ Bytes: documentId, Valid: true },
		PermissionLevel: repoPermission,
		CreatedBy: pgtype.UUID{ Bytes: creatorId, Valid: true },
	}
	err = txQueries.InsertPermissionGuest(ctx, paramsPermission)
	if err != nil {
		var pgError *pgconn.PgError
		if errors.As(err, &pgError) {
			if pgError.Code == conflictErrorCode {
				return uuid.Nil, service.UniqueConflict(
					fmt.Sprintf(
						"unique conflict encountered when creating permission on document: %s, for guest with id: %s",
						documentId.String(),
						guestId.String(),
					),
					err,
				)
			} else {
				return uuid.Nil, service.RepoImpl("encountered a postgres error when trying to create a permission", err)
			}
		} else {
			return uuid.Nil, service.RepoImpl("encountered an unexpected error when creating a permission", err)
		}
	}
	// commit the transaction
	err = tx.Commit(ctx)
	if err != nil {
		return uuid.Nil, service.RepoImpl("failed to commit transaction", err)
	}
	return guestId, nil
}

func (dr *DocumentRepository) UpsertPermissionsUser(
	ctx context.Context, 
	userId uuid.UUID, 
	documentId uuid.UUID, 
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
		RecipientID: pgtype.UUID{ Bytes: userId, Valid: true },
		DocumentID: pgtype.UUID{ Bytes: documentId, Valid: true },
		PermissionLevel: repoPermission,
		CreatedBy: pgtype.UUID{ Bytes: userId, Valid: true },
	}
	err = dr.queries.UpsertPermissionUser(ctx, params)
	if err != nil {
		return service.RepoImpl("failed to update user permission", err)
	}
	return nil
}

func (dr *DocumentRepository) UpdatePermissionGuest(
	ctx context.Context,
	guestId uuid.UUID,
	documentId uuid.UUID,
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
		RecipientID: pgtype.UUID{ Bytes: guestId, Valid: true },
		DocumentID: pgtype.UUID{ Bytes: documentId, Valid: true },
		PermissionLevel: permissionRepo,
	}
	count, err := dr.queries.UpdatePermissionGuest(ctx, params)
	if err != nil {
		return service.RepoImpl("failed to update guest permissions", err)
	}
	if count < 1 {
		return service.NotFound(
			fmt.Sprintf(
				"unable to find permission of guest: %s on document: %s",
				guestId.String(),
				documentId.String(),
			),
			nil,
		)
	}
	return nil
}

func (dr *DocumentRepository) DeletePermissionsPrincipal(
	ctx context.Context,
	recipientId uuid.UUID,
	documentId uuid.UUID,
) (err error) {
	// let the code at the service level decide if we should be able to delete the owner of 
	// a documents permissions on that document. This business logic does not need to be
	// enforced in two places
	params := sqlc.DeletePermissionPrincipalParams{
		RecipientID: pgtype.UUID{ Bytes: recipientId, Valid: true },
		DocumentID: pgtype.UUID{ Bytes: documentId, Valid: true },
	}
	count, err := dr.queries.DeletePermissionPrincipal(ctx, params)
	if err != nil {
		return service.RepoImpl(
			fmt.Sprintf(
				"error encountered when deleting permissions of %s on document %s",
				recipientId.String(),
				documentId.String(),
			),
			err,
		)
	}
	if count < 1 {
		return service.NotFound(
			fmt.Sprintf(
				"no permission found when deleting permission with recipient: %s and document %s",
				recipientId.String(),
				documentId.String(),
			),
			nil,
		)
	}
	return nil
}