package service

import (
	"context"
	"time"
	"fmt"

	"github.com/google/uuid"
)

// TODO: come up with a definitive approach for creating enums
//		 in the mean time, use integer values of named types in memory
//		 but use strings in the database and when returning to the caller.
//		 this balances the simplicity of using integers with the readability
//		 of using strings
type PermissionLevel int32
const (
	Viewer PermissionLevel = iota
	Editor 
	Owner
)

var AllPermissions []PermissionLevel = []PermissionLevel{
	Viewer, Editor, Owner,
}

type RecipientType int32
const (
	User RecipientType = iota
	Guest
)

type SortField int32
const (
	CreatedAt SortField = iota
	LastModifiedAt
)

type Document struct {
	ID uuid.UUID
	Name *string
	Description *string
	CreatedAt time.Time
	LastModifiedAt time.Time
}

type Permission struct {
	RecipientID uuid.UUID
	RecipientType RecipientType
	DocumentID uuid.UUID
	PermissionLevel PermissionLevel
	CreatedBy uuid.UUID
	CreatedAt time.Time
	LastModifiedAt time.Time
}

type Cursor struct {
	SortField SortField
	LastSeenTime time.Time
	LastSeenID uuid.UUID
}

const DefaultPageSize int32 = 10
const MaxPageSize int32 = 100

type DocumentPermission struct {
	Document Document
	Permission PermissionLevel
}

func MaxDocumentID() uuid.UUID {
    var maxUUID uuid.UUID
    for i := range maxUUID {
        maxUUID[i] = 0xff
    }
    return maxUUID
}

func NewBeginningCursor(sortField SortField) *Cursor {
	return &Cursor{
		SortField: sortField,
		LastSeenTime: time.Now(),
		LastSeenID: MaxDocumentID(),
	}
}

/*
Open questions:
- should the calling code or the repository be in charge of generating
  UUIDs?
	- leave the generation of UUIDs to the repository so that the repository
	  can optionally choose implementation specific uuids like uuid7
	- if we ever want to migrate to a system with more explicit generation of
	  ids or a central id provider then generating ids at the service level
	  will be preferable
*/

type DocumentRepository interface {
	CreateDocument(ctx context.Context, userId uuid.UUID, documentName *string, documentDescription *string) (documentId uuid.UUID, err error)
	GetDocument(ctx context.Context, documentId uuid.UUID) (document *Document, err error)
	UpdateDocument(ctx context.Context, documentId uuid.UUID, documentName *string, documentDescription *string) (err error)
	DeleteDocument(ctx context.Context, documentId uuid.UUID) (err error)
	DeleteDocuments(ctx context.Context, documentIds uuid.UUIDs, userId uuid.UUID) (err error)
	// list the documents that are associated with that user at those permission levels
	ListDocumentsByPrincipal(ctx context.Context, principalId uuid.UUID, permissions []PermissionLevel, cursor *Cursor, pageSize int32) (documentPermissions []DocumentPermission, cursorResp *Cursor, err error)
	GetPermissionOfPrincipalOnDocument(ctx context.Context, documentId uuid.UUID, principalId uuid.UUID) (permission Permission, err error)
	// consider if we also want to be able to filter on user type here
	ListPermissionsOnDocument(ctx context.Context, documentId uuid.UUID, permissions []PermissionLevel, cursor *Cursor, pageSize int32) (recipientPermissions []Permission, cursorResp *Cursor, err error)
	CreateGuest(ctx context.Context, creatorId uuid.UUID, documentId uuid.UUID, permission PermissionLevel) (guestId uuid.UUID, err error)
	UpsertPermissionUser(ctx context.Context, userId uuid.UUID, documentId uuid.UUID, permission PermissionLevel) (err error)
	UpdatePermissionGuest(ctx context.Context, guestId uuid.UUID, permission PermissionLevel) (err error)
	DeletePermissionsPrincipal(ctx context.Context, recipientId uuid.UUID, documentId uuid.UUID) (err error)
}

type DocumentService struct {
	documentRepo DocumentRepository
}

func NewDocumentService(documentRepo DocumentRepository) *DocumentService {
	return &DocumentService{
		documentRepo: documentRepo,
	}
}

