package client

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	pb "github.com/townsag/reed/document_service/api/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type DocumentServiceClient struct {
	conn *grpc.ClientConn
	client pb.DocumentServiceClient
}

func NewDocumentServiceClient(addr string) (*DocumentServiceClient, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	// TODO: this is where we should add an observability interceptor
	if err != nil {
		return nil, fmt.Errorf("failed to create a connection: %s", err.Error())
	}
	// create a client struct using the generated protobuf
	// wrap the client struct with this client struct that also includes the connection
	client := pb.NewDocumentServiceClient(conn)
	return &DocumentServiceClient{
		conn: conn,
		client: client,
	}, nil
}

func (c *DocumentServiceClient) Close() error {
	return c.conn.Close()
}

/*
TODO:
- add some validations that the required pointers 
  that are being passed in are not nil
- add some validations that the arrays that are being passed in that we expect to have 
  values in them actually have values in them
*/

func (c *DocumentServiceClient) CreateDocument(
	ctx context.Context,
	ownerUserId uuid.UUID,
	documentName *string,
	documentDescription *string,
) (*pb.CreateDocumentReply, error) {
	return c.client.CreateDocument(
		ctx,
		&pb.CreateDocumentRequest{
			OwnerUserId: ownerUserId.String(),
			DocumentName: documentName,
			DocumentDescription: documentDescription,
			ClientContext: &pb.ClientContext{
				PrincipalId: ownerUserId.String(),
			},
		},
	)
}

func (c *DocumentServiceClient) GetDocument(
	ctx context.Context,
	documentId uuid.UUID,
	principalId uuid.UUID,
) (*pb.GetDocumentReply, error) {
	return c.client.GetDocument(
		ctx,
		&pb.GetDocumentRequest{
			DocumentId: documentId.String(),
			ClientContext: &pb.ClientContext{
				PrincipalId: principalId.String(),
			},
		},
	)
}

func (c *DocumentServiceClient) UpdateDocument(
	ctx context.Context,
	documentId uuid.UUID,
	principalId uuid.UUID,
	name *string,
	description *string,
) error {
	_, err := c.client.UpdateDocument(
		ctx,
		&pb.UpdateDocumentRequest{
			DocumentId: documentId.String(),
			Name: name,
			Description: description,
			ClientContext: &pb.ClientContext{
				PrincipalId: principalId.String(),
			},
		},
	)
	return err
}

func (c *DocumentServiceClient) DeleteDocument(
	ctx context.Context,
	documentId uuid.UUID,
	userId uuid.UUID,
) error {
	_, err := c.client.DeleteDocument(
		ctx,
		&pb.DeleteDocumentRequest{
			DocumentId: documentId.String(),
			ClientContext: &pb.ClientContext{
				PrincipalId: userId.String(),
				PrincipalType: pb.Principal_USER.Enum(),
			},
		},
	)
	return err
}

func (c *DocumentServiceClient) DeleteDocuments(
	ctx context.Context,
	documentIds uuid.UUIDs,
	userId uuid.UUID,
) error {
	_, err := c.client.DeleteDocuments(
		ctx,
		&pb.DeleteDocumentsRequest{
			DocumentIds: documentIds.Strings(),
			ClientContext: &pb.ClientContext{
				PrincipalId: userId.String(),
				PrincipalType: pb.Principal_USER.Enum(),
			},
		},
	)
	return err
}

func (c *DocumentServiceClient) ListDocumentsByPrincipal(
	ctx context.Context,
	targetPrincipalId uuid.UUID,
	callingPrincipalId uuid.UUID,
	permissionFilter []pb.PermissionLevel,
	cursor *pb.Cursor,
	pageSize *int32,
) (*pb.ListDocumentsByPrincipalReply, error) {
	return c.client.ListDocumentsByPrincipal(
		ctx,
		&pb.ListDocumentByPrincipalRequest{
			PrincipalId: targetPrincipalId.String(),
			PermissionsFilter: permissionFilter,
			Cursor: cursor,
			PageSize: pageSize,
			ClientContext: &pb.ClientContext{
				PrincipalId: callingPrincipalId.String(),
			},
		},
	)
}

