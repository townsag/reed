package document_repository_test

import (
	"testing"
	"time"
	"errors"

	"github.com/google/uuid"

	"github.com/townsag/reed/document_service/internal/repository"
	"github.com/townsag/reed/document_service/internal/service"
)

// ========== ListDocumentsByPrincipal: Observe Permission Mutations ========== //
// in this test we validate that the returned cursor is correct, in future tests of this function
// we do not need to validate the returned cursor. We validate both the nil cursor case and the
// found document permissions case
// func TestCreateDocument_ViewByUserId_DeleteDocument_Integration(t *testing.T) {
func TestListDocumentsByPrincipal_OnUser_DeleteDocumentPath_Integration(t *testing.T) {
	// create a document repository with a connection to the postgres instance 
	documentRepo := createTestingDocumentRepo(t)
	// create a dummy user id
	userId := uuid.New()
	// create a document for that user
	documentId, err := documentRepo.CreateDocument(t.Context(), userId, nil, nil)
	if err != nil {
		t.Fatalf("failed to create a document with error: %v", err)
	}
	// get that document so that we have its created at time
	document, err := documentRepo.GetDocument(t.Context(), documentId)
	if err != nil {
		t.Fatalf("failed to get created document with error: %v", err)
	}
	// create a list of permissions and a an empty cursor to call the ListDocumentsByPrincipal function with
	permissionsFilter := []service.PermissionLevel{service.Editor, service.Owner, service.Viewer}
	// use the current time as the last seen time, the cursor will start at the last seen time
	// then traverse the created at index in descending order
	cursor := service.Cursor{ SortField: service.CreatedAt, LastSeenTime: time.Now(), LastSeenID: service.MaxDocumentID() }
	// view that document in the response from ListDocumentsByPrincipal
	documentPermissions, respCursor, err := documentRepo.ListDocumentsByPrincipal(t.Context(), userId, permissionsFilter, &cursor, 10)
	if err != nil {
		t.Fatalf("failed to list documents by principal with error: %v", err)
	}
	// verify that the user has owner permissions on the created document
	found := false
	for _, documentPermission := range documentPermissions {
		if documentPermission.Document.ID == documentId {
			found = true
			if documentPermission.Permission != service.Owner {
				t.Errorf(
					"document was created with incorrect permission, want: %v, got: %v",
					service.Owner,
					documentPermission.Permission,
				)
			}
		}
	}
	if !found {
		t.Fatalf("failed to retrieve the created document %s, got this list of document permissions: %v",documentId, documentPermissions)
	}
	// verify that the response cursor is correctly formed
	if respCursor.LastSeenID != documentId {
		t.Errorf("the returned cursor has the wrong last seen document value, want: %v, got: %v", documentId, respCursor.LastSeenID)
	}
	if respCursor.LastSeenTime != document.CreatedAt {
		t.Errorf("the returned cursor has the wrong last seen time value, want %v, got: %v", document.CreatedAt, respCursor.LastSeenTime)
	}
	// delete that document
	err = documentRepo.DeleteDocument(t.Context(), documentId)
	if err != nil {
		t.Fatalf("failed to delete document with error: %v", err)
	}
	// verify that the document cannot be viewed in the result of ListDocumentsByPrincipal
	documentPermissions, respCursor, err = documentRepo.ListDocumentsByPrincipal(t.Context(), userId, permissionsFilter, &cursor, 10)
	if err != nil {
		t.Fatalf("failed to list documents by principal with error: %v", err)
	}
	if len(documentPermissions) > 0 {
		t.Errorf("failed to remove permission on document after deleting the document, expected empty list, got: %v", documentPermissions)
	}
	// verify that the nil response cursor is correct
	if respCursor.LastSeenID != service.MaxDocumentID() {
		t.Errorf("failed to return a correct nil cursor value for last seen document, want: %v, got: %v", service.MaxDocumentID(), respCursor.LastSeenID)
	}
	if respCursor.LastSeenTime != cursor.LastSeenTime {
		t.Errorf(
			"failed to return a correct nil cursor value for last seen time, " + 
			"the expected value is the time that was sent in the request cursor." + 
			"want: %v, got: %v", cursor.LastSeenTime, respCursor.LastSeenTime,
		)
	}
}

