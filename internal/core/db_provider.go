package core

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// IDBProvider defines the contract for any database source capable of
// starting a transaction. This is typically satisfied by *pgxpool.Pool
// in production and by mocks in unit tests.
//
//go:generate mockgen -source=db_provider.go -destination=mocks/db_provider.go -package=mockscore
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
