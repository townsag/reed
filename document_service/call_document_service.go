package main

import (
	"context"
	"log"
	"fmt"

	"github.com/google/uuid"
	"github.com/townsag/reed/document_service/pkg/client"
	api "github.com/townsag/reed/document_service/api/v1"
)

func main() {
	ctx := context.Background()
	// create an instance of the document service client
	client, err := client.NewDocumentServiceClient("localhost:50052")
	if err != nil {
		log.Fatalf("failed to create a connection to document service: %v", err)
	}
	ownerId := uuid.New()
	sharedId := uuid.New()
	// create a new document
	documentName := "test document"
	documentDescription := "a document for testing tracing"
	documentId, err := client.CreateDocument(
		ctx, ownerId, &documentName, &documentDescription,
	)
	if err != nil {
		log.Fatalf("failed to create document: %v", err)
	}
	// create some permissions on that document
	err = client.UpsertPermissionUser(
		ctx, sharedId, ownerId, documentId, api.PermissionLevel_PERMISSION_VIEWER,
	)
	if err != nil {
		log.Fatalf("failed to create document: %v", err)
	}
	// get the document
	document, err := client.GetDocument(
		ctx, documentId, ownerId,
	)
	if err != nil {
		log.Fatalf("failed to get the document: %v", err)
	}
	fmt.Printf("created document: %+v\n", document)
	// get the permissions on the document
	permission, err := client.GetPermissionsOfPrincipalOnDocument(
		ctx, documentId, sharedId,ownerId,
	)
	if err != nil {
		log.Fatalf("failed to get the permission of the principal: %v", err)
	}
	fmt.Printf("%+v\n", permission)
}
