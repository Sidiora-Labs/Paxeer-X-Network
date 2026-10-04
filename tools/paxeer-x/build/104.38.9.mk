.PHONY: paxeer-x-build-104.38.9
paxeer-x-build-104.38.9:
	python3 tools/qualification/paxeer-x/program-abi-policy.py build --output '$(PAXEER_X_ABI_POLICY_ARTIFACTS)'
