# swag and mockgen are pinned as tool dependencies in go.mod, so `go tool`
# runs the exact versions CI runs. Nothing here needs anything on your PATH
# beyond the Go toolchain itself.

generate-mocks:
  @echo '[+] Scanning all packages for go:generate directives'
  go generate ./...
  @echo '[+] Done'

test:
    go test ./...

swag:
    go tool swag init --parseDependency --parseInternal

# Verify the committed artefacts match what the generators produce right now.
# This is the same check CI runs; run it locally to find out before CI does.
verify-generated: generate-mocks swag
    @git diff --exit-code -- docs/ ':(glob)**/mocks/**' \
      || (echo '[!] Generated files are stale — commit the diff above' && exit 1)
    @echo '[+] Generated files are up to date'

# Run this before pushing if you've touched any handler
pre-push: generate-mocks swag
    go build ./...
    go vet ./...
    go test ./...
