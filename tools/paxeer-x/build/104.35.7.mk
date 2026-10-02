PAXEER_X_ATTEST_ARTIFACT_DIR ?= $(CURDIR)/build/paxeer-x-104.35.7
PAXEER_X_ATTEST_DRIVER := tools/qualification/paxeer-x/program_market_attestation.py

.PHONY: paxeer-x-build-104.35.7 paxeer-x-attestation-begin
.NOTPARALLEL: paxeer-x-build-104.35.7

paxeer-x-attestation-begin:
	PAXEER_X_ATTEST_ARTIFACT_DIR='$(PAXEER_X_ATTEST_ARTIFACT_DIR)' python3 $(PAXEER_X_ATTEST_DRIVER) --begin-build

paxeer-x-build-104.35.7: paxeer-x-attestation-begin $(LIBRARY)
	PAXEER_X_ATTEST_ARTIFACT_DIR='$(PAXEER_X_ATTEST_ARTIFACT_DIR)' python3 $(PAXEER_X_ATTEST_DRIVER) --build \
		--cargo='$(PROGRAMS_CARGO)' --target-dir='$(PROGRAMS_TARGET_DIR)' \
		--cc='$(CC)' --cppflags='$(CPPFLAGS)' --cflags='$(CFLAGS)' \
		--ldflags='$(EXTRA_LDFLAGS)' --native-library='$(LIBRARY)' \
		--native-output='$(BUILD_DIR)/tests/programs_market_attestation'
