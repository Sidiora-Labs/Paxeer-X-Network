.PHONY: paxeer-x-build-104.32.4 paxeer-x-native-104.32.4

paxeer-x-build-104.32.4:
	python3 tools/qualification/paxeer-x/program_fee_governance.py --build --build-dir '$(BUILD_DIR)' --cc '$(CC)'

paxeer-x-native-104.32.4: $(LIBRARY) $(CHECKPOINT_SETTLEMENT_HEADER)
	@mkdir -p '$(BUILD_DIR)/tests'
	$(CC) $(CPPFLAGS) $(CFLAGS) tests/programs/test_fee_governance.c -Wl,--start-group $(LIBRARY) '$(PAXEER_FEE_RUNTIME_LIB)' -Wl,--end-group $(EXTRA_LDFLAGS) $(PROGRAMS_NATIVE_LDLIBS) -lcrypto -pthread -ldl -lm -o '$(BUILD_DIR)/tests/programs_fee_governance_104_32_4'
