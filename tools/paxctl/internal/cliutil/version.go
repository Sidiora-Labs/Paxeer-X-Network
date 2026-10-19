package cliutil

// Version is stamped onto the seictl.sei.io/version provenance annotation
// by both command trees' render(). Linker override:
//
//	-ldflags "-X 'github.com/sei-protocol/seictl/internal/cliutil.Version=$VERSION'"
//
// `make build` wires version.json; bare `go build`/`go test` see "dev".
var Version = "dev"
