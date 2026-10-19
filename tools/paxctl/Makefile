BIN       := seictl
BUILD_DIR := ./build
VERSION   := $(shell jq -r .version version.json)

# Guard against ldflag injection from a malicious version.json. The
# value lands inside `-ldflags "-X '...=$(VERSION)'"`; a quote or
# whitespace would let an attacker rewrite arbitrary symbols at link
# time. Restrict to standard semver shape.
VERSION_OK := $(shell printf '%s' '$(VERSION)' | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+(-[A-Za-z0-9.-]+)?$$' >/dev/null && echo ok)
ifneq ($(VERSION_OK),ok)
$(error version.json's .version ($(VERSION)) does not match ^v[0-9]+\.[0-9]+\.[0-9]+(-[A-Za-z0-9.-]+)?$$)
endif

LDFLAGS := -X 'github.com/sei-protocol/seictl/internal/cliutil.Version=$(VERSION)'

.PHONY: build install run test lint fmt generate clean

build:
	go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BIN) .

install:
	go install -ldflags "$(LDFLAGS)" .

run:
	go run . $(ARGS)

test:
	go test ./...

lint:
	gofmt -s -l .

fmt:
	gofmt -s -w .

generate:
	go generate ./...

clean:
	rm -rf $(BUILD_DIR)
