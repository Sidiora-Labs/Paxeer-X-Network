.PHONY: paxeer-x-arbiter-native-authority-build paxeer-x-arbiter-native-authority-runtime
paxeer-x-arbiter-native-authority-build: $(BUILD_DIR)/tests/lxp_test_arbiter_native_authority \
	$(BUILD_DIR)/tests/lxp_test_module_maintenance $(BUILD_DIR)/tests/lxp_test_daemon_finality_authority \
	$(BUILD_DIR)/tests/lxp_test_guarantor_runtime $(BUILD_DIR)/bin/layerxd \
	$(BUILD_DIR)/bin/layerx-genesis-build $(BUILD_DIR)/bin/layerx-handover

paxeer-x-arbiter-native-authority-runtime:
	env CARGO_BUILD_JOBS=4 CARGO_TARGET_DIR='$(PROGRAMS_TARGET_DIR)' $(PROGRAMS_CARGO) build --locked --manifest-path programs/Cargo.toml -p layerx-programs-sandbox --features host-ffi

$(BUILD_DIR)/tests/lxp_test_arbiter_native_authority: tests/daemon/lxp_test_arbiter_native_authority.c \
		$(filter-out $(BUILD_DIR)/obj/cmd/layerxd/main.o,$(LAYERXD_OBJECTS)) \
		$(LIBRARY) paxeer-x-arbiter-native-authority-runtime
	@mkdir -p $(@D)
	$(CC) $(CPPFLAGS) $(CFLAGS) -Icmd/layerxd $< -Wl,--wrap=layerx_programs_call_begin \
		$(filter-out $(BUILD_DIR)/obj/cmd/layerxd/main.o,$(LAYERXD_OBJECTS)) \
		$(LIBRARY) $(PROGRAMS_RUNTIME_LIB) $(LIBRARY) $(EXTRA_LDFLAGS) \
		-lssl -lcrypto -lsqlite3 -pthread -ldl -lm -o $@
