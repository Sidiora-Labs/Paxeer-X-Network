PAXEER_X_ARBITER_PRESTATE_DIR ?= /root/lx-target/arbiter-prestate

.PHONY: paxeer-x-arbiter-prestate-build
paxeer-x-arbiter-prestate-build:
	python3 tools/qualification/paxeer-x/arbiter_prestate.py --build \
		--build-dir='$(PAXEER_X_ARBITER_PRESTATE_DIR)' --cc='$(CC)' \
		--cppflags='$(CPPFLAGS)' --cflags='$(CFLAGS)' --ldflags='$(EXTRA_LDFLAGS)' \
		--cargo='$(PROGRAMS_CARGO)'
