package service

import (
	"context"
	"fmt"
	"time"
	"log/slog"

	"github.com/google/uuid"
	"github.com/townsag/reed/user_service/internal/config"
)

type User struct {
	UserId uuid.UUID
	UserName string
	Email string
	MaxDocuments int32
	HashedPassword string
	IsActive bool
	CreatedAt time.Time
	LastModified time.Time
}

// the consumer of the repository package defines the interface that
// the repository object has to conform to. This allows multiple repos
// to implement the UserRepository interface
type UserRepository interface {
	CreateUser(ctx context.Context, userName string, email string, maxDocuments int32, password string) (userId uuid.UUID, err error)
	GetUserById(ctx context.Context, userId uuid.UUID) (*User, error)
	GetUserByEmail(ctx context.Context, userEmail string) (*User, error)
	DeactivateUser(ctx context.Context, userId uuid.UUID) (error)
	// push the responsibility for hashing passwords down to the repository layer, the user service
	// just deals in plaintext passwords. This makes the interactions between the service and the 
	// repository cleaner because the service does not have to hold an interactive transaction in 
	// case another process changes the users password while the service is validating it
	ModifyPassword(ctx context.Context, userId uuid.UUID, oldPassword string, newPassword string) (error)
}

// in the case of repositories, we wanted to be able to swap out multiple different repository
// types with each using a different underlying implementation like postgres or dynamodb
// in the case of the UserService, I do not expect that we will want different implementations
// because there probably will not be two versions of the business logic. For this reason
// i will leave the user service as a struct and not an interface. The Server layer and the 
// service layer will be tightly coupled, unlike the service and the repository layer

// since the service layer and the server layer are already tightly coupled, I think i'll
// return domain errors from the service layer and map them to gRPC protocol statuses at
// the server layer

type UserService struct {
	repo UserRepository
}

func NewUserService(repo UserRepository) *UserService {
	return &UserService{
		repo: repo,
	}
}

// the guideline is to accept interfaces and return structs.. in these cases, I think the data is simple enough
// to just accept the data as individual arguments. This also prevents the boilerplate of having to make
// interfaces to pass between the server and the service layer and prevents the service layer from being
// aware of gRPC specific structs

func (us *UserService) CreateUser(ctx context.Context, userName string, email string, maxDocuments *int32, password string) (uuid.UUID, error) {
	if len(userName) < config.MinUsernameLength {
		slog.WarnContext(ctx, "failed to create user, username is too small", "userName", userName)
		return uuid.Nil, Invalid(
			fmt.Sprintf("username: <%s> did not match the min username length constraint: %d", userName, config.MinUsernameLength),
			nil,
		)
	}
	// TODO: validate the email using regex, etc.
	// TODO: create a sign-up flow that requires clicking a link in their inbox
	if len(password) < config.MinPasswordLength {
		slog.WarnContext(ctx, "failed to create user, password is too small", "password", password)
		return uuid.Nil, Invalid(
			fmt.Sprintf("password: <%s> did not match the min password length constraint: %d", password, config.MinPasswordLength),
			nil,
		)
	}
	resolvedMaxDocuments := config.DefaultMaxDocuments
	if maxDocuments != nil {
		resolvedMaxDocuments = *maxDocuments
	}
	userId, err := us.repo.CreateUser(ctx, userName, email, resolvedMaxDocuments, password)
	if err != nil {
		// we have no guarantee here that the error returned by the repo is a semantic / domain error
		// defined by the service package. Here we can perform a runtime check on the error to see
		// if it is one of our domain errors. If it is not, we can instead wrap it and return a domain error

		// I am still on the fence about this pattern, it is a bit verbose
		if _, ok := err.(DomainError); !ok {
			err = RepoImpl("unknown error creating user", err)
		}
		slog.ErrorContext(
			ctx,
			"failed to create user because of repository error",
			"error", err.Error(),
		)
		return uuid.Nil, err
	} else {
		return userId, nil
	}
}

func (us *UserService) GetUser(ctx context.Context, userId uuid.UUID) (*User, error) {
	user, err := us.repo.GetUserById(ctx, userId)
	if err != nil {
		if _, ok := err.(DomainError); !ok {
			err = RepoImpl("unknown error getting user", err)
		}
		slog.ErrorContext(
			ctx,
			"failed to get user because of repository error",
			"error", err.Error(),
		)
		return nil, err
	} else {
		return user, nil
	}
}

// calls to deactivate a user are like an upsert, if the user has already been deactivated they have no effect
func (us *UserService) DeactivateUser(ctx context.Context, userId uuid.UUID) error {
	err := us.repo.DeactivateUser(ctx, userId)
	if err != nil {
		if _, ok := err.(DomainError); !ok {
			err = RepoImpl("unknown error deactivating user", err)
		}
		slog.ErrorContext(
			ctx,
			"failed to deactivate user because of repository error",
			"error", err.Error(),
		)
	}
	return err
}

func (us *UserService) ChangePassword(ctx context.Context, userId uuid.UUID, oldPassword string, newPassword string) error {
	// TODO: add regex validation of the password
	err := us.repo.ModifyPassword(ctx, userId, oldPassword, newPassword)
	if err != nil {
		if _, ok := err.(DomainError); !ok {
			err = RepoImpl("unknown error modifying users password", err)
		}
		slog.ErrorContext(
			ctx,
			"failed to change password because of repository error",
			"error", err.Error(),
		)
	}
	return err
}

// Questions:
// where should I be defining the user service interface?
//	- current solution: don't define one, use a struct instead
// what is the calling code for the user service interface?
//	- the server in internal/server/user_server.go
// how much input validation do I need to do and how much input validation will be handled by gRPC?

/*
## What goes in the service layer:
- the service layer is the home of all business logic
	- examples:
		- make sure that a request to update a password includes the valid old password
		- hashing passwords
		- make sure that requests to change the users email conform to the valid syntax of an email
- the service layer does not know about gRPC or postgres or sql or redis etc.

## What goes in the server layer:
- this is network protocol specific implementation details
	- status codes
	- parsing requests and formatting responses
	- rate limiting
	- authentication
*/