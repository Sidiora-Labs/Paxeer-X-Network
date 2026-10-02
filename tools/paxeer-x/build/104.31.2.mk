PAXEER_X_BALANCE_DIR ?= $(abspath $(BUILD_DIR)/paxeer-x-balance)
PAXEER_X_BALANCE_CARGO_DIR ?= $(or $(CARGO_TARGET_DIR),$(PROGRAMS_TARGET_DIR))
PAXEER_X_BALANCE_NATIVE := $(BUILD_DIR)/tests/programs_balance_read
PAXEER_X_BALANCE_ACCOUNTS := $(BUILD_DIR)/tests/programs_balance_existing_accounts
PAXEER_X_BALANCE_STATIC := $(PAXEER_X_BALANCE_CARGO_DIR)/debug/liblayerx_programs_sandbox.a
PAXEER_X_BALANCE_ADAPTER := tools/qualification/paxeer-x/program_balance_read.py

.PHONY: paxeer-x-build-104.31.2
paxeer-x-build-104.31.2:
	python3 $(PAXEER_X_BALANCE_ADAPTER) snapshot --compiler '$(CC)' --cargo '$(PROGRAMS_CARGO)' --cppflags='$(CPPFLAGS)' --cflags='$(CFLAGS)' --ldflags='$(EXTRA_LDFLAGS)'
	$(MAKE) --no-print-directory $(LIBRARY) $(CHECKPOINT_SETTLEMENT_HEADER)
	@mkdir -p '$(PAXEER_X_BALANCE_DIR)/guests' '$(dir $(PAXEER_X_BALANCE_NATIVE))'
	env CARGO_TARGET_DIR='$(PAXEER_X_BALANCE_CARGO_DIR)' $(PROGRAMS_CARGO) build --locked --manifest-path programs/Cargo.toml -p layerx-programs-sandbox --features host-ffi
	env CARGO_TARGET_DIR='$(PAXEER_X_BALANCE_CARGO_DIR)' $(PROGRAMS_CARGO) test --locked --manifest-path programs/Cargo.toml -p layerx-programs-runtime --lib --no-run --message-format=json balance_sight_ > '$(PAXEER_X_BALANCE_DIR)/cargo-tests.jsonl'
	$(CC) $(CPPFLAGS) $(CFLAGS) tests/programs/test_balance_read.c $(LIBRARY) '$(PAXEER_X_BALANCE_STATIC)' $(LIBRARY) $(EXTRA_LDFLAGS) -lcrypto -lsqlite3 -pthread -ldl -lm -o '$(PAXEER_X_BALANCE_NATIVE)'
	$(CC) $(CPPFLAGS) $(CFLAGS) tests/programs/test_accounts.c $(LIBRARY) '$(PAXEER_X_BALANCE_STATIC)' $(LIBRARY) $(EXTRA_LDFLAGS) -lcrypto -lsqlite3 -pthread -ldl -lm -o '$(PAXEER_X_BALANCE_ACCOUNTS)'
	'$(PAXEER_X_BALANCE_NATIVE)' --emit-guests '$(PAXEER_X_BALANCE_DIR)/guests'
	python3 $(PAXEER_X_BALANCE_ADAPTER) record --native '$(PAXEER_X_BALANCE_NATIVE)' --accounts-tests '$(PAXEER_X_BALANCE_ACCOUNTS)' --library '$(LIBRARY)' --staticlib '$(PAXEER_X_BALANCE_STATIC)' --cargo-json '$(PAXEER_X_BALANCE_DIR)/cargo-tests.jsonl' --guests '$(PAXEER_X_BALANCE_DIR)/guests' --generated-header '$(CHECKPOINT_SETTLEMENT_HEADER)'