// in the previous test we validated that the cursor was well formed. we do not need to
// validate that the cursor is well formed in this test
// func TestCreateDocument_ViewByUserId_DeletePermission_Integration(t *testing.T) {
func TestListDocumentsByPrincipal_OnUser_DeletePermissionPath_Integration(t *testing.T) {
	// create a document repository with a connection to the postgres instance 
	documentRepo := createTestingDocumentRepo(t)
	// create a dummy user id
	userId := uuid.New()
	// create a document for that user
	documentId, err := documentRepo.CreateDocument(t.Context(), userId, nil, nil)
	if err != nil {
		t.Fatalf("failed to create a document with error: %v", err)
	}
	// create a dummy recipient user
	recipientUserId := uuid.New()
	// share the document with the recipient user
	err = documentRepo.UpsertPermissionUser(t.Context(), recipientUserId, documentId, service.Editor)
	if err != nil {
		t.Fatalf("failed to create a permission on a document with error: %v", err)
	}
	// create a list of permissions and a an empty cursor to call the ListDocumentsByPrincipal function with
	permissionsFilter := []service.PermissionLevel{service.Editor, service.Owner, service.Viewer}
	// use the current time as the last seen time, the cursor will start at the last seen time
	// then traverse the created at index in descending order
	cursor := service.Cursor{ SortField: service.CreatedAt, LastSeenTime: time.Now(), LastSeenID: service.MaxDocumentID() }
	// view that document in the response from ListDocumentsByPrincipal for the recipient user
	documentPermissions, _, err := documentRepo.ListDocumentsByPrincipal(t.Context(), recipientUserId, permissionsFilter, &cursor, 10)
	if err != nil {
		t.Fatalf("failed to list documents by principal with error: %v", err)
	}
	// verify that the user has editor permissions on the created document
	found := false
	for _, documentPermission := range documentPermissions {
		if documentPermission.Document.ID == documentId {
			found = true
			if documentPermission.Permission != service.Editor {
				t.Errorf(
					"document was created with incorrect permission, want: %v, got: %v",
					service.Editor,
					documentPermission.Permission,
				)
			}
		}
	}
	if !found {
		t.Fatalf("failed to retrieve the created document %s, got this list of document permissions: %v",documentId, documentPermissions)
	}
	// delete the recipient users permission on the document
	err = documentRepo.DeletePermissionsPrincipal(t.Context(), recipientUserId, documentId)
	if err != nil {
		t.Fatalf("failed to delete permission on a document for the recipient user with error: %v", err)
	}
	// verify that the document cannot be viewed in the result of ListDocumentsByPrincipal
	documentPermissions, _, err = documentRepo.ListDocumentsByPrincipal(t.Context(), recipientUserId, permissionsFilter, &cursor, 10)
	if err != nil {
		t.Fatalf("failed to list documents by principal with error: %v", err)
	}
	if len(documentPermissions) > 0 {
		t.Errorf("failed to remove permission on document after deleting the document, expected empty list, got: %v", documentPermissions)
	}
}

