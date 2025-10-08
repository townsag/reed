package server

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/townsag/reed/user_service/api"
	"github.com/townsag/reed/user_service/internal/service"
)

/*
Responsibilities:
- the server layer is responsible for handling:
	- the routing of requests to the correct handler
	- marshaling and unmarshaling of messages 
	- authentication 

- TODO:
	- refactor so that I always return the request id even on error response 
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
	if getUserReq.UserId == 0 {
		return nil, status.Errorf(codes.InvalidArgument, "UserId is required, received: %d", getUserReq.UserId)
	}
	user, err := s.userService.GetUser(ctx, getUserReq.UserId)
	if err != nil {
		return nil, serviceToGRPCError(err)
	}
	return &pb.UserReply{
		User: &pb.User{
			UserId: user.UserId,
			UserName: user.UserName,
			Email: user.Email,
			MaxDocuments: user.MaxDocuments, 
		},
		// TODO: add request id
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
		// TODO: add request id
		UserId: userId,
	}, nil
}

func (s *UserServiceServerImpl) DeactivateUser(
	ctx context.Context,
	deactivateUserReq *pb.DeactivateUserRequest,
) (*pb.DeactivateUserReply, error) {
	if deactivateUserReq.UserId == 0 {
		return nil, status.Errorf(codes.InvalidArgument, "user_id is required, received: %d", deactivateUserReq.UserId)
	}
	err := s.userService.DeactivateUser(ctx, deactivateUserReq.UserId)
	if err != nil {
		return nil, serviceToGRPCError(err)
	}
	// TODO: add request id
	return &pb.DeactivateUserReply{}, nil
}

func (s *UserServiceServerImpl) ChangePassword(
	ctx context.Context,
	changePasswordRequest *pb.ChangeUserPasswordRequest,
) (*pb.ChangeUserPasswordReply, error) {
	if changePasswordRequest.OldPassword == "" {
		return nil, status.Error(codes.InvalidArgument, "user old_password is a required argument")
	}
	if changePasswordRequest.NewPassword == "" {
		return nil, status.Error(codes.InvalidArgument, "user new_password is a required argument")
	}
	err := s.userService.ChangePassword(ctx, changePasswordRequest.UserId, changePasswordRequest.OldPassword, changePasswordRequest.NewPassword)
	if err != nil {
		return nil, serviceToGRPCError(err)
	}
	// TODO: add request id
	return &pb.ChangeUserPasswordReply{}, nil
}