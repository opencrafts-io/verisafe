install-tools:
  @echo '[+] Installing required tools & packages'
  go install go.uber.org/mock/mockgen@latest

generate-mocks: install-tools
  @which mockgen > /dev/null || (echo 'mockgen not found: go install github.com/golang/mock/mockgen@latest' && exit 1)
  @echo 'Generating mocks for external packages'
  mockgen -package mockscore github.com/jackc/pgx/v5 Tx > internal/core/mocks/mock_tx.go
  @echo '[+] Generated mock for pgx.Tx'
  @echo 'Scanning all directories for go:generate directives'
  go generate ./...
  @echo '[+] Done'


test:
    @go test ./... | grep -v "no test files" || true