// func TestCreateDocument_ViewByUserId_UpdatePermission_Integration(t *testing.T) {
func TestListDocumentsByPrincipal_OnUser_UpdatePermissionPath_Integration(t *testing.T) {
	// create a document repository with a connection to the postgres instance 
	documentRepo := createTestingDocumentRepo(t)
	// create a dummy user id
	userId := uuid.New()
	// create a document for that user
	documentId, err := documentRepo.CreateDocument(t.Context(), userId, nil, nil)
	if err != nil {
		t.Fatalf("failed to create a document with error: %v", err)
	}
	// create a dummy recipient user
	recipientUserId := uuid.New()
	// share the document with the recipient user
	err = documentRepo.UpsertPermissionUser(t.Context(), recipientUserId, documentId, service.Editor)
	if err != nil {
		t.Fatalf("failed to create a permission on a document with error: %v", err)
	}
	// create a list of permissions and a an empty cursor to call the ListDocumentsByPrincipal function with
	permissionsFilter := []service.PermissionLevel{service.Editor, service.Owner, service.Viewer}
	// use the current time as the last seen time, the cursor will start at the last seen time
	// then traverse the created at index in descending order
	cursor := service.Cursor{ SortField: service.CreatedAt, LastSeenTime: time.Now(), LastSeenID: service.MaxDocumentID() }
	// view that document in the response from ListDocumentsByPrincipal
	documentPermissions, _, err := documentRepo.ListDocumentsByPrincipal(t.Context(), recipientUserId, permissionsFilter, &cursor, 10)
	if err != nil {
		t.Fatalf("failed to list documents by principal with error: %v", err)
	}
	// verify that the user has editor permissions on the created document
	found := false
	for _, documentPermission := range documentPermissions {
		if documentPermission.Document.ID == documentId {
			found = true
			if documentPermission.Permission != service.Editor {
				t.Errorf(
					"document was created with incorrect permission, want: %v, got: %v",
					service.Editor,
					documentPermission.Permission,
				)
			}
		}
	}
	if !found {
		t.Fatalf("failed to retrieve the created document %s, got this list of document permissions: %v",documentId, documentPermissions)
	}
	// modify the recipient users permission on the document
	err = documentRepo.UpsertPermissionUser(t.Context(), recipientUserId, documentId, service.Viewer)
	if err != nil {
		t.Fatalf("failed to update permission on a document for the recipient user with error: %v", err)
	}
	// verify that the document can be viewed in the result of ListDocumentsByPrincipal with the updated permission
	documentPermissions, _, err = documentRepo.ListDocumentsByPrincipal(t.Context(), recipientUserId, permissionsFilter, &cursor, 10)
	if err != nil {
		t.Fatalf("failed to list documents by principal with error: %v", err)
	}
	found = false
	for _, documentPermission := range documentPermissions {
		if documentPermission.Document.ID == documentId {
			found = true
			if documentPermission.Permission != service.Viewer {
				t.Errorf(
					"document was found without the edited permissions, want: %v, got: %v",
					service.Viewer,
					documentPermission.Permission,
				)
			}
		}
	}
	if !found {
		t.Fatalf("failed to retrieve the document by user id after modifying the permissions, got: %v", documentPermissions)
	}
}

// func TestCreateDocument_ViewByGuestId_DeleteDocument_Integration(t *testing.T) {
func TestListDocumentsByPrincipal_OnGuest_DeleteDocumentPath_Integration(t *testing.T) {
	// create a document repository with a connection to the postgres instance 
	documentRepo := createTestingDocumentRepo(t)
	// create a dummy user id
	userId := uuid.New()
	// create a document for that user
	documentId, err := documentRepo.CreateDocument(t.Context(), userId, nil, nil)
	if err != nil {
		t.Fatalf("failed to create a document with error: %v", err)
	}
	// create a guest on that document
	guestId, err := documentRepo.CreateGuest(t.Context(), userId, documentId, service.Editor)
	if err != nil {
		t.Fatalf("failed to create a guest with error: %v", guestId)
	}
	// create a list of permissions and an empty cursor to call the ListDocumentsByPrincipal function with
	permissionsFilter := []service.PermissionLevel{service.Editor, service.Viewer}
	// use the current time as the last seen time, the cursor will start at the last seen time
	// then traverse the created at index in descending order
	cursor := service.Cursor{ SortField: service.CreatedAt, LastSeenTime: time.Now(), LastSeenID: service.MaxDocumentID() }
	// view that document in the response from ListDocumentsByPrincipal for the recipient user
	documentPermissions, _, err := documentRepo.ListDocumentsByPrincipal(t.Context(), guestId, permissionsFilter, &cursor, 10)
	if err != nil {
		t.Fatalf("failed to list documents by principal with error: %v", err)
	}
	// verify that the guest has editor permissions on the created document
	found := false
	for _, documentPermission := range documentPermissions {
		if documentPermission.Document.ID == documentId {
			found = true
			if documentPermission.Permission != service.Editor {
				t.Errorf(
					"document was created with incorrect permission, want: %v, got: %v",
					service.Editor,
					documentPermission.Permission,
				)
			}
		}
	}
	if !found {
		t.Fatalf("failed to retrieve the created document %s, got this list of document permissions: %v",documentId, documentPermissions)
	}
	// delete the document
	err = documentRepo.DeleteDocument(t.Context(), documentId)
	if err != nil {
		t.Fatalf("failed to delete the document with error: %v", err)
	}
	// verify that the document cannot be viewed in the result of ListDocumentsByPrincipal
	documentPermissions, _, err = documentRepo.ListDocumentsByPrincipal(t.Context(), guestId, permissionsFilter, &cursor, 10)
	if err != nil {
		t.Fatalf("failed to list documents by principal with error: %v", err)
	}
	if len(documentPermissions) > 0 {
		t.Errorf("failed to remove permission on document after deleting the document, expected empty list, got: %v", documentPermissions)
	}
}

