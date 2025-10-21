package sqlc

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

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