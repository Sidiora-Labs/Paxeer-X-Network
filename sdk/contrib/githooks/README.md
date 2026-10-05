# Git hooks

Installation, from the repository root:

```
git config core.hooksPath sdk/contrib/githooks
```

## pre-commit

The hook runs `gofmt -s`, `misspell` and `goimports` on the staged `.go`
files and adds them back to the commit. It skips paths matching `vendor/`,
`client/docs/statik/`, `tests/mocks/` and `*.pb.go`. It then runs
`go mod tidy` and stages `go.mod` and `go.sum`.

If any of `git`, `go`, `gofmt`, `goimports` or `misspell` is missing from
`$PATH`, the hook prints which one and exits without changing anything.
Install the two tools that do not ship with Go:

```
go install golang.org/x/tools/cmd/goimports@latest
go install github.com/golangci/misspell/cmd/misspell@latest
```

The hook still passes the upstream `-local github.com/cosmos/cosmos-sdk`
prefix to `goimports`, so it does not group imports of
`github.com/sidiora-labs/paxeer-network` as local.