// func TestCreateDocument_ViewByGuestId_DeletePermission_Integration(t *testing.T) {
func TestListDocumentsByPrincipal_OnGuest_DeletePermissionPath_Integration(t *testing.T) {
	// create a document repository with a connection to the postgres instance 
	documentRepo := createTestingDocumentRepo(t)
	// create a dummy user id
	userId := uuid.New()
	// create a document for that user
	documentId, err := documentRepo.CreateDocument(t.Context(), userId, nil, nil)
	if err != nil {
		t.Fatalf("failed to create a document with error: %v", err)
	}
	// create a guest on that document
	guestId, err := documentRepo.CreateGuest(t.Context(), userId, documentId, service.Editor)
	if err != nil {
		t.Fatalf("failed to create a guest with error: %v", guestId)
	}
	// create a list of permissions and an empty cursor to call the ListDocumentsByPrincipal function with
	permissionsFilter := []service.PermissionLevel{service.Editor, service.Viewer}
	// use the current time as the last seen time, the cursor will start at the last seen time
	// then traverse the created at index in descending order
	cursor := service.Cursor{ SortField: service.CreatedAt, LastSeenTime: time.Now(), LastSeenID: service.MaxDocumentID() }
	// view that document in the response from ListDocumentsByPrincipal for the recipient user
	documentPermissions, _, err := documentRepo.ListDocumentsByPrincipal(t.Context(), guestId, permissionsFilter, &cursor, 10)
	if err != nil {
		t.Fatalf("failed to list documents by principal with error: %v", err)
	}
	// verify that the guest has editor permissions on the created document
	found := false
	for _, documentPermission := range documentPermissions {
		if documentPermission.Document.ID == documentId {
			found = true
			if documentPermission.Permission != service.Editor {
				t.Errorf(
					"document was created with incorrect permission, want: %v, got: %v",
					service.Editor,
					documentPermission.Permission,
				)
			}
		}
	}
	if !found {
		t.Fatalf("failed to retrieve the created document %s, got this list of document permissions: %v",documentId, documentPermissions)
	}
	// delete the guests permission on that document
	err = documentRepo.DeletePermissionsPrincipal(t.Context(), guestId, documentId)
	if err != nil {
		t.Fatalf("failed to delete the guests permission on a document with error: %v", err)
	}
	// verify that the document cannot be viewed in the result of ListDocumentsByPrincipal
	documentPermissions, _, err = documentRepo.ListDocumentsByPrincipal(t.Context(), guestId, permissionsFilter, &cursor, 10)
	if err != nil {
		t.Fatalf("failed to list documents by principal with error: %v", err)
	}
	if len(documentPermissions) > 0 {
		t.Errorf("failed to remove permission on document after deleting the document, expected empty list, got: %v", documentPermissions)
	}
}

func TestListDocumentsByPrincipal_OnGuest_UpdatePermissionPath_Integration(t *testing.T) {
	// create a document repository with a connection to the postgres instance 
	documentRepo := createTestingDocumentRepo(t)
	// create a dummy user id
	userId := uuid.New()
	// create a document for that user
	documentId, err := documentRepo.CreateDocument(t.Context(), userId, nil, nil)
	if err != nil {
		t.Fatalf("failed to create a document with error: %v", err)
	}
	// create a guest on that document
	guestId, err := documentRepo.CreateGuest(t.Context(), userId, documentId, service.Editor)
	if err != nil {
		t.Fatalf("failed to create a guest with error: %v", guestId)
	}
	// create a list of permissions and an empty cursor to call the ListDocumentsByPrincipal function with
	permissionsFilter := []service.PermissionLevel{service.Editor, service.Viewer}
	// use the current time as the last seen time, the cursor will start at the last seen time
	// then traverse the created at index in descending order
	cursor := service.Cursor{ SortField: service.CreatedAt, LastSeenTime: time.Now(), LastSeenID: service.MaxDocumentID() }
	// view that document in the response from ListDocumentsByPrincipal for the recipient user
	documentPermissions, _, err := documentRepo.ListDocumentsByPrincipal(t.Context(), guestId, permissionsFilter, &cursor, 10)
	if err != nil {
		t.Fatalf("failed to list documents by principal with error: %v", err)
	}
	// verify that the guest has editor permissions on the created document
	found := false
	for _, documentPermission := range documentPermissions {
		if documentPermission.Document.ID == documentId {
			found = true
			if documentPermission.Permission != service.Editor {
				t.Errorf(
					"document was created with incorrect permission, want: %v, got: %v",
					service.Editor,
					documentPermission.Permission,
				)
			}
		}
	}
	if !found {
		t.Fatalf("failed to retrieve the created document %s, got this list of document permissions: %v",documentId, documentPermissions)
	}
	// delete the guests permission on that document
	err = documentRepo.DeletePermissionsPrincipal(t.Context(), guestId, documentId)
	if err != nil {
		t.Fatalf("failed to delete the guests permission on a document with error: %v", err)
	}
	// verify that the document cannot be viewed in the result of ListDocumentsByPrincipal
	documentPermissions, _, err = documentRepo.ListDocumentsByPrincipal(t.Context(), guestId, permissionsFilter, &cursor, 10)
	if err != nil {
		t.Fatalf("failed to list documents by principal with error: %v", err)
	}
	if len(documentPermissions) > 0 {
		t.Errorf("failed to remove permission on document after deleting the document, expected empty list, got: %v", documentPermissions)
	}
}

