package document_repository_test

import (
	"context"
	"sync"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/townsag/reed/document_service/internal/repository"
	"github.com/townsag/reed/document_service/internal/repository/sqlc"
)


var (
	testPool *pgxpool.Pool
	pgContainer *postgres.PostgresContainer
	pgErr error
	setupOncePG sync.Once
	cleanupOncePG sync.Once
)

/*
Problem:
- I want to run testing code against a live postgres database instance
	- this will allow the unit tests to better reflect the way that the service
	  will be used in production

Approach:
- add a factory method and a teardown function for the postgres testcontainers container
	- this allows us to pass a live postgres connection to our service struct when we create
	  it instead of a mock
- use the once pattern to prevent the testcontainer from being initialized or destroyed more
  than once
*/

func setupPostgresContainer() (*pgxpool.Pool, error) {
	// do we want to preserve the error between invocations of the function?
	setupOncePG.Do(
		func ()  {
			ctx := context.Background()
			fmt.Println("creating postgres testcontainer")
			pgContainer, pgErr = postgres.Run(
				ctx,
				"postgres:17-alpine",
				postgres.WithInitScripts(filepath.Join("..", "sqlc", "sql", "schema.sql")),
				postgres.WithDatabase("testing"),
				postgres.WithUsername("testing"),
				postgres.WithPassword("testing"),
				postgres.BasicWaitStrategies(),
			)
			if pgErr != nil {
				pgErr = fmt.Errorf("failed to start testing postgres container: %w", pgErr)
				return
			}
			// create a connection to the running postgres container
			var dbURL string
			dbURL, pgErr = pgContainer.ConnectionString(ctx)
			if pgErr != nil {
				pgErr = fmt.Errorf("unable to connect to postgres container: %w", pgErr)
				return
			}
			// use new with config so that we can register types when connecting
			// to the database 
			var config *pgxpool.Config
			config, pgErr = pgxpool.ParseConfig(dbURL)
			if pgErr != nil {
				pgErr = fmt.Errorf("failed to parse connection string: %w", pgErr)
				return
			}
			config.AfterConnect = sqlc.RegisterTypes
			testPool, pgErr = pgxpool.NewWithConfig(ctx, config)
			if pgErr != nil {
				pgErr = fmt.Errorf("unable to create a connection pool: %w", pgErr)
				return
			}
		},
	)
	return testPool, pgErr
}

func cleanupPostgresContainer() error {
	var err error = nil
	cleanupOncePG.Do(
		func() {
			if testPool != nil {
				testPool.Close()
			}
			fmt.Println("cleaning up the postgres testing container")
			if pgContainer != nil {
				err = testcontainers.TerminateContainer(pgContainer)
				if err != nil {
					err = fmt.Errorf("unable to terminate the postgres testcontainer: %w", err)
					return
				}
			}
		},
	)
	return err
}

func createTestingDocumentRepo(t *testing.T) *repository.DocumentRepository {
	pool, err := setupPostgresContainer()
	if err != nil {
		t.Fatalf("failed to create a connection to the postgres container: %v", err)
	}
	documentRepo := repository.NewDocumentRepository(pool)
	return documentRepo
}