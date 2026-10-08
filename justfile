# swag and mockgen are pinned as tool dependencies in go.mod, so `go tool`
# runs the exact versions CI runs. The lint recipe uses the same golangci-lint
# configuration as CI.

generate-mocks:
  @echo '[+] Scanning all packages for go:generate directives'
  go generate ./...
  @echo '[+] Done'

test:
    go test ./...

swag:
    go tool swag init --parseDependency --parseInternal

# Apply the standard Go formatter to every package.
format:
    rg --files -0 -g '*.go' | xargs -0 gofmt -w

# Run the same blocking linter configured in CI.
lint:
    golangci-lint run ./...

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
