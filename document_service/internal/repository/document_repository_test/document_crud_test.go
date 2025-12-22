package document_repository_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/townsag/reed/document_service/internal/repository"
	"github.com/townsag/reed/document_service/internal/service"
)

// ========== Document CRUD tests ========== //
// TODO: add comment anchors to the description of the test above each test
func TestCreateUpdateDocumentIntegration(t *testing.T) {
	// create a document repository with a connection to the postgres container instance
	documentRepo := createTestingDocumentRepo(t)
	// create a dummy user id
	userId := uuid.New()
	// create a document for that dummy user id
	dummyName := "dummy document"
	documentId, err := documentRepo.CreateDocument(t.Context(), userId, &dummyName, nil)
	if err != nil {
		t.Fatalf("failed to create a document with err: %v", err)
	}
	// get the created document
	document, err := documentRepo.GetDocument(t.Context(), documentId)
	if err != nil {
		t.Fatalf("failed to get created document with error: %v", err)
	}
	// update the name of that document
	updatedName := "updated document"
	err = documentRepo.UpdateDocument(t.Context(), documentId, &updatedName, nil)
	if err != nil {
		t.Fatalf("failed to update the document with error: %v", err)
	}
	// get the updated document
	documentUpdated, err := documentRepo.GetDocument(t.Context(), documentId)
	if err != nil {
		t.Fatalf("failed to get the updated document with error: %v", err)
	}
	// verify that the updated description is there, verify that the last modified date is changes
	if documentUpdated.Name == nil {
		t.Fatalf("updated document has no name, want: %s, got: %v", updatedName, documentUpdated.Name)
	}
	if *documentUpdated.Name != updatedName {
		t.Errorf("failed to update document name, want: %s, got: %s", updatedName, *document.Name)
	}
	if documentUpdated.LastModifiedAt.After(document.LastModifiedAt) {
		t.Errorf(
			"failed to update document last modified at: want a timestamp different from the previous timestamp: %v, got: %v",
			document.LastModifiedAt,
			documentUpdated.LastModifiedAt,
		)
	}
}

func TestCreateDeleteDocumentIntegration(t *testing.T) {
	// create a document repository with a connection to the postgres testcontainers instance
	documentRepo := createTestingDocumentRepo(t)
	// create a dummy user id
	dummyUserId := uuid.New()
	// create a document for that user id
	name := "dummy name"
	description := "dummy description"
	documentId, err := documentRepo.CreateDocument(t.Context(), dummyUserId, &name, &description)
	if err != nil {
		t.Fatalf("failed to create document with error: %v", err)
	}
	// get that document
	document, err := documentRepo.GetDocument(t.Context(), documentId)
	if err != nil {
		t.Fatalf("failed to get the document with err: %v", err)
	}
	// verify that the retrieved document has the correct attributes
	if *document.Name != name {
		t.Errorf("document name was not correct: want: %s, got: %s", name, *document.Name)
	}
	if *document.Description != description {
		t.Errorf("document description is incorrect: want: %s, got: %s", description, *document.Description)
	}
	// delete that document
	err = documentRepo.DeleteDocument(t.Context(), documentId)
	if err != nil {
		t.Fatalf("failed to delete document with err: %v", err)
	}
	// verify that the deleted document is unreachable by trying to get the document
	_, err = documentRepo.GetDocument(t.Context(), documentId)
	if err == nil {
		t.Errorf("failed to delete document, want a not found error, got: %v", err)
	}
}

func TestGetDocument_NotFound_Integration(t *testing.T) {
	// create a document repository object that has a connection to the
	// testing postgres instance
	documentRepository := createTestingDocumentRepo(t)
	// call get document on a document that does not exist
	_, err := documentRepository.GetDocument(
		t.Context(), uuid.New(),
	)
	if err == nil {
		t.Fatalf(
			"expected an error when calling get document on a document that " +
			"does not exist but got nil instead",
		)
	} else {
		var target *service.NotFoundError
		if !errors.As(err, &target) {
			t.Errorf(
				"got the wrong kind of error when getting a document that does " +
				"not exist, want a not found error, got: %v", err,
			)
		}
	}
}

func TestUpdateDocument_NotFound_Integration(t *testing.T) {
	// create a document repository object that has a connection to the
	// testing postgres instance
	documentRepository := createTestingDocumentRepo(t)
	// call update document on a document that does not exist
	name := "howdy partner"
	err := documentRepository.UpdateDocument(
		t.Context(), uuid.New(), &name, nil,
	)
	if err == nil {
		t.Fatalf(
			"expected an error when calling update document on a document that " +
			"does not exist but got nil instead",
		)
	} else {
		var target *service.NotFoundError
		if !errors.As(err, &target) {
			t.Errorf(
				"got the wrong kind of error when getting a document that does " +
				"not exist, want a not found error, got: %v", err,
			)
		}
	}
}

