package core

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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
//
// It embeds Queryer because a pooled connection genuinely can be queried
// outside a transaction, and a dozen handlers do exactly that for single
// reads. Declaring the method set here rather than importing the identical
// repository.DBTX keeps core at the bottom of the dependency graph; Go
// interfaces are structural, so a value of this type still satisfies DBTX.
type IDBConnection interface {
	Queryer
	Begin(ctx context.Context) (pgx.Tx, error)
	Release()
}

// Queryer is the query surface shared by a pooled connection and a
// transaction. It is method-for-method repository.DBTX, which is what makes
// repository.New accept either.
type Queryer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}
