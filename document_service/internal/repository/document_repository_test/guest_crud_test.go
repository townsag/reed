package document_repository_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/townsag/reed/document_service/internal/service"
)

func TestCreateGuest_InvalidDocument_Integration(t *testing.T) {
	// create a document repo object that has a connection to the postgres
	// database
	documentRepo := createTestingDocumentRepo(t)
	// call create guest on a document that does not exist in the database
	_, err := documentRepo.CreateGuest(
		t.Context(), uuid.New(), uuid.New(), service.Editor,
	)
	// validate that the error is correct
	if err == nil {
		t.Fatalf("expected an error when creating a guest on an invalid document but got nil")
	} else {
		var target *service.NotFoundError
		if !errors.As(err, &target) {
			t.Errorf("the wrong type of error was returned, want not found error, got: %v", err)
		}
	}
}