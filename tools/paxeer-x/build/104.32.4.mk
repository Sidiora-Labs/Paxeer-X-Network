PAXEER_X_104_32_4_OUT := $(abspath $(BUILD_DIR))/paxeer-x-104.32.4
PAXEER_X_104_32_4_SANDBOX_LIB := $(CARGO_TARGET_DIR)/debug/liblayerx_programs_sandbox.a
PAXEER_X_104_32_4_FEATURES := layerx-programs-sandbox/host-ffi
PAXEER_X_104_32_4_SOURCES := \
	programs/crates/layerx-programs-runtime/src/meter.rs \
	src/modules/programs/fees.c \
	tools/paxeer-x/gates/104.32.4.sh \
	tools/qualification/paxeer-x/program_fee_governance.py \
	tools/paxeer-x/build/104.32.4.mk \
	tests/programs/test_fee_governance.c \
	programs/crates/layerx-programs-runtime/tests/fee_governance.rs \
	include/layerx/programs.h \
	Makefile \
	programs/Cargo.lock \
	rust-toolchain.toml

PAXEER_X_104_32_4_CARGO_LIB = cd programs && env CARGO_TARGET_DIR=$(CARGO_TARGET_DIR) $(PROGRAMS_CARGO) build --locked -p layerx-programs-sandbox --lib --features $(PAXEER_X_104_32_4_FEATURES)
PAXEER_X_104_32_4_NATIVE = $(CC) $(CPPFLAGS) $(CFLAGS) tests/programs/test_fee_governance.c -Wl,--start-group $(LIBRARY) $(PAXEER_X_104_32_4_SANDBOX_LIB) -Wl,--end-group $(EXTRA_LDFLAGS) -lcrypto -pthread -ldl -lm -o $(PAXEER_X_104_32_4_OUT)/programs_fee_governance
PAXEER_X_104_32_4_CARGO_TESTS = cd programs && env CARGO_TARGET_DIR=$(CARGO_TARGET_DIR) $(PROGRAMS_CARGO) test --locked -p layerx-programs-runtime --test fee_governance --test replay --no-run --message-format=json

.PHONY: paxeer-x-build-104.32.4

paxeer-x-build-104.32.4: $(LIBRARY) $(CHECKPOINT_SETTLEMENT_HEADER)
	@case "$(CARGO_TARGET_DIR)" in /*) ;; *) echo "paxeer-x-build-104.32.4: CARGO_TARGET_DIR must be an absolute path" >&2; exit 2;; esac
	rm -rf $(PAXEER_X_104_32_4_OUT)
	mkdir -p $(PAXEER_X_104_32_4_OUT)
	printf '%s\n' "$(PAXEER_X_104_32_4_CARGO_LIB)" "$(PAXEER_X_104_32_4_NATIVE)" "$(PAXEER_X_104_32_4_CARGO_TESTS)" > $(PAXEER_X_104_32_4_OUT)/commands.txt
	$(PAXEER_X_104_32_4_CARGO_LIB)
	test -f $(PAXEER_X_104_32_4_SANDBOX_LIB)
	$(PAXEER_X_104_32_4_NATIVE)
	$(PAXEER_X_104_32_4_CARGO_TESTS) > $(PAXEER_X_104_32_4_OUT)/cargo-test.jsonl
	python3 -c 'import json,shutil,sys; a=[json.loads(l) for l in open(sys.argv[1]) if l.startswith("{")]; e={m["target"]["name"]: m["executable"] for m in a if m.get("reason") == "compiler-artifact" and m.get("executable") and m["profile"]["test"] and m["target"]["kind"] == ["test"] and "layerx-programs-runtime" in m["package_id"]}; [shutil.copy2(e[n], sys.argv[2] + "/rust_" + n) for n in ("fee_governance", "replay")]' $(PAXEER_X_104_32_4_OUT)/cargo-test.jsonl $(PAXEER_X_104_32_4_OUT)
	python3 -c 'import hashlib,json,os,shlex,subprocess,sys; o=sys.argv[1]; r=lambda c, d=".": subprocess.run(c, cwd=d, check=True, capture_output=True, text=True).stdout; h=lambda p: hashlib.sha256(open(p, "rb").read()).hexdigest(); b=(("native_fee_governance", "programs_fee_governance"), ("rust_fee_governance", "rust_fee_governance"), ("rust_replay", "rust_replay")); m={"revision": r(["git", "rev-parse", "HEAD"]).strip(), "sources": {p: h(p) for p in sys.argv[4:]}, "toolchain": {"rustc": r(["rustc", "-V"], "programs").strip(), "cargo": r(["cargo", "-V"], "programs").strip(), "cc": r(shlex.split(sys.argv[2]) + ["--version"]).splitlines()[0]}, "features": [sys.argv[3]], "env": {k: os.environ.get(k) for k in ("CARGO_INCREMENTAL", "CARGO_PROFILE_DEV_DEBUG", "CARGO_PROFILE_TEST_DEBUG")}, "commands": open(o + "/commands.txt").read().splitlines(), "binaries": {k: {"path": os.path.abspath(o + "/" + f), "sha256": h(o + "/" + f)} for k, f in b}}; t=o + "/artifacts.json.tmp"; f=open(t, "w"); json.dump(m, f, indent=2, sort_keys=True); f.write("\n"); f.close(); os.replace(t, o + "/artifacts.json")' $(PAXEER_X_104_32_4_OUT) "$(CC)" $(PAXEER_X_104_32_4_FEATURES) $(PAXEER_X_104_32_4_SOURCES)
