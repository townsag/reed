package service

import (
	"context"
	"time"

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
	// list the documents that are associated with that user at those permission levels
	ListDocumentsByPrincipal(ctx context.Context, principalId uuid.UUID, permissions []PermissionLevel, cursor *Cursor, pageSize int32) (documentPermissions []DocumentPermission, cursorResp *Cursor, err error)
	GetPermissionOfPrincipalOnDocument(ctx context.Context, documentId uuid.UUID, principalId uuid.UUID) (permission Permission, err error)
	// consider if we also want to be able to filter on user type here
	ListPermissionsOnDocument(ctx context.Context, documentId uuid.UUID, permissions []PermissionLevel, cursor *Cursor, pageSize int32) (recipientPermissions []Permission, cursorResp *Cursor, err error)
	CreateGuest(ctx context.Context, creatorId uuid.UUID, documentId uuid.UUID, permission PermissionLevel) (guestId uuid.UUID, err error)
	UpsertPermissionsUser(ctx context.Context, userId uuid.UUID, documentId uuid.UUID, permission PermissionLevel) (err error)
	UpdatePermissionGuest(ctx context.Context, guestId uuid.UUID, documentId uuid.UUID, permission PermissionLevel) (err error)
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
	// 
	documentId, err := ds.documentRepo.CreateDocument(ctx, ownerUserId, documentName, documentDescription)
	if err != nil {
		// var domainError DomainError
		// // &domainError is a reference to an interface, since interfaces are reference like
		// // types then we do not have to use a pointer to an interface. We do have to pass the
		// // address of the interface type so that we can modify the pointer to be pointing at
		// // a different piece of memory
		// if !errors.As(err, &domainError) {
		// 	err = RepoImpl("unexpected error encountered when creating document", err)
		// }
		// err.(DomainError) syntax does not check all the way down the error chain but instead 
		// checks the type of the top error. We want to use this syntax because our goal is to wrap
		// errors in a domain error instead of find the identity of the error
		if _, ok := err.(DomainError); !ok {
			err = RepoImpl("unexpected error encountered when creating document", err)
		}
	}
	return documentId, err
}