func (ds *DocumentService) CreateDocument(
	ctx context.Context,
	ownerUserId uuid.UUID,
	documentName *string,
	documentDescription *string,
) (uuid.UUID, error) {
	// this is an internal api that will be called by the api gateway layer. We can expect that
	// the owner userId is a valid Id without checking with the user service
	documentId, err := ds.documentRepo.CreateDocument(ctx, ownerUserId, documentName, documentDescription)
	if err != nil {
		// err.(DomainError) syntax does not check all the way down the error chain but instead 
		// checks the type of the top error. We want to use this syntax because our goal is to wrap
		// errors in a domain error instead of find the identity of the error
		if _, ok := err.(DomainError); !ok {
			err = RepoImpl("unexpected error encountered when creating document", err)
		}
	}
	return documentId, err
}

func (ds *DocumentService) GetDocument(
	ctx context.Context,
	documentId uuid.UUID,
) (*Document, error) {
	document, err := ds.documentRepo.GetDocument(ctx, documentId)
	if err != nil {
		// this is a runtime type assertion
		// it works on interfaces (the error interface). if it is successful it will return a concrete
		// domain error struct.
		// This is different from a compile time type conversion. Compile time type conversions work
		// on concrete types and do not return errors
		if _, ok := err.(DomainError); !ok {
			err = RepoImpl("unexpected error encountered when getting document", err)
		}
	}
	return document, err
}

func (ds *DocumentService) UpdateDocument(
	ctx context.Context,
	documentId uuid.UUID,
	documentName *string,
	documentDescription *string,
) (err error) {
	if documentName == nil && documentDescription == nil {
		return InvalidInput("at least one of documentName or documentDescription must be provided to update document", nil)
	}
	err = ds.documentRepo.UpdateDocument(ctx, documentId, documentName, documentDescription)
	if err != nil {
		if _, ok := err.(DomainError); !ok {
			err = RepoImpl("unexpected error when updating document", err)
		}
	}
	return err
}

func (ds *DocumentService) DeleteDocument(
	ctx context.Context,
	documentId uuid.UUID,
) (err error) {
	err = ds.documentRepo.DeleteDocument(ctx, documentId)
	if err != nil {
		if _, ok := err.(DomainError); !ok {
			err = RepoImpl("unexpected error when deleting document", err)
		}
	}
	return err
}

func (ds *DocumentService) DeleteDocuments(
	ctx context.Context,
	documentIds uuid.UUIDs,
	userId uuid.UUID,
) (err error) {
	err = ds.documentRepo.DeleteDocuments(ctx, documentIds, userId)
	if err != nil{
		if _, ok := err.(DomainError); !ok {
			err = RepoImpl("unexpected error when deleting documents", err)
		}
	}
	return err
}

func (ds *DocumentService) ListDocumentsByPrincipal(
	ctx context.Context,
	principalId uuid.UUID,
	permissions []PermissionLevel, 
	cursor *Cursor,
	pageSize int32,
) (documentPermissions []DocumentPermission, cursorResp *Cursor, err error) {
	// validate the inputs and replace them with default values where necessary
	// if the list of permissions is empty, replace it with the default value (all permissions)
	if len(permissions) < 1 {
		permissions = AllPermissions
	}
	// if the cursor is empty, replace it with the default starting cursor
	if cursor == nil {
		cursor = NewBeginningCursor(CreatedAt)
	}
	// if the page size is -1, replace it with the default page size
	// if the page size is too large, replace it with the default page size
	if pageSize < 1 || pageSize > MaxPageSize {
		pageSize = DefaultPageSize
	}
	// call the relevant document repo function
	documentPermissions, cursorResp, err = ds.documentRepo.ListDocumentsByPrincipal(
		ctx,
		principalId,
		permissions,
		cursor,
		pageSize,
	)
	if err != nil {
		if _, ok := err.(DomainError); !ok {
			err = RepoImpl("unexpected error found when listing documents by principal", err)
		}
		return nil, nil, err
	}
	return documentPermissions, cursorResp, nil
}

