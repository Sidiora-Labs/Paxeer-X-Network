.PHONY: paxeer-x-build-104.31.6
paxeer-x-build-104.31.6:
	python3 tools/qualification/paxeer-x/program-abi-freeze.py build --output '$(PAXEER_X_ABI_FREEZE_ARTIFACTS)'
