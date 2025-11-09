package document_repository_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/townsag/reed/document_service/internal/service"
)

func TestGetPermissionOfPrincipalOnDocument_OnUser_UpdatePermissionPath_Integration(t *testing.T) {
	// create a document repo instance that has a connection to the postgres test container
	documentRepo := createTestingDocumentRepo(t)
	// create a user and a recipient to share the document with
	var (
		userId = uuid.New()
		recipientId = uuid.New()
	)
	// create a document owned by the user
	documentId, err := documentRepo.CreateDocument(t.Context(), userId, nil, nil)
	if err != nil {
		t.Fatalf("failed to create a document with error: %v", err)
	}
	// share the document with the recipient
	err = documentRepo.UpsertPermissionUser(t.Context(), recipientId, documentId, service.Editor)
	if err != nil {
		t.Fatalf("failed to create permission on document with error: %v", err)
	}
	// verify that the recipient has permission on the document
	permission, err := documentRepo.GetPermissionOfPrincipalOnDocument(t.Context(), documentId, recipientId)
	if err != nil {
		t.Errorf("failed to get permission of principal on document with error: %v", err)
	}
	if permission.PermissionLevel != service.Editor {
		t.Fatalf(
			"wrong permission was created on recipient, want: %v, got: %v", 
			service.Editor, 
			permission.PermissionLevel,
		)
	}
	// update the permission of the recipient on the document
	err = documentRepo.UpsertPermissionUser(t.Context(), recipientId, documentId, service.Viewer)
	if err != nil {
		t.Fatalf("failed to update permission of user on document with error: %v", err)
	}
	// verify that the permission is updated
	permission, err = documentRepo.GetPermissionOfPrincipalOnDocument(t.Context(), documentId, recipientId)
	if err != nil {
		t.Fatalf("failed to get permission of user on document with error: %v", err)
	}
	if permission.PermissionLevel != service.Viewer {
		t.Errorf(
			"the updated permission is not observed, want: %v, got: %v",
			service.Viewer, 
			permission.PermissionLevel,
		)
	}
}

func TestGetPermissionOfPrincipalOnDocument_OnUser_DeletePermissionPath_Integration(t *testing.T) {
	// create a document repo instance that has a connection to the postgres test container
	documentRepo := createTestingDocumentRepo(t)
	// create a user and a recipient to share the document with
	var (
		userId = uuid.New()
		recipientId = uuid.New()
	)
	// create a document owned by the user
	documentId, err := documentRepo.CreateDocument(t.Context(), userId, nil, nil)
	if err != nil {
		t.Fatalf("failed to create a document with error: %v", err)
	}
	// share the document with the recipient
	err = documentRepo.UpsertPermissionUser(t.Context(), recipientId, documentId, service.Editor)
	if err != nil {
		t.Fatalf("failed to create permission on document with error: %v", err)
	}
	// verify that the recipient has permission on the document
	permission, err := documentRepo.GetPermissionOfPrincipalOnDocument(t.Context(), documentId, recipientId)
	if err != nil {
		t.Errorf("failed to get permission of principal on document with error: %v", err)
	}
	if permission.PermissionLevel != service.Editor {
		t.Fatalf(
			"wrong permission was created on recipient, want: %v, got: %v", 
			service.Editor, 
			permission.PermissionLevel,
		)
	}
	// delete the permission of the recipient on the document
	err = documentRepo.DeletePermissionsPrincipal(t.Context(), recipientId, documentId)
	if err != nil {
		t.Fatalf("failed to delete permission of user on document with error: %v", err)
	}
	// verify that the permission is deleted
	_, err = documentRepo.GetPermissionOfPrincipalOnDocument(t.Context(), documentId, recipientId)
	if err == nil {
		t.Fatalf(
			"expected an error response when getting permission of a recipient on a document" +
			" after the recipient permission has been deleted but got nil instead of an error",
		)
	} else {
		// verify that the error is a not found error
		var target *service.NotFoundError
		if !errors.As(err, &target) {
			t.Errorf(
				"expected a not found error when calling get permission of principal on document" +
				" after the recipients permission has been deleted on that document, instead got: %v",
				err,
			)
		}
	}
}

