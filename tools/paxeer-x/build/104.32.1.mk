PAXEER_X_CACHE_DIR ?= $(abspath $(BUILD_DIR)/paxeer-x-cache)
PAXEER_X_CACHE_CARGO_DIR ?= $(or $(CARGO_TARGET_DIR),$(PROGRAMS_TARGET_DIR))
PAXEER_X_CACHE_NATIVE := $(BUILD_DIR)/tests/programs_cache_native_equivalence
PAXEER_X_CACHE_STATIC := $(PAXEER_X_CACHE_CARGO_DIR)/debug/liblayerx_programs_sandbox.a
PAXEER_X_CACHE_ADAPTER := tools/qualification/paxeer-x/program_cache_native_equivalence.py

.PHONY: paxeer-x-build-104.32.1
paxeer-x-build-104.32.1:
	python3 $(PAXEER_X_CACHE_ADAPTER) snapshot --compiler '$(CC)' --cargo '$(PROGRAMS_CARGO)' --cppflags='$(CPPFLAGS)' --cflags='$(CFLAGS)' --ldflags='$(EXTRA_LDFLAGS)'
	$(MAKE) --no-print-directory $(LIBRARY) $(CHECKPOINT_SETTLEMENT_HEADER)
	@mkdir -p '$(PAXEER_X_CACHE_DIR)/guests' '$(dir $(PAXEER_X_CACHE_NATIVE))'
	env CARGO_TARGET_DIR='$(PAXEER_X_CACHE_CARGO_DIR)' $(PROGRAMS_CARGO) build --locked --manifest-path programs/Cargo.toml -p layerx-programs-sandbox --features host-ffi
	env CARGO_TARGET_DIR='$(PAXEER_X_CACHE_CARGO_DIR)' $(PROGRAMS_CARGO) test --locked --manifest-path programs/Cargo.toml -p layerx-programs-runtime --lib --no-run --message-format=json cache::tests > '$(PAXEER_X_CACHE_DIR)/cargo-tests.jsonl'
	$(CC) $(CPPFLAGS) $(CFLAGS) tests/programs/test_cache_native_equivalence.c $(LIBRARY) '$(PAXEER_X_CACHE_STATIC)' $(LIBRARY) $(EXTRA_LDFLAGS) -lcrypto -pthread -ldl -lm -o '$(PAXEER_X_CACHE_NATIVE)'
	'$(PAXEER_X_CACHE_NATIVE)' --emit-guests '$(PAXEER_X_CACHE_DIR)/guests'
	python3 $(PAXEER_X_CACHE_ADAPTER) record --native '$(PAXEER_X_CACHE_NATIVE)' --library '$(LIBRARY)' --staticlib '$(PAXEER_X_CACHE_STATIC)' --cargo-json '$(PAXEER_X_CACHE_DIR)/cargo-tests.jsonl' --guests '$(PAXEER_X_CACHE_DIR)/guests' --generated-header '$(CHECKPOINT_SETTLEMENT_HEADER)'
