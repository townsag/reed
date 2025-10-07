package repository_test

import (
	"errors"
	"os"
	"testing"

	"github.com/townsag/reed/user_service/internal/repository"
	"github.com/townsag/reed/user_service/internal/service"
	"golang.org/x/crypto/bcrypt"
)

func TestMain(m *testing.M) {
	// run the tests
	code := m.Run()
	// now that the tests have been run, cleanup the postgres container
	cleanupPostgresContainer()
	os.Exit(code)
}

// verify the happy path on creating and retrieving a user by id
func TestCreateUser(t *testing.T) {
	conn, err := setupPostgresContainer()
	if err != nil {
		t.Fatalf("unable to connect to postgres test container: %v", err)
	}
	var userRepo *repository.UserRepository = repository.NewUserRepository(conn)
	// pass some dummy data to the user repository create user function
	userId, err := userRepo.CreateUser(t.Context(), "testUser", "test@example.com", 100, "asdfasdf")
	if err != nil {
		t.Fatalf("failed to create user with error: %v", err)
	}
	// call the get user by id function to validate that the create user function worked 
	user, err := userRepo.GetUserById(t.Context(), userId)
	if err != nil {
		t.Fatalf("failed to retrieve user from the database by id: %v", err)
	}
	if user.UserId != userId {
		t.Errorf("want userId: %d, got userId: %d", userId, user.UserId)
	}
	if user.UserName != "testUser" {
		t.Errorf("want userName: %s, got userName: %s", "testUser", user.UserName)
	}
	if user.Email != "test@example.com" {
		t.Errorf("want userEmail: %s, got userEmail: %s", "test@example.com", user.Email)
	}
	if user.MaxDocuments != 100 {
		t.Errorf("want maxDocuments: %d, got maxDocuments: %d", 100, user.MaxDocuments)
	}
	err = bcrypt.CompareHashAndPassword([]byte(user.HashedPassword), []byte("asdfasdf"))
	if err != nil {
		t.Errorf("failed to validate that the stored hashed password is the hash of the provided password: %v", err)
	}
}

// verify the happy path on creating and retrieving a user by email
func TestGetUserEmail(t *testing.T) {
	conn, err := setupPostgresContainer()
	if err != nil {
		t.Fatalf("unable to connect to postgres test container: %v", err)
	}
	var userRepo *repository.UserRepository = repository.NewUserRepository(conn)
	userId, err := userRepo.CreateUser(t.Context(), "testUser2", "test2@example.com", 100, "asdfasdf")
	if err != nil {
		t.Fatalf("failed to create test user: %v", err)
	}
	user, err := userRepo.GetUserByEmail(t.Context(), "test2@example.com")
	if err != nil {
		t.Fatalf("failed to retreive user by email: %v", err)
	}
	if user.UserId != userId {
		t.Fatalf("when retrieving user by email, got userId: %d, want userId: %d", user.UserId, userId)
	}
}

// verify the failure path on creating a user: we should not be able to create a user with a duplicate email or username
func TestCreateDuplicateUserEmail(t *testing.T) {
	conn, err := setupPostgresContainer()
	if err != nil {
		t.Fatalf("unable to connect to postgres container: %v", err)
	}
	var userRepo *repository.UserRepository = repository.NewUserRepository(conn)
	_, err = userRepo.CreateUser(t.Context(), "testUser3", "test3@example.com", 100, "asdf")
	if err != nil {
		t.Fatalf("unable to create a new user: %v", err)
	}
	// create a user with the same duplicate email
	_, err = userRepo.CreateUser(t.Context(), "testUser3Duplicate", "test3@example.com", 100, "asdf")
	var uniqueError *service.UniqueConflictError
	// errors.Is is useful for type equivalence checks on the tree of errors
	// errors.As traverses the tree of errors and finds the first error in the tree that can be assigned
	// to the type pointed at by target. Target had to be a pointer to a pointer because we are going to
	// modify the pointer target itself instead of modifying the value pointed to by target
	if !errors.As(err, &uniqueError) {
		t.Errorf("when creating a duplicate user, want unique error, got: %v", err)
	}
}

// verify the failure path on creating a user: we should not be able to create a user with duplicate username
func TestCreateDuplicateUserName(t *testing.T) {
	conn, err := setupPostgresContainer()
	if err != nil {
		t.Fatalf("unable to connect to postgres container: %v", err)
	}
	var userRepo *repository.UserRepository = repository.NewUserRepository(conn)
	_, err = userRepo.CreateUser(t.Context(), "testUser4", "test4@example.com", 100, "asdf")
	if err != nil {
		t.Fatalf("unable to create a new user: %v", err)
	}
	// create a user with the same duplicate email
	_, err = userRepo.CreateUser(t.Context(), "testUser4", "test4Duplicate@example.com", 100, "asdf")
	var uniqueError *service.UniqueConflictError
	// errors.Is is useful for type equivalence checks on the tree of errors
	// errors.As traverses the tree of errors and finds the first error in the tree that can be assigned
	// to the type pointed at by target. Target had to be a pointer to a pointer because we are going to
	// modify the pointer target itself instead of modifying the value pointed to by target
	if !errors.As(err, &uniqueError) {
		t.Errorf("when creating a duplicate user, want unique constraint error, got: %v", err)
	}
}