func TestGetPermissionOfPrincipalOnDocument_OnGuest_UpdatePermissionPath_Integration(t *testing.T) {
	// create a document repo instance that has a connection to the postgres test container
	documentRepo := createTestingDocumentRepo(t)
	// create a user
	userId := uuid.New()
	// create a document owned by the user
	documentId, err := documentRepo.CreateDocument(t.Context(), userId, nil, nil)
	if err != nil {
		t.Fatalf("failed to create a document with error: %v", err)
	}
	// create a guest on that document
	guestId, err := documentRepo.CreateGuest(t.Context(), userId, documentId, service.Editor)
	if err != nil {
		t.Fatalf("failed to create guest on document with error: %v", err)
	}
	// verify that the guest has permission on the document
	permission, err := documentRepo.GetPermissionOfPrincipalOnDocument(t.Context(), documentId, guestId)
	if err != nil {
		t.Errorf("failed to get permission of guest on document with error: %v", err)
	}
	if permission.PermissionLevel != service.Editor {
		t.Fatalf(
			"wrong permission was created on guest, want: %v, got: %v", 
			service.Editor, 
			permission.PermissionLevel,
		)
	}
	// update the permission of the recipient on the document
	err = documentRepo.UpdatePermissionGuest(t.Context(), guestId, service.Viewer)
	if err != nil {
		t.Fatalf("failed to update permission of guest on document with error: %v", err)
	}
	// verify that the permission is updated
	permission, err = documentRepo.GetPermissionOfPrincipalOnDocument(t.Context(), documentId, guestId)
	if err != nil {
		t.Fatalf("failed to get permission of guest on document with error: %v", err)
	}
	if permission.PermissionLevel != service.Viewer {
		t.Errorf(
			"the updated permission is not observed, want: %v, got: %v",
			service.Viewer, 
			permission.PermissionLevel,
		)
	}
}

func TestGetPermissionOfPrincipalOnDocument_OnGuest_DeletePermissionPath_Integration(t *testing.T) {
	// create a document repo instance that has a connection to the postgres test container
	documentRepo := createTestingDocumentRepo(t)
	// create a user
	userId := uuid.New()
	// create a document owned by the user
	documentId, err := documentRepo.CreateDocument(t.Context(), userId, nil, nil)
	if err != nil {
		t.Fatalf("failed to create a document with error: %v", err)
	}
	// create a guest on that document
	guestId, err := documentRepo.CreateGuest(t.Context(), userId, documentId, service.Editor)
	if err != nil {
		t.Fatalf("failed to create guest on document with error: %v", err)
	}
	// verify that the guest has permission on the document
	permission, err := documentRepo.GetPermissionOfPrincipalOnDocument(t.Context(), documentId, guestId)
	if err != nil {
		t.Errorf("failed to get permission of guest on document with error: %v", err)
	}
	if permission.PermissionLevel != service.Editor {
		t.Fatalf(
			"wrong permission was created on guest, want: %v, got: %v", 
			service.Editor, 
			permission.PermissionLevel,
		)
	}
	// delete permissions of that guest on that document
	err = documentRepo.DeletePermissionsPrincipal(t.Context(), guestId, documentId)
	if err != nil {
		t.Fatalf("failed to delete permissions of guest on document with error: %v", err)
	}
	// verify that the permission is deleted
	_, err = documentRepo.GetPermissionOfPrincipalOnDocument(t.Context(), documentId, guestId)
	if err == nil {
		t.Fatalf(
			"expected an error response when getting permission of a guest on a document" +
			" after the guest permission has been deleted but got nil instead of an error",
		)
	} else {
		// verify that the error is a not found error
		var target *service.NotFoundError
		if !errors.As(err, &target) {
			t.Errorf(
				"expected a not found error when calling get permission of principal on document" +
				" after the guests permission has been deleted on that document, instead got: %v",
				err,
			)
		}
	}
}

func TestGetPermissionOfPrincipalOnDocument_InvalidDocument_Integration(t *testing.T) {
	// create a connection to the testing database
	documentRepo := createTestingDocumentRepo(t)
	// call the function on a document that does not exist
	_, err := documentRepo.GetPermissionOfPrincipalOnDocument(
		t.Context(), uuid.New(), uuid.New(),
	)
	// verify that we get a not found error
	if err == nil {
		t.Fatalf(
			"expected an error when calling get permission of principal on a document" +
			"that does not exist but go nil instead",
		)
	} else {
		var target *service.NotFoundError
		if !errors.As(err, &target) {
			t.Errorf(
				"expected a not found error, got: %v",
				err,
			)
		}
	}
}