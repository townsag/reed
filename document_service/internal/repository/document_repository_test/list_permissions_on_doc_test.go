package document_repository_test

import (
	"testing"
	"errors"

	"github.com/google/uuid"

	"github.com/townsag/reed/document_service/internal/service"
	"github.com/townsag/reed/document_service/internal/repository"
)

func TestListPermissionsOnDocument_OnUser_DeleteDocumentPath_Integration(t *testing.T) {
	documentRepo := createTestingDocumentRepo(t)
	// create a dummy user and recipient
	userId := uuid.New()
	recipientId := uuid.New()
	// create a document
	documentId, err := documentRepo.CreateDocument(t.Context(), userId, nil, nil)
	if err != nil {
		t.Fatalf("failed to create a document with error: %v", err)
	}
	// share that document with the recipient
	err = documentRepo.UpsertPermissionUser(t.Context(), recipientId, documentId, service.Editor)
	if err != nil {
		t.Fatalf("failed to share the document with the recipient with error: %v", err)
	}
	// delete the document
	err = documentRepo.DeleteDocument(t.Context(), documentId)
	if err != nil {
		t.Fatalf("failed to delete document with error: %v", err)
	}
	// list the permissions on that document, verify that the user and recipient permissions are missing
	cursor := service.NewBeginningCursor(service.CreatedAt)
	_, _, err = documentRepo.ListPermissionsOnDocument(
		t.Context(), documentId, []service.PermissionLevel{service.Editor, service.Owner}, cursor, 10,
	)
	if err == nil {
		t.Fatalf("successfully listed the permissions on a document that had been deleted. expected a not found error")
	}
}

func TestListPermissionsOnDocument_OnUser_DeletePermissionPath_Integration(t *testing.T) {
	documentRepo := createTestingDocumentRepo(t)
	// create a dummy user
	userId := uuid.New()
	// create a document
	documentId, err := documentRepo.CreateDocument(t.Context(), userId, nil, nil)
	if err != nil {
		t.Fatalf("failed to create a document with error: %v", err)
	}
	// create a dummy recipient user to share that document with
	recipientId := uuid.New()
	// share the document with the recipient 
	err = documentRepo.UpsertPermissionUser(t.Context(), recipientId, documentId, service.Editor)
	if err != nil {
		t.Fatalf("failed to add permission on user with error: %v", err)
	}
	// verify that the user and the recipient both have permissions on the document
	cursor := service.NewBeginningCursor(service.CreatedAt)
	permissionsFilter := []service.PermissionLevel{service.Editor, service.Owner}
	permissions, respCursor, err := documentRepo.ListPermissionsOnDocument(
		t.Context(), documentId, permissionsFilter, cursor, 10,
	)
	if err != nil {
		t.Fatalf("failed to get permissions on document with error: %v", err)
	}
	foundUser := false
	foundRecipient := false
	for _, permission := range permissions {
		if permission.RecipientID == userId {
			foundUser = true
			if permission.PermissionLevel != service.Owner {
				t.Errorf("user has incorrect permission on document, want: %v, got: %v", service.Owner, permission.PermissionLevel)
			}
		}
		if permission.RecipientID == recipientId {
			foundRecipient = true
			if permission.PermissionLevel != service.Editor {
				t.Errorf("recipient has incorrect permission on document, want: %v, got: %v", service.Editor, permission.PermissionLevel)
			}
		}
	}
	if !foundUser {
		t.Fatalf("failed to find the permission of the user on the document when listing permissions")
	}
	if !foundRecipient {
		t.Fatalf("failed to find the permission of the recipient on the document when listing permissions")
	}
	// verify that the response cursor is properly formatted
	if respCursor.SortField != service.CreatedAt {
		t.Errorf("response cursor has wrong sort field, want: %v, got: %v", service.CreatedAt, respCursor.SortField)
	}
	if respCursor.LastSeenID != userId {
		t.Errorf(
			"the returned cursor does not have the correct value as the least recently created permission on the document, " +
			"expected the least recent recipient to be the creator of the document: %v, instead found: %v",
			userId.String(), recipientId.String(),
		)
	}
	// delete the recipients permissions on that document
	err = documentRepo.DeletePermissionsPrincipal(t.Context(), recipientId, documentId)
	if err != nil {
		t.Fatalf("failed to delete the recipients permission on the document with error: %v", err)
	}
	// verify that now only the user has permissions on the document
	permissions, _, err = documentRepo.ListPermissionsOnDocument(
		t.Context(), documentId, permissionsFilter, cursor, 10,
	)
	if err!= nil { t.Fatalf("failed to list permissions on document with error: %v", err )}
	for _, permission := range permissions {
		if permission.RecipientID == recipientId {
			t.Errorf(
				"deletion of permission of user on document not reflected in list permissions on document " +
				"was still able to retrieve the permission of the recipient on the document",
			)
		}
	}
}

