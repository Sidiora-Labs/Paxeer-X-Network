.PHONY: paxeer-x-arbiter-native-authority-build paxeer-x-arbiter-native-authority-runtime
paxeer-x-arbiter-native-authority-build: $(BUILD_DIR)/tests/lxp_test_arbiter_native_authority

paxeer-x-arbiter-native-authority-runtime:
	env CARGO_BUILD_JOBS=4 CARGO_TARGET_DIR='$(PROGRAMS_TARGET_DIR)' $(PROGRAMS_CARGO) build --locked --manifest-path programs/Cargo.toml -p layerx-programs-sandbox --features host-ffi

$(BUILD_DIR)/tests/lxp_test_arbiter_native_authority: tests/daemon/lxp_test_arbiter_native_authority.c \
		$(BUILD_DIR)/generated/replay-authority-fixture-base.inc tests/programs/test_call_activity.c \
		$(filter-out $(BUILD_DIR)/obj/cmd/layerxd/main.o,$(LAYERXD_OBJECTS)) \
		$(LIBRARY) paxeer-x-arbiter-native-authority-runtime
	@mkdir -p $(@D)
	$(CC) $(CPPFLAGS) $(CFLAGS) -Icmd/layerxd -I$(BUILD_DIR)/generated $< \
		$(filter-out $(BUILD_DIR)/obj/cmd/layerxd/main.o,$(LAYERXD_OBJECTS)) \
		$(LIBRARY) $(PROGRAMS_RUNTIME_LIB) $(LIBRARY) $(EXTRA_LDFLAGS) \
		-lssl -lcrypto -lsqlite3 -pthread -ldl -lm -o $@
