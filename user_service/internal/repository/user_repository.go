package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	sqlc "github.com/townsag/reed/user_service/internal/repository/sqlc/db"
	"github.com/townsag/reed/user_service/internal/service"
)

// ensure at compile time that the user repository struct implements that user
// repository interface that is defined at the service level
var _ service.UserRepository = (*UserRepository)(nil)

// TODO: figure out what the logging story is for the repo object?
// TODO: figure out the error handling story for the repo object?
//		- do we need to record that we got a not found error and then raise the not found error
//		- should we be raising a specific type of error?
// 		- how will the calling code know if we should return a 404 or a 500?
type UserRepository struct {
	queries *sqlc.Queries
	pool *pgxpool.Pool
}

// pgxpool implements the DBTX interface defined by the generated sqlc code
// func NewUserRepository(conn *pgxpool.Pool) *UserRepository {
// ^removed as to follow golang best practice of accepting interfaces and returning structs
func NewUserRepository(conn *pgxpool.Pool) *UserRepository {
	return &UserRepository{ queries: sqlc.New(conn), pool: conn }
}

// add the helper method for converting from the User struct defined by the generated
// sqlc code to the user struct defined in the service package here
func serviceToRepository(user service.User) *sqlc.User {
	return &sqlc.User{
		ID: pgtype.UUID{ Bytes: user.UserId, Valid: true },
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
		UserId: uuid.UUID(user.ID.Bytes),
		UserName: user.UserName,
		Email: user.Email,
		MaxDocuments: user.MaxDocuments.Int32,
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
	maxDocuments int32, 
	password string,
) (uuid.UUID, service.DomainError) {
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return uuid.Nil, service.RepoImpl("error creating hash of users new password", err)
	}
	userId := uuid.New()
	params := sqlc.CreateUserParams{
		ID: pgtype.UUID{ Bytes: userId, Valid: true },
		UserName: userName,
		Email: email,
		MaxDocuments: pgtype.Int4{ Int32: maxDocuments, Valid: true },
		HashedPassword: string(hashedPassword),
	}
	err = r.queries.CreateUser(ctx, params)
	if err != nil {
		var pgError *pgconn.PgError
		if errors.As(err, &pgError) {
			// parse the error code here and determine a semantic error type
			// unique conflict
			if pgError.Code == "23505" {
				return uuid.Nil, service.UniqueConflict(
					fmt.Sprintf("constraint: %s, detail: %s", pgError.ConstraintName, pgError.Detail), 
					err,
				)
			} else {
				// db implementation error
				return uuid.Nil, service.RepoImpl(pgError.Error(), pgError)
			}
		} else {
			return uuid.Nil, service.RepoImpl("unknown error encountered when creating user", err)
		}
	}
	return userId, nil
}

func (r *UserRepository) GetUserById(
	ctx context.Context,
	userId uuid.UUID,
) (*service.User, service.DomainError) {
	user, err := r.queries.GetUserById(ctx, pgtype.UUID{ Bytes: userId, Valid: true })
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, service.NotFound(fmt.Sprintf("No user found for userId: %d", userId))
		} else {
			return nil, service.RepoImpl(err.Error(), err)
		}
	}
	return repositoryToService(user), nil
}

func (r *UserRepository) GetUserByEmail(ctx context.Context, userEmail string) (*service.User, service.DomainError) {
	user, err := r.queries.GetUserByEmail(ctx, userEmail)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, service.NotFound(fmt.Sprintf("No user found with email: %s", userEmail))
		} else {
			return nil, service.RepoImpl(err.Error(), err)
		}
	}
	return repositoryToService(user), nil
}

func (r *UserRepository) DeactivateUser (ctx context.Context, userId uuid.UUID) service.DomainError {
	_, err := r.queries.DeactivateUser(ctx, pgtype.UUID{ Bytes: userId, Valid: true })
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return service.NotFound(fmt.Sprintf("No user found with userId: %d to deactivate", userId))
		} else {
			return service.RepoImpl(err.Error(), err)
		}
	}
	return nil
}

func (r *UserRepository) ModifyPassword(
	ctx context.Context, 
	userId uuid.UUID, 
	oldPassword string, 
	newPassword string,
) service.DomainError {
	// create a transaction
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return service.RepoImpl(
			"failed to create a transaction when modifying password",
			err,
		)
	}
	defer tx.Rollback(ctx)
	txQueries := r.queries.WithTx(tx)
	// read the password associated with this user
	user, err := txQueries.GetUserForUpdate(ctx, pgtype.UUID{ Bytes: userId, Valid: true })
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return service.NotFound(fmt.Sprintf("No user found with userId: %d to update", userId))
		} else {
			return service.RepoImpl("unexpected error found when reading user", err)
		}
	}
	// validate that the old password matches the hashed password in the database
	if err = bcrypt.CompareHashAndPassword([]byte(user.HashedPassword), []byte(oldPassword)); err != nil {
		return service.PasswordMismatch(err)
	}
	// update the database to reflect the change in hashed password
	newHashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return service.RepoImpl("error creating hash of users new password", err)
	}
	param := sqlc.ChangeUserPasswordParams{
		HashedPassword: string(newHashedPassword),
		ID: user.ID,
	}
	_, err = txQueries.ChangeUserPassword(ctx, param)
	if err != nil {
		return service.RepoImpl("error updating user record with new hashed password", err)
	}
	err = tx.Commit(ctx)
	if err != nil {
		return service.RepoImpl("error committing the update password hash transaction", err)
	} 
	return nil
}

func (r *UserRepository) ValidatePassword(
	ctx context.Context,
	userName string,
	password string,
) (uuid.UUID, bool, service.DomainError) {
	// read the password associated with the user
	row, err := r.queries.GetHashedPassword(
		ctx, userName,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, false, service.NotFound(fmt.Sprintf(
				"no user found with user name: %s for checking password", 
				userName,
			))
		} else {
			return uuid.Nil, false, service.RepoImpl(
				fmt.Sprintf("unexpected error found when reading user with username: %s", userName),
				err,
			)
		}
	}
	// hash the given users password and compare the hashed password to the stored hashed password
	if err := bcrypt.CompareHashAndPassword([]byte(row.HashedPassword), []byte(password)); err != nil {
		return uuid.Nil, false, nil
	}
	return uuid.UUID(row.ID.Bytes), true, nil
}

// consider adding something like this
// func (r *PostgresUserRepository) UpdateByID(ctx context.Context, userID int, updateFn func(user *User) (bool, error)) error {
// https://threedots.tech/post/database-transactions-in-go/
// This is a generic way for us to update a user record, it allows us to define the update application logic in the 
// service layer but define the update database logic in the repository layer