// verify the failure path on getting a user by id: we should not be able to get a user that does not exist
func TestGetMissingUserId(t *testing.T) {
	conn, err := setupPostgresContainer()
	if err != nil {
		t.Fatalf("unable to connect to postgres container: %v", err)
	}
	var userRepo *repository.UserRepository = repository.NewUserRepository(conn)
	// try to get a user that does not exist
	_, err = userRepo.GetUserById(t.Context(), 1234)
	var notFoundError *service.NotFoundError
	// errors.As traverses the error chain until it finds an error of the same type as the value of the pointer target
	// errors are interface (pointer) types so we would be looking for an error of type (*service.DomainError)
	// it then assigned the value pointed at by target (also a pointer) to the address of the error that matches the
	// type of target
	// Target must be a non nil pointer to a type that implements error (pointers to custom errors implement the error interface)
	// We need to modify the value of domain error (pointer) instead of modifying the memory pointed to by domain error because initially
	// domain error is not pointing at any memory, it is nil. Also, we do not want to allocate more memory, we just want to assign
	// the value that is pointed to by target to the value of the pointer to the found error
	if !errors.As(err, &notFoundError) {
		t.Errorf("when getting a user that does not exist, expected not found error, got: %v", err)
	}
}

// verify the failure path on getting a user by email: we should not be able to get a user that does not exist
func TestGetMissingUserEmail(t *testing.T) {
	conn, err := setupPostgresContainer()
	if err != nil {
		t.Fatalf("unable to connect to postgres container: %v", err)
	}
	var userRepo *repository.UserRepository = repository.NewUserRepository(conn)
	// try to get a user that does not exist
	_, err = userRepo.GetUserByEmail(t.Context(), "missing@example.com")
	var notFoundError *service.NotFoundError
	if !errors.As(err, &notFoundError) {
		t.Errorf("when getting a user that does not exist, expected not found error, got: %v", err)
	}
}

// verify the happy path on deactivating a user
//	- also verify that deactivating a user updates it's last modified
func TestDeactivateUser(t *testing.T) {
	conn, err := setupPostgresContainer()
	if err != nil {
		t.Fatalf("unable to connect to postgres container: %v", err)
	}
	var userRepo *repository.UserRepository = repository.NewUserRepository(conn)
	// create a user
	userId, err := userRepo.CreateUser(t.Context(), "testUser5", "test5@example.com", 10, "asdf")
	if err != nil {
		t.Fatalf("unable to create a new user: %v", err)
	}
	// deactivate that user
	err = userRepo.DeactivateUser(t.Context(), userId)
	if err != nil {
		t.Fatalf("unable to deactivate user: %v", err)
	}
	user, err := userRepo.GetUserById(t.Context(), userId)
	if err != nil {
		t.Fatalf("unable to get user by id after deactivating it: %v", err)
	}
	// validate that the user is deactivated
	if user.IsActive {
		t.Errorf("want user.IsActive to be false, got: %t", user.IsActive)
	}
	// validate that the modified date is different than the created date
	isBefore := user.CreatedAt.Before(user.LastModified)
	if !isBefore {
		t.Errorf("want created at to be before last modified: %t, found: %t", true, isBefore)
	}
}

// verify the failure path on deactivating a user: we should not be able to deactivate a user that does not exist
func TestDeactivateUserNotFound(t *testing.T) {
	conn, err := setupPostgresContainer()
	if err != nil {
		t.Fatalf("unable to connect to postgres container: %v", err)
	}
	var userRepo *repository.UserRepository = repository.NewUserRepository(conn)
	err = userRepo.DeactivateUser(t.Context(), 1234)
	var notFoundErr *service.NotFoundError
	if !errors.As(err, &notFoundErr) {
		t.Errorf("when deactivating a user that does not exist, want not found error, got: %v", err)
	}
}

// verify the happy path on modifying a password
// - also verify that modifying a users password updates it's last modified
func TestModifyPassword(t *testing.T) {
	conn, err := setupPostgresContainer()
	if err != nil {
		t.Fatalf("unable to connect to postgres container: %v", err)
	}
	var userRepo *repository.UserRepository = repository.NewUserRepository(conn)
	userId, err := userRepo.CreateUser(t.Context(), "testUser6", "test6@example.com", 12, "asdf")
	if err != nil {
		t.Fatalf("failed to create a user: %v", err)
	}
	// update the hashed password of the user
	err = userRepo.ModifyPassword(t.Context(), userId, "asdf", "qwer")
	if err != nil {
		t.Fatalf("failed to modify the password: %v", err)
	}
	user, err := userRepo.GetUserById(t.Context(), userId)
	if err != nil {
		t.Fatalf("failed to get the modified user: %v", err)
	}
	// verify that the hashed password is updated
	err = bcrypt.CompareHashAndPassword([]byte(user.HashedPassword), []byte("qwer"))
	if err != nil {
		t.Errorf("failed to validate that the new hashed password corresponds to the new password: %v", err)
	}
	// verify that updating the hashed password changed the last modified date
	isBefore := user.CreatedAt.Before(user.LastModified)
	if !isBefore {
		t.Errorf("got want created at to be before hashed password to be true, got: %t", isBefore)
	}
}

// verify the failure path on modifying a password: we should not be able to modify the password of a user that does not exist
func TestModifyPasswordNotFound(t *testing.T) {
		conn, err := setupPostgresContainer()
	if err != nil {
		t.Fatalf("unable to connect to postgres container: %v", err)
	}
	var userRepo *repository.UserRepository = repository.NewUserRepository(conn)
	err = userRepo.ModifyPassword(t.Context(), 1234, "zxcv", "qwer")
	var notFoundErr *service.NotFoundError
	if !errors.As(err, &notFoundErr) {
		t.Errorf("when modifying the password of a user that does not exist, want not found error, got: %v", err)
	}
}