package client

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/google/uuid"
	pb "github.com/townsag/reed/user_service/api"
)

type UserServiceClient struct {
	conn *grpc.ClientConn
	client pb.UserServiceClient
}

func NewUserServiceClient(addr string) (*UserServiceClient, error) {
	// perform some validations on the address to ensure that it is of the correct shape
	// create a connection to the grpc server
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	// TODO: this^ is where I would add an interceptor that did observability
	if err != nil {
		return nil, fmt.Errorf("failed to create a connection: %w", err)
	}
	// create a grpc client struct generated using the api proto
	client := pb.NewUserServiceClient(conn)
	return &UserServiceClient{
		conn: conn,
		client: client,
	}, nil
}

func (c *UserServiceClient) Close() error {
	return c.conn.Close()
}

func (c *UserServiceClient) GetUser(ctx context.Context, userId uuid.UUID) (*pb.UserReply, error) {
	return c.client.GetUser(ctx, &pb.GetUserRequest{ UserId: userId.String() })
}

func (c *UserServiceClient) CreateUser(
	ctx context.Context,
	userName string,
	userEmail string,
	password string,
	maxDocuments *int32,
) (*pb.CreateUserReply, error) {
	return c.client.CreateUser(
		ctx,
		&pb.CreateUserRequest{
			UserName: userName,
			UserEmail: userEmail,
			Password: password,
			MaxDocuments: maxDocuments,
		},
	)
}

func (c *UserServiceClient) DeactivateUser(ctx context.Context, userId uuid.UUID) error {
	_, err := c.client.DeactivateUser(ctx, &pb.DeactivateUserRequest{ UserId: userId.String() })
	return err
}

func (c *UserServiceClient) ChangeUserPassword(
	ctx context.Context,
	userId uuid.UUID,
	oldPassword string,
	newPassword string,
) error {
	_, err := c.client.ChangeUserPassword(
		ctx,
		&pb.ChangeUserPasswordRequest{
			UserId: userId.String(),
			OldPassword: oldPassword,
			NewPassword: newPassword,
		},
	)
	return err
}

func (c *UserServiceClient) ValidatePassword(
	ctx context.Context,
	userId uuid.UUID,
	password string,
) (bool, error) {
	reply, err := c.client.ValidatePassword(
		ctx,
		&pb.ValidatePasswordRequest{
			UserId: userId.String(),
			UserPassword: password,
		},
	)
	if err != nil {
		return false, err
	} else {
		return reply.IsValid, nil
	}
}