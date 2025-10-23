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

// validate at compile time that the repository.DocumentRepository struct conforms to the 
// service.DocumentRepository interface
var _ service.DocumentRepository = (*DocumentRepository)(nil)
// ^this is a type conversion of the format (type)(value)
// we are assigning a nil pointer as a pointer to a repository.DocumentRepository
// variable. This checks at runtime if the repository.DocumentRepository struct type 
// implements the methods in the document repository interface
// I really like this neat trick

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

func serviceToRepoPermissionLevel(
	permissionService service.PermissionLevel,
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

func repoToServicePermissionLevel(
	permissionRepo sqlc.PermissionLevel,
) (service.PermissionLevel, error) {
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

func repoToServicePermission(
	permissionRepo sqlc.Permission,
) (service.Permission, error) {
	errorSuffix := fmt.Sprintf(
		" of recipient: %s on document: %s", 
		permissionRepo.RecipientID.String(), 
		permissionRepo.DocumentID.String(),
	)
	permissionLevel, err := repoToServicePermissionLevel(permissionRepo.PermissionLevel)
	if err != nil {
		return service.Permission{}, service.RepoImpl("failed to parse permission level" + errorSuffix, err)
	}
	serviceRecipientType, err := repoToServiceRecipientType(permissionRepo.RecipientType)
	if err != nil {
		return service.Permission{}, service.RepoImpl("failed to parse recipient type" + errorSuffix, err)
	}
	recipientId, err := uuid.FromBytes(permissionRepo.RecipientID.Bytes[:])
	if err != nil {
		return service.Permission{}, service.RepoImpl("failed to parse the recipient id" + errorSuffix, err)
	}
	documentId, err := uuid.FromBytes(permissionRepo.DocumentID.Bytes[:])
	if err != nil {
		return service.Permission{}, service.RepoImpl("failed to parse the document id" + errorSuffix, err)
	}
	creatorId, err := uuid.FromBytes(permissionRepo.CreatedBy.Bytes[:])
	if err != nil {
		return service.Permission{}, service.RepoImpl("failed to parse created by id" + errorSuffix, err)
	}
	return service.Permission{
		RecipientID: recipientId,
		RecipientType: serviceRecipientType,
		DocumentID: documentId,
		PermissionLevel: permissionLevel,
		CreatedBy: creatorId,
		CreatedAt: permissionRepo.CreatedAt.Time,
		LastModifiedAt: permissionRepo.CreatedAt.Time,
	}, nil
}

func repoToServiceRecipientType(
	recipientTypeRepo sqlc.RecipientType,
) (service.RecipientType, error) {
	switch recipientTypeRepo {
	case sqlc.RecipientTypeUser:
		return service.User, nil
	case sqlc.RecipientTypeGuest:
		return service.Guest, nil
	default:
		return -1, fmt.Errorf("failed to match any of the valid recipient types")
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
	permissionLevelService, err := repoToServicePermissionLevel(permissionLevel)
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
		Permission: permissionLevelService,
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
			ID: pgtype.UUID{ Bytes: cursor.LastSeenID, Valid: true },
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
			ID: pgtype.UUID{ Bytes: cursor.LastSeenID, Valid: true },
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
	permissions []service.PermissionLevel,
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
	for _, permissionLevel := range permissions {
		repoPermissionLevel, err := serviceToRepoPermissionLevel(permissionLevel)
		if err != nil {
			return nil, nil, service.InvalidInput(
				fmt.Sprintf("input permission: %v does not map to any valid permissions", permissionLevel), nil,
			)
		}
		repoPermissionsList = append(repoPermissionsList, repoPermissionLevel)
	}
	cursorResp = &service.Cursor{
		SortField: cursor.SortField,
	}
	// read from the database
	documentPermissions, err = dr.readDocuments(ctx, principalId, repoPermissionsList, cursor, pageSize)
	if err != nil {
		return nil, nil, err
	}
	// populate the new cursor
	if len(documentPermissions) > 0 {
		if cursorResp.SortField == service.CreatedAt {
			cursorResp.LastSeenTime = documentPermissions[len(documentPermissions) - 1].Document.CreatedAt
		} else {
			cursorResp.LastSeenTime = documentPermissions[len(documentPermissions) - 1].Document.LastModifiedAt
		}
		cursorResp.LastSeenID = documentPermissions[len(documentPermissions) - 1].Document.ID
	} else {
		cursorResp.LastSeenTime = cursor.LastSeenTime
		cursorResp.LastSeenID = cursor.LastSeenID
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
			return service.Permission{}, service.NotFound(
				fmt.Sprintf(
					"no permissions found for principal: %s on document: %s",
					principalId.String(),
					documentId.String(),
				),
				err,
			)
		} else {
			return service.Permission{}, service.RepoImpl(
				fmt.Sprintf(
					"failed to get permission for principal: %s on document: %s",
					principalId.String(),
					documentId.String(),
				),
				err,
			)
		}
	}
	return repoToServicePermission(row)
}

// call the relevant list permissions on document helper function with the correct cursor input
// parse service permissions objects from the sqlc permission objects
// create a response cursor
// return the parsed permissions and the cursor
// func (dr *DocumentRepository) readPermissions(
// 	ctx context.Context,

// )

/*
CHECKPOINT:
- you were modifying the sql queries so that they were paginated for listing permissions by document id
  using either the created at or the last modified at fields as sort keys in descending order with the
  recipientID as the tiebreaker
	- this works because we are always searching on the document id
	- might be worth it to just make two cursor objects then?
	- the trade off is code duplication for readability? I think the code duplication is minimal so
	  it is worth it
- after that you were going to change the repo implementation to be paginated and return a cursor
*/
func (dr *DocumentRepository) readPermissions(
	ctx context.Context,
	documentId uuid.UUID,
	permissionFilter []sqlc.PermissionLevel,
	cursor *service.Cursor,
	maxPermissions int32,
) (repoPermissions []sqlc.Permission, err error) {
	switch cursor.SortField {
	case service.CreatedAt:
		params := sqlc.ListPermissionOnDocumentCreatedAtParams{
			DocumentID: pgtype.UUID{ Bytes: documentId, Valid: true },
			CreatedAt: pgtype.Timestamptz{ Time: cursor.LastSeenTime, Valid: true },
			RecipientID: pgtype.UUID{ Bytes: cursor.LastSeenID, Valid: true },
			Limit: maxPermissions,
			PermissionsList: permissionFilter,
		}
		repoPermissions, err = dr.queries.ListPermissionOnDocumentCreatedAt(ctx, params)
		if err != nil {
			return nil, service.RepoImpl(fmt.Sprintf("failed to retrieve permissions on document %s", documentId.String()), err)
		}
	case service.LastModifiedAt:
		params := sqlc.ListPermissionOnDocumentLastModifiedAtParams{
			DocumentID: pgtype.UUID{ Bytes: documentId, Valid: true },
			LastModifiedAt: pgtype.Timestamptz{ Time: cursor.LastSeenTime, Valid: true },
			RecipientID: pgtype.UUID{ Bytes: cursor.LastSeenID, Valid: true },
			Limit: maxPermissions,
			PermissionsList: permissionFilter,
		}
		repoPermissions, err = dr.queries.ListPermissionOnDocumentLastModifiedAt(ctx, params)
		if err != nil {
			return nil, service.RepoImpl(fmt.Sprintf("failed to retrieve permissions on document %s", documentId.String()), err)
		}
	}
	return repoPermissions, nil
}

func (dr *DocumentRepository) ListPermissionsOnDocument(
	ctx context.Context,
	documentId uuid.UUID,
	permissionFilter []service.PermissionLevel,
	cursor *service.Cursor,
	pageSize int32,
) (permissions []service.Permission, respCursor *service.Cursor, err error) {
	// parse the permission filters
	repoPermissionFilter := make([]sqlc.PermissionLevel, len(permissionFilter))
	for i, pl := range permissionFilter {
		rpl, err := serviceToRepoPermissionLevel(pl)
		if err != nil {
			return nil, nil, service.RepoImpl("failed to parse permission filter", err)
		}
		repoPermissionFilter[i] = rpl
	}
	// get the recipient permission rows from the database
	repoPermissions, err := dr.readPermissions(
		ctx, documentId, repoPermissionFilter, cursor, pageSize,
	)
	// return errors if necessary
	if err != nil {
		return nil, nil, service.RepoImpl(
			fmt.Sprintf("failed to read permissions on document: %s", documentId.String()),
			err,
		)	
	}
	// reformat them from repo to service format
	permissions = make([]service.Permission, len(repoPermissions))
	for i, elem := range repoPermissions {
		servicePermission, err := repoToServicePermission(elem)
		if err != nil {
			return nil, nil, err
		}
		permissions[i] = servicePermission
	}
	// construct a return cursor
	// if we retrieved previously unseen permissions, then update the cursor with the new permission 
	// information, else, we update it with the previously seen cursor information
	respCursor = &service.Cursor{ SortField: cursor.SortField }
	if len(permissions) > 0 {
		respCursor.LastSeenID = permissions[len(permissions) - 1].RecipientID
		switch cursor.SortField {
		case service.CreatedAt:
			respCursor.LastSeenTime = permissions[len(permissions) - 1].CreatedAt
		case service.LastModifiedAt:
			respCursor.LastSeenTime = permissions[len(permissions) - 1].LastModifiedAt
		}
	} else {
		respCursor.LastSeenID = cursor.LastSeenID
		respCursor.LastSeenTime = cursor.LastSeenTime
	}
	return permissions, respCursor, nil
}

func (dr *DocumentRepository) CreateGuest(
	ctx context.Context, 
	creatorId uuid.UUID,
	documentId uuid.UUID,
	permissionLevel service.PermissionLevel,
) (guestId uuid.UUID, err error) {
	// generate a new uuid for the guest
	guestId = uuid.New()
	repoPermission, err := serviceToRepoPermissionLevel(permissionLevel)
	if err != nil {
		return uuid.Nil, service.InvalidInput(
			fmt.Sprintf("invalid input for permission: %v", permissionLevel),
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
	permissionLevel service.PermissionLevel,
) (err error) {
	repoPermission, err := serviceToRepoPermissionLevel(permissionLevel)
	if err != nil {
		return service.InvalidInput(
			fmt.Sprintf("invalid input for permission: %d", permissionLevel),
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
	permissionLevel service.PermissionLevel,
) (err error) {
	permissionRepo, err := serviceToRepoPermissionLevel(permissionLevel)
	if err != nil {
		return service.InvalidInput(
			fmt.Sprintf("invalid input received for permission: %d", permissionLevel),
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