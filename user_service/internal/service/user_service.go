package service

import (
	"time"
	"context"
)

type User struct {
	UserId int32
	UserName string
	Email string
	MaxDocuments int
	HashedPassword string
	IsActive bool
	CreatedAt time.Time
	LastModified time.Time
}

// the consumer of the repository package defines the interface that
// the repository object has to conform to. This allows multiple repos
// to implement the UserRepository interface
type UserRepository interface {
	CreateUser(ctx context.Context, userName string, email string, maxDocuments int, hashedPassword string) (userId int32, err error)
	GetUserById(ctx context.Context, userId int32) (*User, error)
	GetUserByEmail(ctx context.Context, userEmail string) (*User, error)
	DeactivateUser(ctx context.Context, userId int32) (error)
	ModifyPassword(ctx context.Context, userId int32, newHashedPassword string) (error)
}

type UserService struct {
	repo UserRepository
}

func NewUserService(repo UserRepository) *UserService {
	return &UserService{
		repo: repo,
	}
}