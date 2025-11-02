package document_repository_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/townsag/reed/document_service/internal/service"
)

func TestUpsertPermissionUser_DocumentNotFound_Integration(t *testing.T) {
	// create a document repo struct with access to the testing postgres instance
	documentRepo := createTestingDocumentRepo(t)
	// call upsert permission user on a document that does not exist
	err := documentRepo.UpsertPermissionsUser(t.Context(), uuid.New(), uuid.New(), service.Editor)
	// validate that the returned error is a not found error
	if err == nil {
		t.Fatalf(
			"expected an error when calling update permission on a missing document but got nil",
		)
	} else {
		var target *service.NotFoundError
		if !errors.As(err, &target) {
			t.Errorf(
				"the returned error type is incorrect, want a not found error, got: %v",
				err,
			)
		}
	}
}

func TestUpdatePermissionGuest_GuestNotFound_Integration(t *testing.T) {
	// create a document repo struct with access to the testing postgres instance
	documentRepo := createTestingDocumentRepo(t)
	// create a user
	userId := uuid.New()
	// create a document with that user
	documentId, err := documentRepo.CreateDocument(t.Context(), userId, nil, nil)
	if err != nil {
		t.Fatalf("failed to create document with error: %v", err)
	}
	// update the permission of a guest that does not exist on that document
	err = documentRepo.UpdatePermissionGuest(t.Context(), uuid.New(), documentId, service.Viewer)
	// verify that the returned error is of the correct type
	if err == nil {
		t.Fatal(
			"expected an error when calling update permission guest on a guest that does " + 
			"not exist but got nil instead",
		)
	} else {
		var target *service.NotFoundError
		if !errors.As(err, &target) {
			t.Errorf(
				"the returned error type is incorrect, want not found error, got: %v",
				err,
			)
		}
	}
}

func TestUpdatePermissionGuest_DocumentNotFound_Integration(t *testing.T) {
	// create a document repo struct with access to the testing postgres instance
	documentRepo := createTestingDocumentRepo(t)
	// call update permission guest on a document that does not exist
	err := documentRepo.UpdatePermissionGuest(t.Context(), uuid.New(), uuid.New(), service.Viewer)
	// verify that the returned error is of the correct type
	if err == nil {
		t.Fatal(
			"expected an error when calling update permission guest on a document that does " + 
			"not exist but got nil instead",
		)
	} else {
		var target *service.NotFoundError
		if !errors.As(err, &target) {
			t.Errorf(
				"the returned error type is incorrect, want not found error, got: %v",
				err,
			)
		}
	}
}

func TestDeletePermissionPrincipal_NotFound_Integration(t *testing.T) {
	// create a document repo struct with access to the testing postgres instance
	documentRepo := createTestingDocumentRepo(t)
	// create a user
	userId := uuid.New()
	// create a document
	documentId, err := documentRepo.CreateDocument(t.Context(), userId, nil, nil)
	if err != nil {
		t.Fatalf("failed to create a document with error: %v", err)
	}
	// call delete permission principal on that document but a different recipient
	err = documentRepo.DeletePermissionsPrincipal(t.Context(), uuid.New(), documentId)
	// verify that the error type is correct
	if err == nil {
		t.Fatal(
			"expected an error when calling delete permission principal on a user that does " + 
			"not exist but got nil instead",
		)
	} else {
		var target *service.NotFoundError
		if !errors.As(err, &target) {
			t.Errorf(
				"the returned error type is incorrect, want not found error, got: %v",
				err,
			)
		}
	}
}