func verifyDocumentPermission(
	t *testing.T,
	documentPermissions []service.DocumentPermission,
	desiredDocumentId uuid.UUID,
	desiredPermissionLevel service.PermissionLevel,
) bool {
	// iterate through the list of document permissions
	// keep track of wether the correct document permission is found
	found := false
	for _, documentPermission := range documentPermissions {
		if documentPermission.Document.ID == desiredDocumentId {
			found = true
			// when the correct document permission is found, validate that the 
			// permission level is correct
			if documentPermission.Permission != desiredPermissionLevel {
				t.Errorf(
					"the permission level is incorrect, want: %v, got: %v",
					desiredPermissionLevel,
					documentPermission.Permission,
				)
			}
		}
	}
	// return wether or not the document permission was found
	return found
}

// ========== ListDocumentsByPrincipal: Filtering Logic ========== //
func TestListDocumentsByPrincipal_PermissionFiltering_Integration(t *testing.T) {
	// create a document repository with a connection to the postgres instance 
	documentRepo := createTestingDocumentRepo(t)
	// create a dummy user
	userId := uuid.New()
	// create a dummy recipient user
	recipientUserId := uuid.New()
	// create two documents
	documentIdA, err := documentRepo.CreateDocument(t.Context(), userId, nil, nil)
	if err != nil {
		t.Fatalf("failed to create document with error: %v", err)
	}
	documentIdB, err := documentRepo.CreateDocument(t.Context(), userId, nil, nil)
	if err != nil {
		t.Fatalf("failed to create document with error: %v", err)
	}
	// share the two documents with the recipient user at editor and viewer level
	err = documentRepo.UpsertPermissionUser(t.Context(), recipientUserId, documentIdA, service.Editor)
	if err != nil {
		t.Fatalf("failed to share document with user with error: %v", err)
	}
	err = documentRepo.UpsertPermissionUser(t.Context(), recipientUserId, documentIdB, service.Viewer)
	if err != nil {
		t.Fatalf("failed to share document with user with error: %v", err)
	}
	// verify that the user can see both documents when filtering on owner permission
	permissions := []service.PermissionLevel{service.Owner}
	cursor := &service.Cursor{
		SortField: service.CreatedAt,
		LastSeenTime: time.Now(),
		LastSeenID: service.MaxDocumentID(),
	}
	documentPermissions, _, err := documentRepo.ListDocumentsByPrincipal(
		t.Context(), userId, permissions, cursor, 10,

	)
	if err != nil {
		t.Fatalf("failed to list documents by principal for dummy user with error: %v", err)
	}
	if !verifyDocumentPermission(t, documentPermissions, documentIdA, service.Owner) {
		t.Errorf("failed to find document A in the list of retrieved documents for the dummy user")
	}
	if !verifyDocumentPermission(t, documentPermissions, documentIdB, service.Owner) {
		t.Errorf("failed to find document B in the list of retrieved documents for the dummy user")
	}
	// verify that the user can see no documents when filtering on editor permissions
	permissions = []service.PermissionLevel{service.Editor}
	documentPermissions, _, err = documentRepo.ListDocumentsByPrincipal(
		t.Context(), userId, permissions, cursor, 10,
	)
	if err != nil {
		t.Fatalf("failed to list documents by principal with error: %v", err)
	}
	if len(documentPermissions) > 0 {
		t.Errorf("received permissions even after filtering out all expected permissions, want an empty list, got: %v", documentPermissions)
	}
	// verify that the recipient user can see no documents when filtering on the owner permission
	permissions = []service.PermissionLevel{ service.Owner }
	documentPermissions, _, err = documentRepo.ListDocumentsByPrincipal(
		t.Context(), recipientUserId, permissions, cursor, 10,
	)
	if err != nil {
		t.Fatalf("failed to read documents by principal with error: %v", err)
	}
	if len(documentPermissions) > 0 {
		t.Errorf(
			"received permissions even after filtering out all expected permissions," + 
			"want an empty list, got: %v",
			documentPermissions,
		)
	}
	// verify that the recipient user can see the first document when filtering on the editor permission
	permissions = []service.PermissionLevel{ service.Editor }
	documentPermissions, _, err = documentRepo.ListDocumentsByPrincipal(
		t.Context(), recipientUserId, permissions, cursor, 10,
	)
	if err != nil {
		t.Fatalf("failed to list documents by principal with error: %v", err)
	}
	if !verifyDocumentPermission(t, documentPermissions, documentIdA, service.Editor) {
		t.Errorf("failed to find document A in the list of retrieved documents for the recipient user")
	}
	// verify that the recipient user can see the second document when filtering on the viewer permission
	permissions = []service.PermissionLevel{ service.Viewer }
	documentPermissions, _, err = documentRepo.ListDocumentsByPrincipal(
		t.Context(), recipientUserId, permissions, cursor, 10,
	)
	if err != nil {
		t.Fatalf("failed to list documents by principal with error: %v", err)
	}
	if !verifyDocumentPermission(t, documentPermissions, documentIdB, service.Viewer) {
		t.Errorf("failed to find document b in the list of retrieved documents for the recipient user")
	}
}

