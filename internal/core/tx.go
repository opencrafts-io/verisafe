// internal/core/tx.go
package core

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// WithTransaction executes a unit of work within a database transaction.
//
// It handles the full transaction lifecycle:
//  1. Starts a transaction via the IDBProvider.
//  2. Executes the provided 'fn' closure.
//  3. Guards against failures: if 'fn' or 'Commit' returns an error,
//     the transaction is automatically rolled back.
//  4. Commits the transaction if the closure finishes without error.
//  5. Automatically closes the provided connection
//
// Note: We use a named return parameter 'err' to ensure the deferred
// rollback block can inspect the final error state of the function.
// InTx acquires a connection, runs fn inside a transaction, commits, and
// releases the connection.
//
// Prefer this to calling Acquire and WithTransaction separately. Because
// WithTransaction owns the Release, any early return between the two leaks a
// pooled connection permanently; here there is no window in which that can
// happen, since nothing is held when Acquire fails.
func InTx[T any](
	ctx context.Context,
	db IDBProvider,
	fn func(tx pgx.Tx) (T, error),
) (T, error) {
	var zero T

	conn, err := db.Acquire(ctx)
	if err != nil {
		return zero, fmt.Errorf("%w: failed to acquire connection", ErrInternal)
	}

	var out T
	if err := WithTransaction(ctx, conn, func(tx pgx.Tx) error {
		var ferr error
		out, ferr = fn(tx)
		return ferr
	}); err != nil {
		return zero, err
	}

	return out, nil
}

// InTxDo is InTx for work that produces no value.
func InTxDo(
	ctx context.Context,
	db IDBProvider,
	fn func(tx pgx.Tx) error,
) error {
	_, err := InTx(ctx, db, func(tx pgx.Tx) (struct{}, error) {
		return struct{}{}, fn(tx)
	})
	return err
}

func WithTransaction(
	ctx context.Context,
	conn IDBConnection,
	fn func(tx pgx.Tx) error,
) (err error) {
	defer conn.Release()

	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("%w: failed to begin transaction", ErrInternal)
	}

	// Defer the rollback guard.
	// This only executes if 'err' is non-nil when the function exits.
	defer func() {
		if err != nil {
			// Rollback is a best-effort cleanup; we ignore its error
			// to preserve the original business logic or commit error.
			tx.Rollback(ctx)
		}
	}()

	if err = fn(tx); err != nil {
		return err
	}

	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("%w: failed to commit tx", ErrInternal)
	}

	return nil
}
