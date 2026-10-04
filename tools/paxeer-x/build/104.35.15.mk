.PHONY: paxeer-x-arbiter-admission-build
paxeer-x-arbiter-admission-build: layerxd $(BUILD_DIR)/tests/lxp_test_arbiter_admission
	python3 tools/qualification/paxeer-x/arbiter-admission.py --build \
		--native='$(BUILD_DIR)/tests/lxp_test_arbiter_admission' \
		--daemon='$(BUILD_DIR)/bin/layerxd' --cargo='$(PROGRAMS_CARGO)'

$(BUILD_DIR)/tests/lxp_test_arbiter_admission: tests/daemon/lxp_test_arbiter_admission.c \
		tests/daemon/lxp_test_arbiter_prestate.c tests/programs/test_call_activity.c \
		$(filter-out $(BUILD_DIR)/obj/cmd/layerxd/main.o,$(LAYERXD_OBJECTS)) \
		$(LIBRARY) $(PROGRAMS_RUNTIME_LIB) | programs-build
	@mkdir -p $(@D)
	$(CC) $(CPPFLAGS) $(CFLAGS) -Icmd/layerxd $< \
		$(filter-out $(BUILD_DIR)/obj/cmd/layerxd/main.o,$(LAYERXD_OBJECTS)) \
		$(LIBRARY) $(PROGRAMS_RUNTIME_LIB) $(LIBRARY) $(EXTRA_LDFLAGS) \
		-lcrypto -lsqlite3 -pthread -ldl -lm -o $@
