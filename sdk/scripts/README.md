# scripts

Helper scripts inherited from the upstream Cosmos SDK. Run them from `sdk/`.

| Script | What it does |
| ------ | ------------ |
| `module-tests.sh` | Runs `go test` with coverage in every nested Go module under the current directory (each `go.mod` other than `./go.mod`) and merges the profiles into `coverage-go-submod-profile.out`. If `GIT_DIFF` is set, modules with no changed files are skipped. |
| `protoc-swagger-gen.sh` | Generates Swagger files from the `query.proto` and `service.proto` files under `./proto` with `buf protoc`. The `proto-swagger-gen` target in [`sdk/Makefile`](../Makefile) calls it. |
| `update-swagger-ui-statik.sh` | Runs `statik` to embed `client/docs/swagger-ui` into `client/docs`. |
| `linkify_changelog.py` | Rewrites ` \#<number>` references in a changelog file into links to upstream Cosmos SDK issues. |
