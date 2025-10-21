package document_repository_test

import (
	"testing"

	"github.com/google/uuid"
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