func TestListPermissionsOnDocument_OnUser_UpdatePermissionPath_Integration(t *testing.T) {
	// create a document repository instance with access to the postgres container
	documentRepo := createTestingDocumentRepo(t)
	// create a user and two recipient users
	var (
		userId = uuid.New()
		recipientIdA = uuid.New()
		recipientIdB = uuid.New()
	)
	// create a document
	documentId, err := documentRepo.CreateDocument(t.Context(), userId, nil, nil)
	if err != nil {
		t.Fatalf("failed to create a document with error: %v", err)
	}
	// share the document with the two recipient users
	for _, recipientId := range []uuid.UUID{ recipientIdA, recipientIdB } {
		err = documentRepo.UpsertPermissionUser(t.Context(), recipientId, documentId, service.Editor)
		if err != nil {
			t.Fatalf("failed to share document with recipient with error: %v", err)
		}
	}
	// list the permissions on the document to verify that the two users are there
	cursor := service.NewBeginningCursor(service.LastModifiedAt)
	permissions, _, err := documentRepo.ListPermissionsOnDocument(
		t.Context(), documentId, []service.PermissionLevel{ service.Editor, service.Owner }, cursor, 10,
	)
	if err != nil {
		t.Fatalf("failed to list permissions on document with error: %v", err)
	}
	if len(permissions) < 3 {
		t.Fatalf("not enough permissions were returned, want 3, got: %d", len(permissions))
	}
	// the first permission should be the permission on recipientB because it was the most recently modified
	if permissions[0].RecipientID != recipientIdB {
		t.Fatalf(
			"permissions were returned out of order, the first permission should be the permission associated" + 
			"with the most recently added recipient: %v, got: %v", recipientIdB, permissions[0].RecipientID,
		)
	}
	if permissions[0].PermissionLevel != service.Editor {
		t.Fatalf(
			"the permission associated with recipientB has the wrong level, want: %v, got: %v", 
			service.Editor, permissions[0].PermissionLevel,
		)
	}
	// modify the permission of recipientA, this should change the order in which the permissions are returned
	err = documentRepo.UpsertPermissionUser(t.Context(), recipientIdA, documentId, service.Viewer)
	if err != nil {
		t.Fatalf("failed to update permissions on user with error: %v", err)
	}
	// list the permissions on the document again by last modified at to verify that the first
	cursor = service.NewBeginningCursor(service.LastModifiedAt)
	permissions, _, err = documentRepo.ListPermissionsOnDocument(
		t.Context(), documentId, []service.PermissionLevel{ service.Editor, service.Viewer, service.Owner }, cursor, 10,
	)
	if err != nil {
		t.Fatalf("failed to list permissions on document with error: %v", err)
	}
	// user is in the correct order by last modified at and the permission level associated with
	// the first user has changed
	if len(permissions) < 3 {
		t.Fatalf("not enough permissions were returned, expected 3, got: %v", len(permissions))
	}
	if permissions[0].RecipientID != recipientIdA {
		t.Errorf(
			"the first permission is the list is associated with the wrong recipient, want recipientA: %v, got %v",
			recipientIdA.String(),
			permissions[0].RecipientID.String(),
		)
	}
	if permissions[0].PermissionLevel != service.Viewer {
		t.Errorf("failed to update the recipients permission level, want: %v, got: %v", service.Viewer, permissions[0].PermissionLevel)
	}
}

