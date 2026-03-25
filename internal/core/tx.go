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
