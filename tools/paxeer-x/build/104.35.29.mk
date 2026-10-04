.PHONY: paxeer-x-replay-authority-build
paxeer-x-replay-authority-build: $(BUILD_DIR)/tests/lxp_test_replay_authority

$(BUILD_DIR)/generated/replay-authority-fixture-base.inc: tests/daemon/lxp_test_arbiter_admission.c \
		tools/qualification/paxeer-x/program-replay-record.py \
		tools/qualification/paxeer-x/replay-authority.py
	python3 tools/qualification/paxeer-x/replay-authority.py --prepare-fixture='$@'

$(BUILD_DIR)/tests/lxp_test_replay_authority: tests/daemon/lxp_test_replay_authority.c \
		$(BUILD_DIR)/generated/replay-authority-fixture-base.inc tests/programs/test_call_activity.c \
		$(filter-out $(BUILD_DIR)/obj/cmd/layerxd/main.o,$(LAYERXD_OBJECTS)) \
		$(LIBRARY) $(PROGRAMS_RUNTIME_LIB) | programs-build
	@mkdir -p $(@D)
	$(CC) $(CPPFLAGS) $(CFLAGS) -Icmd/layerxd -I$(BUILD_DIR)/generated $< \
		$(filter-out $(BUILD_DIR)/obj/cmd/layerxd/main.o,$(LAYERXD_OBJECTS)) \
		$(LIBRARY) $(PROGRAMS_RUNTIME_LIB) $(LIBRARY) $(EXTRA_LDFLAGS) \
		-lssl -lcrypto -lsqlite3 -pthread -ldl -lm -o $@
