package pilot

import (
	"context"
	"database/sql"
	"github.com/KdotIV/NostalgiaCade/internal/database"
)

// Migrate is retained for the operator CLI; catalog migrations now own the schema.
func Migrate(ctx context.Context, db *sql.DB) error {
	_, err := database.CleanupCatalog(ctx, db)
	return err
}
