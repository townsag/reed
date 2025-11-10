package config

import (
	"strconv"
	"fmt"
	"context"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5"
	// "github.com/exaring/otelpgx"
	// TODO: ^add otel tracing to the pgxpool
)

func GetEnvWithDefault(key string, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}

func GetConfiguration() (*pgxpool.Config, error) {
	var portEnv string = GetEnvWithDefault("POSTGRES_PORT", "5432")
	port, err := strconv.Atoi(portEnv)
	if err != nil {
		port = 5432
	}
	host := GetEnvWithDefault("POSTGRES_HOST", "localhost")
	dbName := GetEnvWithDefault("POSTGRES_DB", "postgres")
	user := GetEnvWithDefault("POSTGRES_USER", "admin")
	password := GetEnvWithDefault("POSTGRES_PASSWORD", "password")
	poolMaxCons := GetEnvWithDefault("POOL_MAX_CONS", "25")

	cfg, err := pgxpool.ParseConfig(fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s pool_max_conns=%s",
		host, port, user, password, dbName, poolMaxCons,
	))
	if err != nil {
		return nil, err
	}
	// cfg.ConnConfig.Tracer = otelpgx.NewTracer()
	return cfg, nil	
}

func CreateDBConnectionPool(ctx context.Context, config *pgxpool.Config) (*pgxpool.Pool, error) {
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create a database connection pool: %w", err)
	}
	// if err = otelpgx.RecordStats(pool, otelpgx.WithMinimumReadDBStatsInterval(time.Second * 1)); err != nil {
	// 	pool.Close()
	// 	return nil, fmt.Errorf("failed to set up database connection pool observability: %w", err)
	// }
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping the new connection pool: %w", err)
	}
	return pool, nil
}

func RegisterTypes(ctx context.Context, conn *pgx.Conn) error {
	// get the type from the database so that we can register it with pgx
	t, err := conn.LoadTypes(
		ctx,
		[]string{"permission_level", "_permission_level", "recipient_type","_recipient_type"},
	)
	if err != nil {
		return fmt.Errorf("failed to read types from database: %w", err)
	}
	conn.TypeMap().RegisterTypes(t)
	return nil
}