func TestListPermissionsOnDocument_OnGuest_UpdatePermissionPath_Integration(t *testing.T) {
	// create a document repository instance with access to the postgres container
	documentRepo := createTestingDocumentRepo(t)
	// create a user and two recipient users
	userId := uuid.New()
	// create a document
	documentId, err := documentRepo.CreateDocument(t.Context(), userId, nil, nil)
	if err != nil {
		t.Fatalf("failed to create a document with error: %v", err)
	}
	// share the document with the guest
	guestId, err := documentRepo.CreateGuest(t.Context(), userId, documentId, service.Editor)
	if err != nil {
		t.Fatalf("failed to create a guest with error: %v", err)
	}
	// list the permissions on the document to verify that the two users are there
	cursor := service.NewBeginningCursor(service.LastModifiedAt)
	permissions, _, err := documentRepo.ListPermissionsOnDocument(
		t.Context(), documentId, []service.PermissionLevel{ service.Editor, service.Owner }, cursor, 10,
	)
	if err != nil {
		t.Fatalf("failed to list permissions on document with error: %v", err)
	}
	if len(permissions) < 2 {
		t.Fatalf("not enough permissions were returned, want 2, got: %d", len(permissions))
	}
	// the first permission should be the permission on the guest because it was the most recently modified
	if permissions[0].RecipientID != guestId {
		t.Fatalf(
			"permissions were returned out of order, the first permission should be the permission associated" + 
			"with the guest: %v, got: %v", guestId.String(), permissions[0].RecipientID.String(),
		)
	}
	if permissions[0].PermissionLevel != service.Editor {
		t.Fatalf(
			"the permission associated with the guest has the wrong level, want: %v, got: %v", 
			service.Editor, permissions[0].PermissionLevel,
		)
	}
	// modify the permission of the guest
	err = documentRepo.UpdatePermissionGuest(t.Context(), guestId, service.Viewer)
	if err != nil {
		t.Fatalf("failed to update permissions on user with error: %v", err)
	}
	// list the permissions on the document again by last modified
	// we have to create a new cursor because we are traversing the last modified index on the permissions table
	// because we have recently updated the permission, the permission modification could be in the future relative
	// to the cursor that was created before it was modified. Using the old cursor would prevent us from seeing the
	// modified permission
	cursor = service.NewBeginningCursor(service.LastModifiedAt)
	permissions, _, err = documentRepo.ListPermissionsOnDocument(
		t.Context(), documentId, []service.PermissionLevel{ service.Viewer, service.Owner }, cursor, 10,
	)
	if err != nil {
		t.Fatalf("failed to list permissions on document with error: %v", err)
	}
	if len(permissions) < 2 {
		t.Fatalf("not enough permissions were returned, expected 2, got: %v", len(permissions))
	}
	if permissions[0].RecipientID != guestId {
		t.Errorf(
			"the first permission is the list is associated with the wrong recipient, want guestId: %v, got %v",
			guestId.String(),
			permissions[0].RecipientID.String(),
		)
	}
	if permissions[0].PermissionLevel != service.Viewer {
		t.Errorf("failed to update the guests permission level, want: %v, got: %v", service.Viewer, permissions[0].PermissionLevel)
	}
}

