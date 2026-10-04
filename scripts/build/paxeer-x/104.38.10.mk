.PHONY: paxeer-x-build-104.38.10
paxeer-x-build-104.38.10:
	python3 scripts/qualification/paxeer-x/program-event-bounds.py build --output "$(PAXEER_X_EVENT_BOUNDS_ARTIFACTS)"
