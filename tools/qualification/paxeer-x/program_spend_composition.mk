.PHONY: paxeer-x-build-104.30.4 paxeer-x-native-104.30.4

paxeer-x-build-104.30.4:
	python3 tools/qualification/paxeer-x/program_spend_composition.py --build --build-dir '$(BUILD_DIR)' --cc '$(CC)'

paxeer-x-native-104.30.4: $(BUILD_DIR)/tests/programs_spend_capability_composition

$(BUILD_DIR)/tests/programs_spend_capability_composition: tests/programs/test_spend_capability_composition.c tests/programs/test_call_activity.c $(LIBRARY) $(CHECKPOINT_SETTLEMENT_HEADER)
	@mkdir -p $(@D)
	$(CC) $(CPPFLAGS) $(CFLAGS) $< -Wl,--start-group $(LIBRARY) $(PAXEER_SPEND_RUNTIME_LIB) -Wl,--end-group $(EXTRA_LDFLAGS) -lssl -lcrypto -lsqlite3 -pthread -ldl -lm -o $@
