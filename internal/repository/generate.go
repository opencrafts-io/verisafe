package repository

// This file is hand-written. Everything else in this package is emitted by
// sqlc and carries a "DO NOT EDIT" banner, so the go:generate directive for
// the Querier mock lives here where regeneration cannot clobber it.
//
// Querier is mocked in reflect mode rather than source mode because the
// generated querier.go imports this package's own types; reflect mode
// resolves them through the compiled package instead of re-parsing the file.
//
//go:generate go tool mockgen -package=mockQuerier -destination=mocks/mock_querier.go github.com/opencrafts-io/verisafe/internal/repository Querier