func TestListPermissionsOnDocument_OnGuest_DeletePermissionPath_Integration(t *testing.T) {
	// create a document repository instance with access to the postgres container
	documentRepo := createTestingDocumentRepo(t)
	// create a user and two recipient users
	userId := uuid.New()
	// create a document
	documentId, err := documentRepo.CreateDocument(t.Context(), userId, nil, nil)
	if err != nil {
		t.Fatalf("failed to create a document with error: %v", err)
	}
	// share the document with the guest
	guestId, err := documentRepo.CreateGuest(t.Context(), userId, documentId, service.Editor)
	if err != nil {
		t.Fatalf("failed to create a guest with error: %v", err)
	}
	// list the permissions on the document to verify that the two users are there
	cursor := service.NewBeginningCursor(service.LastModifiedAt)
	permissions, _, err := documentRepo.ListPermissionsOnDocument(
		t.Context(), documentId, []service.PermissionLevel{ service.Editor, service.Owner }, cursor, 10,
	)
	if err != nil {
		t.Fatalf("failed to list permissions on document with error: %v", err)
	}
	if len(permissions) < 2 {
		t.Fatalf("not enough permissions were returned, want 2, got: %d", len(permissions))
	}
	// the first permission should be the permission on the guest because it was the most recently modified
	if permissions[0].RecipientID != guestId {
		t.Fatalf(
			"permissions were returned out of order, the first permission should be the permission associated" + 
			"with the guest: %v, got: %v", guestId.String(), permissions[0].RecipientID.String(),
		)
	}
	if permissions[0].PermissionLevel != service.Editor {
		t.Fatalf(
			"the permission associated with the guest has the wrong level, want: %v, got: %v", 
			service.Editor, permissions[0].PermissionLevel,
		)
	}
	// delete the permissions on the guest
	err = documentRepo.DeletePermissionsPrincipal(t.Context(), guestId, documentId)
	if err != nil {
		t.Fatalf("failed to delete permissions on user with error: %v", err)
	}
	// list the permissions on the document again by last modified
	cursor = service.NewBeginningCursor(service.LastModifiedAt)
	permissions, _, err = documentRepo.ListPermissionsOnDocument(
		t.Context(), documentId, []service.PermissionLevel{ service.Viewer, service.Owner }, cursor, 10,
	)
	if err != nil {
		t.Fatalf("failed to list permissions on document with error: %v", err)
	}
	if len(permissions) > 1 {
		t.Fatalf("more permissions than expected were returned, expected 1, got: %v", len(permissions))
	}
}

func TestListPermissionsOnDocument_PermissionFiltering_Integration(t *testing.T) {
	// create a document repo struct with a connection to the testing postgres instance
	documentRepo := createTestingDocumentRepo(t)
	// create a user and three recipients 
	var (
		userId = uuid.New()
		recipientIdA = uuid.New()
		recipientIdB = uuid.New()
		recipientIdC = uuid.New()
	)
	// create a document
	documentId, err := documentRepo.CreateDocument(
		t.Context(), userId, nil, nil,
	)
	if err != nil {
		t.Fatalf("failed to create document with error: %v", err)
	}
	// share the document with the recipients at editor and viewer levels
	for i, recipientId := range []uuid.UUID{ recipientIdA, recipientIdB, recipientIdC } {
		var pl service.PermissionLevel = service.Editor
		if i == 2 { pl = service.Viewer }
		err = documentRepo.UpsertPermissionUser(
			t.Context(), recipientId, documentId, pl,
		)
		if err != nil {
			t.Fatalf("failed to share the document with user with error: %v", err)
		}
	}
	// list the permissions on that document using editor permission level filter
	// verify that the expected number of permissions are returned
	cursor := service.NewBeginningCursor(service.CreatedAt)
	permissions, _, err := documentRepo.ListPermissionsOnDocument(
		t.Context(), documentId, []service.PermissionLevel{service.Editor}, cursor, 10,
	)
	if err != nil {
		t.Fatalf("failed to list permissions on document with error: %v", err)
	}
	if len(permissions) != 2 {
		t.Errorf("the wrong number of permissions were returned, want: 2, got: %v", len(permissions))
	}
	for _, permission := range permissions {
		if permission.PermissionLevel != service.Editor {
			t.Errorf(
				"returned permission has the wrong level, want: %v, got: %v",
				service.Editor, permission.PermissionLevel,
			)
		}
	}
	// list the permissions on the document using viewer permission level filter
	// verify that the expected number of permissions are returned
	permissions, _, err = documentRepo.ListPermissionsOnDocument(
		t.Context(), documentId, []service.PermissionLevel{service.Viewer}, cursor, 10,
	)
	if err != nil {
		t.Fatalf("failed to list permissions on document with error: %v", err)
	}
	if len(permissions) != 1 {
		t.Errorf("the wrong number of permissions were returned, want: 1, got: %v", len(permissions))
	}
	for _, permission := range permissions {
		if permission.PermissionLevel != service.Viewer {
			t.Errorf(
				"returned permission has the wrong level, want: %v, got: %v",
				service.Viewer, permission.PermissionLevel,
			)
		}
	}
}