// ========== ListDocumentsByPrincipal: Input validation ========== //
func TestListDocumentsByPrincipal_NilCursor_Unit(t *testing.T) {
	// create a document repository struct with zero value for database connection
	documentRepo := &repository.DocumentRepository{}
	// verify that calling list documents by principal with a nil cursor returns an error
	_, _, err := documentRepo.ListDocumentsByPrincipal(
		t.Context(), uuid.New(), []service.PermissionLevel{service.Editor }, nil, 10,
	)
	if err == nil {
		t.Errorf("expected an error when calling with bad cursor but instead received nil")
	} else {
		if !errors.Is(err, service.ErrNilPointer) {
			t.Errorf("want ErrNilPointer: %v, got: %v", service.ErrNilPointer, err)
		}
	}
}

func TestListDocumentsByPrincipal_EmptyPermissionsFilter_Unit(t *testing.T) {
	// create a document repository struct with zero value for database connection
	documentRepo := &repository.DocumentRepository{}
	// verify that calling the list documents by principal id with an empty permission
	// filter list returns an error
	var permissions []service.PermissionLevel
	cursor := &service.Cursor{
		SortField: service.CreatedAt,
		LastSeenTime: time.Now(),
		LastSeenID: service.MaxDocumentID(),
	}
	_, _, err := documentRepo.ListDocumentsByPrincipal(
		t.Context(), uuid.New(), permissions, cursor, 10,
	)
	if err == nil {
		t.Error("expected an error when calling with an empty permissions array but instead received nil")
	} else {
		var serviceError *service.InvalidInputError
		if !errors.As(err, &serviceError) {
			t.Errorf("want: a service InvalidInputError, got: %v", err)
		}
	}
}

func TestListDocumentsByPrincipal_InvalidPermissionsFilter_Unit(t *testing.T) {
	// create a document repository struct with zero value for database connection
	documentRepo := &repository.DocumentRepository{}
	// verify that calling the list documents by principal id with an invalid permission
	// returns an error
	permissions := []service.PermissionLevel{ 42 }
	cursor := &service.Cursor{
		SortField: service.CreatedAt,
		LastSeenTime: time.Now(),
		LastSeenID: service.MaxDocumentID(),
	}
	_, _, err := documentRepo.ListDocumentsByPrincipal(
		t.Context(), uuid.New(), permissions, cursor, 10,
	)
	if err == nil {
		t.Error("expected an error when calling with an invalid permission but instead received nil")
	} else {
		var serviceError *service.InvalidInputError
		if !errors.As(err, &serviceError) {
			t.Errorf("want: a service InvalidInputError, got: %v", err)
		}
	}
}