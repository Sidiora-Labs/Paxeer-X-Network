PAXEER_X_PROFILE2_ACCOUNTS_DIR ?= /root/lx-target/profile2-accounts
PAXEER_X_PROFILE2_RUNTIME_LIB ?= /root/lx-target/arbiter-prestate/rust/debug/liblayerx_programs_sandbox.a

.PHONY: paxeer-x-profile2-accounts-build paxeer-x-profile2-native
paxeer-x-profile2-accounts-build:
	python3 tools/qualification/paxeer-x/programs_accounts_winddown.py --task 6.6 --build \
		--build-dir='$(PAXEER_X_PROFILE2_ACCOUNTS_DIR)' --cargo='$(PROGRAMS_CARGO)'

paxeer-x-profile2-native: $(LAYERXD_OBJECTS) $(LIBRARY)
	@mkdir -p $(BUILD_DIR)/bin
	$(CC) $(CPPFLAGS) $(CFLAGS) $(LAYERXD_OBJECTS) \
		-Wl,--start-group $(LIBRARY) $(PAXEER_X_PROFILE2_RUNTIME_LIB) -Wl,--end-group \
		$(EXTRA_LDFLAGS) -lcrypto -lsqlite3 -pthread -ldl -lm -o $(BUILD_DIR)/bin/layerxd
	$(CC) $(CPPFLAGS) $(CFLAGS) cmd/layerx-genesis/main.c \
		cmd/layerx-genesis/lxp_genesis_build_cli.c cmd/layerx-genesis/lxp_genesis_builder.c \
		-Wl,--start-group $(LIBRARY) $(PAXEER_X_PROFILE2_RUNTIME_LIB) -Wl,--end-group \
		$(EXTRA_LDFLAGS) -lcrypto -pthread -ldl -lm -o $(BUILD_DIR)/bin/layerx-genesis-build