func TestDeleteDocument_NotFound_Integration(t *testing.T) {
	// create a document repository object that has a connection to the
	// testing postgres instance
	documentRepository := createTestingDocumentRepo(t)
	// call delete document on a document that does not exist
	err := documentRepository.DeleteDocument(
		t.Context(), uuid.New(),
	)
	if err == nil {
		t.Fatalf(
			"expected an error when calling delete document on a document that " +
			"does not exist but got nil instead",
		)
	} else {
		var target *service.NotFoundError
		if !errors.As(err, &target) {
			t.Errorf(
				"got the wrong kind of error when deleting a document that does " +
				"not exist, want a not found error, got: %v", err,
			)
		}
	}
}

func TestUpdateDocument_NilInputs_Unit(t *testing.T) {
	// create a document repository that does not have a connection to a 
	// testing database
	documentRepo := &repository.DocumentRepository{}
	// call update document with nil inputs
	err := documentRepo.UpdateDocument(
		t.Context(), uuid.New(), nil, nil,
	)
	if err == nil {
		t.Fatalf("expected an error when calling update document with nil inputs but got nil instead")
	} else {
		var target *service.InvalidInputError
		if !errors.As(err, &target) {
			t.Errorf(
				"got the wrong kind of error when calling update doc with nil inputs " +
				"want an invalid input error, got: %v", err, 
			)
		}
	}
}

func TestDeleteDocumentsIntegrations(t *testing.T) {
	// create a document repository object that is connected to the database
	documentRepo := createTestingDocumentRepo(t)
	// add two documents to the database
	dummyUser := uuid.New()
	documentAID, err := documentRepo.CreateDocument(
		t.Context(), dummyUser, nil, nil,
	)
	if err != nil {
		t.Fatalf("failed to create a document with error: %v", err)
	}
	documentBID, err := documentRepo.CreateDocument(
		t.Context(), dummyUser, nil, nil,
	)
	if err != nil {
		t.Fatalf("failed to create a document with error: %v", err)
	}
	// verify that each document was successfully created
	for _, documentId := range []uuid.UUID{documentAID, documentBID} {
		_, err = documentRepo.GetDocument(t.Context(), documentId)
		if err != nil {
			t.Fatalf("failed to retrieve the recently created document: %v", err)
		}
	}
	// perform a delete documents operation with both document ids 
	err = documentRepo.DeleteDocuments(t.Context(), []uuid.UUID{ documentAID, documentBID}, dummyUser)
	// verify that the result is not a error
	if err != nil {
		t.Fatalf("failed to bulk delete document: %v", err)
	}
	// verify that each document has been deleted by calling get document on each document 
	for _, documentId := range []uuid.UUID{documentAID, documentBID} {
		_, err = documentRepo.GetDocument(t.Context(), documentId)
		if err == nil {
			t.Error(
				"want a not found error when trying to retrieve a deleted document " + 
				"instead found nil",
			)
		}
	}
}

func TestDeleteDocuments_NotFound_Integration(t *testing.T) {
	// create a document repository object that is connected to a database
	documentRepository := createTestingDocumentRepo(t)
	// add a document to the database
	dummyUserId := uuid.New()
	documentId, err := documentRepository.CreateDocument(
		t.Context(), dummyUserId, nil, nil,
	)
	if err != nil {
		t.Fatalf("failed to create a document: %v", err)
	}
	// verify that the added document is in the database
	_, err = documentRepository.GetDocument(t.Context(), documentId)
	if err != nil {
		t.Fatalf("failed to read a document: %v", err)
	}
	// send a delete documents request with the added document and with a document that does not exist
	err = documentRepository.DeleteDocuments(
		t.Context(), []uuid.UUID{documentId, uuid.New()},dummyUserId,
	)
	// verify that the operation returns an error
	if err == nil {
		t.Fatal("when deleting a document that did not exist in a batch delete expected an error but got nil")
	}
	// verify that the created document is still there after the delete documents attempt failed
	_, err = documentRepository.GetDocument(t.Context(), documentId)
	if err != nil {
		t.Fatalf("failed to retrieve the document after the delete documents call failed with err: %v", err)
	}
}

func TestDeleteDocuments_EmptyArray_Unit(t *testing.T) {
	// create a document repository object that does not have access to the database
	documentRepo := &repository.DocumentRepository{}
	// call the delete documents function with an empty list
	dummyUser := uuid.New()
	err := documentRepo.DeleteDocuments(t.Context(), []uuid.UUID{}, dummyUser)
	// verify that the result of the operation is an error of the correct type
	if err != nil {
		var invalidInputError *service.InvalidInputError
		if !errors.As(err, &invalidInputError) {
			t.Fatalf("when calling delete documents with an empty list, want: invalid input error, got: %v", err)
		}
	} else {
		t.Fatalf("when calling delete documents with an empty list, want: invalid input error, got: nil")
	}
}