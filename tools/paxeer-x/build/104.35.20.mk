.PHONY: paxeer-x-program-replay-build
paxeer-x-program-replay-build: $(BUILD_DIR)/tests/lxp_test_program_replay
	python3 tools/qualification/paxeer-x/program-replay-record.py --build \
		--native='$(BUILD_DIR)/tests/lxp_test_program_replay' --cargo='$(PROGRAMS_CARGO)' \
		--rust-target='$(PROGRAMS_TARGET_DIR)'

$(BUILD_DIR)/generated/program-replay-fixture-base.inc: tests/daemon/lxp_test_arbiter_admission.c tools/qualification/paxeer-x/program-replay-record.py
	python3 tools/qualification/paxeer-x/program-replay-record.py --prepare-fixture='$@'

$(BUILD_DIR)/tests/lxp_test_program_replay: tests/daemon/lxp_test_program_replay.c \
		$(BUILD_DIR)/generated/program-replay-fixture-base.inc tests/programs/test_call_activity.c \
		$(filter-out $(BUILD_DIR)/obj/cmd/layerxd/main.o,$(LAYERXD_OBJECTS)) \
		$(LIBRARY) $(PROGRAMS_RUNTIME_LIB) | programs-build
	@mkdir -p $(@D)
	$(CC) $(CPPFLAGS) $(CFLAGS) -Icmd/layerxd -I$(BUILD_DIR)/generated $< \
		$(filter-out $(BUILD_DIR)/obj/cmd/layerxd/main.o,$(LAYERXD_OBJECTS)) \
		$(LIBRARY) $(PROGRAMS_RUNTIME_LIB) $(LIBRARY) $(EXTRA_LDFLAGS) \
		$(PROGRAMS_NATIVE_LDLIBS) -lcrypto -lsqlite3 -pthread -ldl -lm -o $@
