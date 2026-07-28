package core

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// IDBProvider defines the contract for any database source capable of
// starting a transaction. This is typically satisfied by *pgxpool.Pool
// in production and by mocks in unit tests.
//
// pgx.Tx is mocked here too, rather than from a shell recipe, so that
// `go generate ./...` is the only command that has to be remembered.
//
//go:generate go tool mockgen -source=db_provider.go -destination=mocks/db_provider.go -package=mockscore
//go:generate go tool mockgen -package=mockscore -destination=mocks/mock_tx.go github.com/jackc/pgx/v5 Tx
type IDBProvider interface {
	Acquire(ctx context.Context) (IDBConnection, error)
}

// IDBConnection wraps a single acquired database connection.
// Release must always be called when the connection is no longer needed
// to return it to the pool.
type IDBConnection interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	Release()
}