func (c *DocumentServiceClient) GetPermissionsOfPrincipalOnDocument(
	ctx context.Context,
	documentId uuid.UUID,
	principalId uuid.UUID,
) (*pb.GetPermissionsReply, error) {
	return c.client.GetPermissionsOfPrincipalOnDocument(
		ctx,
		&pb.GetPermissionsRequest{
			DocumentId: documentId.String(),
			PrincipalId: principalId.String(),
		},
	)
}

/*
Sending an empty list of permissions is treated as no permission filter on the 
server side, therefore it is a valid input to this function
*/
func (c *DocumentServiceClient) ListPermissionsOnDocument(
	ctx context.Context,
	documentId uuid.UUID,
	principalId uuid.UUID,
	permissionFilter []pb.PermissionLevel,
	cursor *pb.Cursor,
	pageSize *int32,
) (*pb.ListPermissionsOnDocumentReply, error) {
	return c.client.ListPermissionsOnDocument(
		ctx,
		&pb.ListPermissionsOnDocumentRequest{
			DocumentId: documentId.String(),
			PermissionsFilter: permissionFilter,
			Cursor: cursor,
			PageSize: pageSize,
			ClientContext: &pb.ClientContext{
				PrincipalId: principalId.String(),
			},
		},
	)
}

func (c *DocumentServiceClient) CreateGuest(
	ctx context.Context,
	documentId uuid.UUID,
	userId uuid.UUID,
	permissionLevel pb.PermissionLevel,
) (*pb.CreateGuestReply, error) {
	return c.client.CreateGuest(
		ctx,
		&pb.CreateGuestRequest{
			DocumentId: documentId.String(),
			PermissionLevel: permissionLevel,
			ClientContext: &pb.ClientContext{
				PrincipalId: userId.String(),
				PrincipalType: pb.Principal_USER.Enum(),
			},
		},
	)
}

func (c *DocumentServiceClient) UpsertPermissionUser(
	ctx context.Context,
	targetUserId uuid.UUID,
	callingUserId uuid.UUID,
	documentId uuid.UUID,
	permissionLevel pb.PermissionLevel,
) error {
	_, err := c.client.UpsertPermissionUser(
		ctx,
		&pb.UpsertPermissionUserRequest{
			UserId: targetUserId.String(),
			DocumentId: documentId.String(),
			PermissionLevel: permissionLevel,
			ClientContext: &pb.ClientContext{
				PrincipalId: callingUserId.String(),
				PrincipalType: pb.Principal_USER.Enum(),
			},
		},
	)
	return err
}

func (c *DocumentServiceClient) UpdatePermissionGuest(
	ctx context.Context,
	guestId uuid.UUID,
	callingUserId uuid.UUID,
	permissionLevel pb.PermissionLevel,
) error {
	_, err := c.client.UpdatePermissionGuest(
		ctx,
		&pb.UpdatePermissionGuestRequest{
			GuestId: guestId.String(),
			PermissionLevel: permissionLevel,
			ClientContext: &pb.ClientContext{
				PrincipalId: callingUserId.String(),
				PrincipalType: pb.Principal_USER.Enum(),
			},
		},
	)
	return err
}

func (c *DocumentServiceClient) DeletePermissionsPrincipal(
	ctx context.Context,
	principalId uuid.UUID,
	documentId uuid.UUID,
	callingUserId uuid.UUID,
) error {
	_, err := c.client.DeletePermissionsPrincipal(
		ctx,
		&pb.DeletePermissionsPrincipalRequest{
			PrincipalId: principalId.String(),
			DocumentId: documentId.String(),
			ClientContext: &pb.ClientContext{
				PrincipalId: callingUserId.String(),
				PrincipalType: pb.Principal_USER.Enum(),
			},
		},
	)
	return err
}