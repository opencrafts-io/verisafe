package database

import (
	"embed"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Runs the goose migrator effectively moving the database from one
// version to the next incase not already migrated
// note that the function may panic in the event of an error.
func RunGooseMigrations(logger *slog.Logger, pool *pgxpool.Pool) {

	if err := goose.SetDialect(string(goose.DialectPostgres)); err != nil {
		panic(err)
	}

	goose.SetBaseFS(migrationsFS)

	db := stdlib.OpenDBFromPool(pool)

	if err := goose.Up(db, "migrations"); err != nil {
		panic(err)
	}

	logger.Info("Migrations ran and were completed successfully")
}
