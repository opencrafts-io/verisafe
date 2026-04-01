package core

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PgxPoolAdapter wraps *pgxpool.Pool to satisfy IDBProvider.
type PgxPoolAdapter struct {
	Pool *pgxpool.Pool
}

func (p *PgxPoolAdapter) Acquire(ctx context.Context) (IDBConnection, error) {
	return p.Pool.Acquire(
		ctx,
	) // *pgxpool.Conn satisfies IDBConnection via its Begin method
}
