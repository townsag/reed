package server

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	emptypb "google.golang.org/protobuf/types/known/emptypb"

	"github.com/google/uuid"
	pb "github.com/townsag/reed/user_service/api"
	"github.com/townsag/reed/user_service/internal/service"
)

/*
Responsibilities:
- the server layer is responsible for handling:
	- the routing of requests to the correct handler
	- marshaling and unmarshaling of messages
	- authentication
*/

type UserServiceServerImpl struct {
	pb.UnimplementedUserServiceServer
	userService *service.UserService
}

func NewUserServiceImpl(userService *service.UserService) *UserServiceServerImpl {
	return &UserServiceServerImpl{
		userService: userService,
	}
}

func serviceToGRPCError(err error) error {
	// something like this could be used to determine if the error is one of our wrapped domain errors or 
	// if it is a unknown error type
	// var domainError *service.DomainError
	// errors.As(error, &domainError)
	var notFound *service.NotFoundError
	var uniqueError *service.UniqueConflictError
	var invalidError *service.InvalidError
	var passwordError *service.PasswordMismatchError

	switch {
	case err == nil:
		return nil
	case errors.As(err, &notFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.As(err, &uniqueError):
		return status.Error(codes.AlreadyExists, err.Error())
	case errors.As(err, &invalidError):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.As(err, &passwordError):
		return status.Error(codes.PermissionDenied, err.Error())
	default:
		return status.Error(codes.Internal, "internal server error encountered")
	}
}

func (s *UserServiceServerImpl) GetUser(
	ctx context.Context,
	getUserReq *pb.GetUserRequest,
) (*pb.UserReply, error) {
	// parse the given userId
	userId, err := uuid.Parse(getUserReq.UserId)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "failed to parse user id as uuid: %v", getUserReq.UserId)
	}
	user, err := s.userService.GetUser(ctx, userId)
	if err != nil {
		return nil, serviceToGRPCError(err)
	}
	return &pb.UserReply{
		User: &pb.User{
			UserId: user.UserId.String(),
			UserName: user.UserName,
			Email: user.Email,
			MaxDocuments: user.MaxDocuments, 
		},
	}, nil
}

func (s *UserServiceServerImpl) CreateUser(
	ctx context.Context, 
	createUserReq *pb.CreateUserRequest,
) (*pb.CreateUserReply, error) {
	// validate the required fields are not empty: user_name, user_email, password
	if createUserReq.UserName == "" {
		return nil, status.Errorf(codes.InvalidArgument, "user_name is required")
	}
	if createUserReq.UserEmail == "" {
		return nil, status.Errorf(codes.InvalidArgument, "user_email is required")
	}
	if createUserReq.Password == "" {
		return nil, status.Errorf(codes.InvalidArgument, "password is required")
	}
	// create the user using the user service layer
	userId, err := s.userService.CreateUser(ctx, createUserReq.UserName, createUserReq.UserEmail, createUserReq.MaxDocuments, createUserReq.Password)
	// try the different types of service errors that can be created, return the appropriate code
	// conflict, internal service error, etc.
	if err != nil {
		return nil, serviceToGRPCError(err)
	}
	return &pb.CreateUserReply{
		UserId: userId.String(),
	}, nil
}

func (s *UserServiceServerImpl) DeactivateUser(
	ctx context.Context,
	deactivateUserReq *pb.DeactivateUserRequest,
) (*emptypb.Empty, error) {
	// parse the user id
	userId, err := uuid.Parse(deactivateUserReq.UserId)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "failed to parse user id as uuid: %v", deactivateUserReq.UserId)
	}
	err = s.userService.DeactivateUser(ctx, userId)
	if err != nil {
		return nil, serviceToGRPCError(err)
	}
	// TODO: add request id
	return &emptypb.Empty{}, nil
}

func (s *UserServiceServerImpl) ChangePassword(
	ctx context.Context,
	changePasswordRequest *pb.ChangeUserPasswordRequest,
) (*emptypb.Empty, error) {
	if changePasswordRequest.OldPassword == "" {
		return nil, status.Error(codes.InvalidArgument, "user old_password is a required argument")
	}
	if changePasswordRequest.NewPassword == "" {
		return nil, status.Error(codes.InvalidArgument, "user new_password is a required argument")
	}
	// parse the user id
	userId, err := uuid.Parse(changePasswordRequest.UserId)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "failed to parse user id as uuid: %v", changePasswordRequest.UserId)
	}
	err = s.userService.ChangePassword(ctx, userId, changePasswordRequest.OldPassword, changePasswordRequest.NewPassword)
	if err != nil {
		return nil, serviceToGRPCError(err)
	}
	// TODO: add request id
	return &emptypb.Empty{}, nil
}

/*
CHECKPOINT:
- you were modifying the user server to use uuids instead of integers
- all the tests are broken now lol
*/