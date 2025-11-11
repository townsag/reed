package config

import (
	"strconv"
	"fmt"
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/exaring/otelpgx"

	"github.com/townsag/reed/user_service/internal/util"
)

func GetConfiguration() (*pgxpool.Config, error) {
	var portEnv string = util.GetEnvWithDefault("POSTGRES_PORT", "5432")
	port, err := strconv.Atoi(portEnv)
	if err != nil {
		port = 5432
	}
	host := util.GetEnvWithDefault("POSTGRES_HOST", "localhost")
	dbName := util.GetEnvWithDefault("POSTGRES_DB", "postgres")
	user := util.GetEnvWithDefault("POSTGRES_USER", "admin")
	password := util.GetEnvWithDefault("POSTGRES_PASSWORD", "password")
	poolMaxCons := util.GetEnvWithDefault("POOL_MAX_CONS", "25")

	cfg, err := pgxpool.ParseConfig(fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s pool_max_conns=%s",
		host, port, user, password, dbName, poolMaxCons,
	))
	if err != nil {
		return nil, err
	}
	cfg.ConnConfig.Tracer = otelpgx.NewTracer()
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