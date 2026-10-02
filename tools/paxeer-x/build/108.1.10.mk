PAXEER_X_CUSTODY_GOVERNANCE_PRODUCER := tools/qualification/paxeer-x/custody_governance.py

.PHONY: paxeer-x-build-108.1.10
paxeer-x-build-108.1.10:
	@test -n '$(PAXEER_X_CUSTODY_GOVERNANCE_ARTIFACTS)' || { echo 'set PAXEER_X_CUSTODY_GOVERNANCE_ARTIFACTS' >&2; exit 2; }
	python3 $(PAXEER_X_CUSTODY_GOVERNANCE_PRODUCER) --produce '$(PAXEER_X_CUSTODY_GOVERNANCE_ARTIFACTS)'
