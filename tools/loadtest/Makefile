# Loadtest_v2 Makefile
# Generates Go bindings for smart contracts and builds the paxload CLI

# Directories
CONTRACTS_DIR := generator/contracts
SCENARIOS_DIR := generator/scenarios
BINDINGS_DIR := generator/bindings
BUILD_DIR := build

# Binary configuration
BINARY_NAME := paxload
INSTALL_PATH := $(GOPATH)/bin
ifeq ($(GOPATH),)
	INSTALL_PATH := $(HOME)/go/bin
endif

# Tools
SOLC := /tmp/solc
ABIGEN := abigen
NVM_DIR := $(HOME)/.nvm
NODE_VERSION := 20

# Find all .sol files in contracts directory
SOL_FILES := $(wildcard $(CONTRACTS_DIR)/*.sol)
CONTRACT_NAMES := $(basename $(notdir $(SOL_FILES)))

# Generated files
ABI_FILES := $(addprefix $(BUILD_DIR)/, $(addsuffix .abi, $(CONTRACT_NAMES)))
BIN_FILES := $(addprefix $(BUILD_DIR)/, $(addsuffix .bin, $(CONTRACT_NAMES)))
BINDING_FILES := $(addprefix $(BINDINGS_DIR)/, $(addsuffix .go, $(CONTRACT_NAMES)))
SCENARIO_TEMPLATE_FILES := $(addprefix $(SCENARIOS_DIR)/, $(addsuffix .go, $(CONTRACT_NAMES)))

.PHONY: generate clean help build-cli install setup-node build test lint

# Default target
help:
	@echo "Available targets:"
	@echo "  build        - Build the paxload CLI (alias for build-cli)"
	@echo "  test         - Run tests with coverage"
	@echo "  lint         - Run linting and static analysis"
	@echo "  setup-node   - Install nvm, Node.js 20, and solc"
	@echo "  generate     - Generate Go bindings and scenario templates for all contracts"
	@echo "  clean        - Remove generated files"
	@echo "  help         - Show this help message"
	@echo "  build-cli    - Build the paxload CLI"
	@echo "  install      - Install the paxload CLI"

# Setup Node.js environment with nvm
setup-node:
	@echo "🔧 Setting up Node.js environment..."
	@if [ ! -d "$(NVM_DIR)" ]; then \
		echo "📦 Installing nvm..."; \
		curl -o- https://raw.githubusercontent.com/nvm-sh/nvm/v0.39.4/install.sh | bash; \
		echo "🔄 Sourcing nvm for current session..."; \
		export NVM_DIR="$(HOME)/.nvm"; \
		[ -s "$$NVM_DIR/nvm.sh" ] && . "$$NVM_DIR/nvm.sh"; \
	else \
		echo "✅ nvm already installed"; \
	fi
	@echo "🔧 Setting up Node.js $(NODE_VERSION)..."
	@export NVM_DIR="$(HOME)/.nvm" && \
	[ -s "$$NVM_DIR/nvm.sh" ] && . "$$NVM_DIR/nvm.sh" && \
	nvm install $(NODE_VERSION) && \
	nvm use $(NODE_VERSION)
	@echo "📦 Installing native solc binary..."
	@curl -L https://github.com/ethereum/solidity/releases/download/v0.8.19/solc-static-linux -o /tmp/solc && \
	chmod +x /tmp/solc
	@echo "✅ Node.js environment setup complete"
	@echo "ℹ️  Note: You may need to restart your shell or run 'source ~/.bashrc' to use nvm in new sessions"

# Main generate target
generate: $(BINDING_FILES) $(SCENARIO_TEMPLATE_FILES)
	@echo "🏭 Updating scenario factory..."
	@./scripts/update_factory.sh $(CONTRACT_NAMES)
	@echo "✅ Generated bindings and scenario templates for contracts: $(CONTRACT_NAMES)"

# Create build directory
$(BUILD_DIR):
	@mkdir -p $(BUILD_DIR)

# Create bindings directory
$(BINDINGS_DIR):
	@mkdir -p $(BINDINGS_DIR)

# Create scenarios directory
$(SCENARIOS_DIR):
	@mkdir -p $(SCENARIOS_DIR)

# Compile a single contract to ABI and bytecode
$(BUILD_DIR)/%.abi $(BUILD_DIR)/%.bin: $(CONTRACTS_DIR)/%.sol | $(BUILD_DIR)
	@echo "🔨 Compiling contract: $*"
	@$(SOLC) --abi --bin --optimize --overwrite -o $(BUILD_DIR) $<
	@echo "✅ Compiled: $*"

# Generate Go binding from ABI and bytecode
$(BINDINGS_DIR)/%.go: $(BUILD_DIR)/%.abi $(BUILD_DIR)/%.bin | $(BINDINGS_DIR)
	@echo "🏭 Generating Go binding: $*"
	@$(ABIGEN) --abi=$(BUILD_DIR)/$*.abi --bin=$(BUILD_DIR)/$*.bin --pkg=bindings --type=$* --out=$@
	@echo "✅ Generated binding: $*"

# Generate scenario template files (only if they don't exist)
$(SCENARIOS_DIR)/%.go: | $(SCENARIOS_DIR)
	@./scripts/generate_scenario_template.sh $* $@

# Clean generated files
clean:
	@echo "🧹 Cleaning generated files ..."
	@rm -rf $(BUILD_DIR) $(BINDINGS_DIR)
	@echo "✅ Cleaned up generated files"

# Check if required tools are installed
check-tools:
	@echo "🔍 Checking required tools ..."
	@which $(SOLC) > /dev/null || (echo "❌ solc not found. Run 'make setup-node' to install" && exit 1)
	@which $(ABIGEN) > /dev/null || (echo "❌ abigen not found. Run 'make install-tools' to install" && exit 1)
	@echo "✅ All required tools are available"

# Install tools (optional convenience target)
install-tools: setup-node
	@echo "📦 Installing required tools ..."
	@echo "Installing abigen ..."
	@go install github.com/ethereum/go-ethereum/cmd/abigen@latest
	@echo "✅ Tools installation complete"

# Build the paxload CLI binary
build-cli: | $(BUILD_DIR)
	@echo "🔨 Building CLI"
	@go mod tidy
	@go mod download
	@go build -o $(BUILD_DIR)/$(BINARY_NAME) .
	@echo "✅ Built CLI: $(BUILD_DIR)/$(BINARY_NAME)"

# Install the paxload CLI
install: build-cli
	@echo "📦 Installing CLI ..."
	@cp $(BUILD_DIR)/$(BINARY_NAME) $(INSTALL_PATH)/$(BINARY_NAME)
	@echo "✅ Installed CLI: $(BINARY_NAME)"

# Build the paxload CLI binary (alias for build-cli)
build: build-cli

# Run tests with coverage
test:
	@echo "🔍 Running tests with coverage..."
	@go test -v -race -coverprofile=coverage.out ./...
	@go tool cover -func=coverage.out
	@echo "✅ Tests passed"

# Run linting and static analysis
lint:
	@echo "🔍 Running linting and static analysis..."
	@golangci-lint run
	@echo "✅ Linting and static analysis passed"
