package repository

import (
	"context"
	"errors"
	"fmt"

	sqlc "github.com/townsag/reed/user_service/internal/repository/sqlc/db"
	"github.com/townsag/reed/user_service/internal/service"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgconn"
)

// TODO: figure out what the logging story is for the repo object?
// TODO: figure out the error handling story for the repo object?
//		- do we need to record that we got a not found error and then raise the not found error
//		- should we be raising a specific type of error?
// 		- how will the calling code know if we should return a 404 or a 500?
type UserRepository struct {
	queries *sqlc.Queries
}

// pgxpool implements the DBTX interface defined by the generated sqlc code
// func NewUserRepository(conn *pgxpool.Pool) *UserRepository {
// ^removed as to follow golang best practice of accepting interfaces and returning structs
func NewUserRepository(conn sqlc.DBTX) *UserRepository {
	return &UserRepository{ queries: sqlc.New(conn)}
}

// add the helper method for converting from the User struct defined by the generated
// sqlc code to the user struct defined in the service package here
func serviceToRepository(user service.User) *sqlc.User {
	return &sqlc.User{
		ID: user.UserId,
		UserName: user.UserName,
		Email: user.Email,
		MaxDocuments: pgtype.Int4{ Int32: int32(user.MaxDocuments), Valid: true },
		HashedPassword: user.HashedPassword,
		IsActive: pgtype.Bool{ Bool:user.IsActive, Valid: true },
		CreatedAt: pgtype.Timestamp{ Time: user.CreatedAt, Valid: true },
		LastModified: pgtype.Timestamp{ Time: user.LastModified, Valid: true },
	}
}

func repositoryToService(user sqlc.User) *service.User {
	return &service.User{
		UserId: user.ID,
		UserName: user.UserName,
		Email: user.Email,
		MaxDocuments: int(user.MaxDocuments.Int32),
		HashedPassword: user.HashedPassword,
		IsActive: user.IsActive.Bool,
		CreatedAt: user.CreatedAt.Time,
		LastModified: user.LastModified.Time,
	}
}

func (r *UserRepository) CreateUser(
	ctx context.Context, 
	userName string,
	email string,
	maxDocuments int, 
	hashedPassword string,
) (userId int32, err error) {
	params := sqlc.CreateUserAndReturnIdParams{
		UserName: userName,
		Email: email,
		MaxDocuments: pgtype.Int4{ Int32: int32(maxDocuments), Valid: true },
		HashedPassword: hashedPassword,
	}
	userId, err = r.queries.CreateUserAndReturnId(ctx, params)
	if err != nil {
		var pgError *pgconn.PgError
		if errors.As(err, &pgError) {
			// TODO: parse the error code here and determine a semantic error type
			// conflict?
			// db implementation error?
			return 0, service.RepoImpl(err)
		} else {
			return 0, service.RepoImpl(err)
		}
	}
	return userId, nil
}

func (r *UserRepository) GetUserById(ctx context.Context, userId int32) (*service.User, error) {
	user, err := r.queries.GetUserById(ctx, userId)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, service.NotFound(fmt.Sprintf("No user found for userId: %d", userId))
		} else {
			return nil, service.RepoImpl(err)
		}
	}
	return repositoryToService(user), nil
}

func (r *UserRepository) GetUserByEmail(ctx context.Context, userEmail string) (*service.User, error) {
	user, err := r.queries.GetUserByEmail(ctx, userEmail)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, service.NotFound(fmt.Sprintf("No user found with email: %s", userEmail))
		}
	} else {
		return nil, service.RepoImpl(err)
	}
	return repositoryToService(user), nil
}

func (r *UserRepository) DeactivateUser (ctx context.Context, userId int32) error {
	_, err := r.queries.DeactivateUser(ctx, userId)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return service.NotFound(fmt.Sprintf("No user found with userId: %d to deactivate", userId))
		} else {
			return service.RepoImpl(err)
		}
	}
	return nil
}

func (r *UserRepository) ModifyPassword(ctx context.Context, userId int32, newHashedPassword string) (error) {
	params := sqlc.ChangeUserPasswordParams{
		HashedPassword: newHashedPassword,
		ID: userId,
	}
	_, err := r.queries.ChangeUserPassword(ctx, params)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return service.NotFound(fmt.Sprintf("No user found with userId: %d to update the password", userId))
		} else {
			return service.RepoImpl(err)
		}
	}
	return nil
}