func (ds *DocumentService) GetPermissionOfPrincipalOnDocument(
	ctx context.Context,
	documentId uuid.UUID,
	principalId uuid.UUID,
) (permission Permission, err error) {
	permission, err = ds.documentRepo.GetPermissionOfPrincipalOnDocument(
		ctx, documentId, principalId,
	)
	if err != nil {
		if _, ok := err.(DomainError); !ok {
			err = RepoImpl("unexpected error found when getting permission", err)
		}
	}
	return permission, err
}

func (ds *DocumentService) ListPermissionsOnDocument(
	ctx context.Context,
	documentId uuid.UUID,
	permissions []PermissionLevel,
	cursor *Cursor,
	pageSize int32,
) (recipientPermissions []Permission, cursorResp *Cursor, err error) {
	// if the list of permissions is empty, replace it with the permissive list of permissions
	if len(permissions) < 1 {
		permissions = AllPermissions
	}
	// if the cursor is a nil pointer, replace it with the default beginning cursor
	if cursor == nil {
		cursor = NewBeginningCursor(CreatedAt)
	}
	// if the pagesize is out of bounds, replace it with the default page size 
	if pageSize < 1 || pageSize > MaxPageSize {
		pageSize = DefaultPageSize
	}
	// call the relevant repo method
	recipientPermissions, cursorResp, err = ds.documentRepo.ListPermissionsOnDocument(
		ctx, documentId, permissions, cursor, pageSize,
	)
	// conditionally wrap the error
	if err != nil {
		if _, ok := err.(DomainError); !ok {
			err = RepoImpl("unexpected error found when listing permissions on document", err)
		}
	}
	return recipientPermissions, cursorResp, err
}

func (ds *DocumentService) CreateGuest(
	ctx context.Context,
	creatorId uuid.UUID,
	documentId uuid.UUID,
	permissionLevel PermissionLevel,
) (guestId uuid.UUID, err error) {
	// verify that the permission level is one of the valid permission levels for a guest
	if permissionLevel == Owner {
		return uuid.Nil, InvalidInput(
			fmt.Sprintf(
				"failed to create guest because guests cannot have this permission level: %v",
				permissionLevel,
			), 
			nil,
		)
	}
	// call the correct repo function
	guestId, err = ds.documentRepo.CreateGuest(
		ctx, creatorId, documentId, permissionLevel,
	)
	if err != nil {
		if _, ok := err.(DomainError); !ok {
			err = RepoImpl("failed to create guest with unknown error", err)
		}
	}
	return guestId, err
}

func (ds *DocumentService) UpsertPermissionUser(
	ctx context.Context, 
	userId uuid.UUID,
	documentId uuid.UUID,
	permissionLevel PermissionLevel,
) (err error) {
	// validate the permission level
	if permissionLevel == Owner {
		return InvalidInput("cannot grant owner permission to user other than by creating a document with that user", nil)
	}
	// call the relevant repo function
	err = ds.documentRepo.UpsertPermissionUser(
		ctx, userId, documentId, permissionLevel,
	)
	// conditionally wrap the error output 
	if err != nil {
		if _, ok := err.(DomainError); !ok {
			err = RepoImpl("failed to upsert permission on user with unknown error", err)
		}
	}
	return err
}

func (ds *DocumentService) UpdatePermissionGuest(
	ctx context.Context,
	guestId uuid.UUID,
	permissionLevel PermissionLevel,
) (err error) {
	// validate the permission level
	if permissionLevel == Owner {
		return InvalidInput("cannot grant owner permission to a guest", nil)
	}
	// call the relevant repo function
	err = ds.documentRepo.UpdatePermissionGuest(
		ctx, guestId, permissionLevel,
	)
	// conditionally wrap the error
	if err != nil {
		if _, ok := err.(DomainError); !ok {
			err = RepoImpl("unknown error found when updating guest permission level", err)
		}
	}
	return err
}

func (ds *DocumentService) DeletePermissionPrincipal(
	ctx context.Context,
	recipientId uuid.UUID,
	documentId uuid.UUID,
) (err error) {
	err = ds.documentRepo.DeletePermissionsPrincipal(
		ctx, recipientId, documentId,
	)
	if err != nil {
		if _, ok := err.(DomainError); !ok {
			err = RepoImpl("unexpected error encountered when deleting permission of principal", err)
		}
	}
	return err
}