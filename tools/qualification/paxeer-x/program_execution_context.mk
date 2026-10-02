.PHONY: paxeer-x-build-104.31.1 paxeer-x-native-104.31.1

paxeer-x-build-104.31.1:
	python3 tools/qualification/paxeer-x/program_execution_context.py --build --build-dir '$(BUILD_DIR)' --cc '$(CC)'

paxeer-x-native-104.31.1: $(BUILD_DIR)/tests/programs_execution_context

$(BUILD_DIR)/tests/programs_execution_context: tests/programs/test_execution_context.c tests/programs/test_call_activity.c $(LIBRARY) $(CHECKPOINT_SETTLEMENT_HEADER)
	@mkdir -p $(@D)
	$(CC) $(CPPFLAGS) $(CFLAGS) $< -Wl,--start-group $(LIBRARY) $(PAXEER_CONTEXT_RUNTIME_LIB) -Wl,--end-group $(EXTRA_LDFLAGS) -lcrypto -pthread -ldl -lm -o $@