func TestListPermissionsOnDocument_MissingDocument_Integration(t *testing.T) {
	documentRepo := createTestingDocumentRepo(t)
	cursor := service.NewBeginningCursor(service.CreatedAt)
	permissionFilter := []service.PermissionLevel{ service.Editor }
	_, _, err := documentRepo.ListPermissionsOnDocument(
		t.Context(), uuid.New(), permissionFilter, cursor, 10,
	)
	if err == nil {
		t.Error("expected an error when calling list permissions on document on a missing document")
	} else {
		var target *service.NotFoundError
		if !errors.As(err, &target) {
			t.Errorf(
				"expected a not found error when calling list permissions on a missing document, got: %v",
				err,
			)
		}
	}
}

func TestListPermissionsOnDocument_NilCursor_Unit(t *testing.T) {
	documentRepo := &repository.DocumentRepository{}
	_, _, err := documentRepo.ListPermissionsOnDocument(
		t.Context(), uuid.New(), []service.PermissionLevel{ service.Editor }, nil, 10,
	)
	if err == nil {
		t.Errorf("expected an error when calling list permissions on document with a nil pointer but instead got nil")
	} else {
		if !errors.Is(err, service.ErrNilPointer) {
			t.Errorf("wrong error type received, want: %v, got: %v", service.ErrNilPointer, err)
		}
	}
}

func TestListPermissionsOnDocument_EmptyPermissionsList_Unit(t *testing.T) {
	documentRepo := &repository.DocumentRepository{}
	cursor := service.NewBeginningCursor(service.CreatedAt)
	_, _, err := documentRepo.ListPermissionsOnDocument(
		t.Context(), uuid.New(), []service.PermissionLevel{}, cursor, 10,
	)
	if err == nil {
		t.Error("expected an error when calling list permissions on document with empty permission level filter list but got nil")
	} else {
		var target *service.InvalidInputError
		if !errors.As(err, &target) {
			t.Errorf(
				"wrong error type returned, want %v, got %v",
				service.InvalidInput("", nil), err,
			)
		}
	}
}

func TestListPermissionsOnDocument_InvalidPermission_Unit(t *testing.T) {
	documentRepo := &repository.DocumentRepository{}
	_, _, err := documentRepo.ListPermissionsOnDocument(
		t.Context(), uuid.New(), []service.PermissionLevel{ -1 }, 
		service.NewBeginningCursor(service.CreatedAt), 10,
	)
	if err == nil {
		t.Error("expected an error when calling list permissions on document with an invalid permission, got nil")
	} else {
		var target *service.InvalidInputError
		if !errors.As(err, &target) {
			t.Errorf("expected a invalid input error when calling list permissions on document with an invalid permission, got: %v", err)
		}
	}
}