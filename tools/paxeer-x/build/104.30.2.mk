# Scoped producer for task 104.30.2 (program-authorised transfer legs).
# Use: make -f Makefile -f tools/paxeer-x/build/104.30.2.mk paxeer-x-build-104.30.2
# Builds only the declared artifacts under $(BUILD_DIR) and $(CARGO_TARGET_DIR);
# never invokes programs-build or a workspace build.

ifeq ($(strip $(CARGO_TARGET_DIR)),)
$(error 104.30.2 producer requires CARGO_TARGET_DIR)
endif

PX_104_30_2_DIR := $(BUILD_DIR)/paxeer-x-104.30.2
PX_104_30_2_TARGET := $(abspath $(CARGO_TARGET_DIR))
PX_104_30_2_CARGO = env CARGO_TARGET_DIR=$(PX_104_30_2_TARGET) $(PROGRAMS_CARGO)
PX_104_30_2_SANDBOX_LIB := $(PX_104_30_2_TARGET)/debug/liblayerx_programs_sandbox.a
PX_104_30_2_MONETARY := $(PX_104_30_2_DIR)/programs_monetary_law
PX_104_30_2_RUST_EXES := $(PX_104_30_2_DIR)/rust-executables.json
PX_104_30_2_MANIFEST := $(PX_104_30_2_DIR)/artifacts.json
PX_104_30_2_CMD_LIB = $(PX_104_30_2_CARGO) build --locked --manifest-path programs/Cargo.toml -p layerx-programs-sandbox --lib --features layerx-programs-sandbox/host-ffi
PX_104_30_2_CMD_CC = $(CC) $(CPPFLAGS) -DLXP_TESTING $(CFLAGS) tests/programs/test_monetary_law.c $(TEST_LIBRARY) $(PX_104_30_2_SANDBOX_LIB) $(EXTRA_LDFLAGS) -lcrypto -pthread -ldl -lm -o $(PX_104_30_2_MONETARY)
PX_104_30_2_CMD_TEST = $(PX_104_30_2_CARGO) test --locked --manifest-path programs/Cargo.toml -p layerx-programs-runtime --lib --test monetary_law --no-run --message-format=json-render-diagnostics
PX_104_30_2_CMD_MAKE = make -f Makefile -f tools/paxeer-x/build/104.30.2.mk paxeer-x-build-104.30.2
PX_104_30_2_INPUTS := programs/crates/layerx-programs-runtime/src/transfer.rs \
	programs/crates/layerx-programs-runtime/tests/monetary_law.rs tests/programs/test_monetary_law.c \
	programs/Cargo.lock rust-toolchain.toml tools/paxeer-x/build/104.30.2.mk Makefile
PX_104_30_2_RUST_INPUTS := rust-toolchain.toml programs/Cargo.toml programs/Cargo.lock \
	$(shell find programs/crates programs/sdk/rust -type f \( -name '*.rs' -o -name Cargo.toml \) \
		-not -path '*/target/*' -print 2>/dev/null | LC_ALL=C sort)

PX_104_30_2_COMMANDS_JSON = ["$(strip $(PX_104_30_2_CMD_MAKE))", "$(strip $(PX_104_30_2_CMD_LIB))", "$(strip $(PX_104_30_2_CMD_CC))", "$(strip $(PX_104_30_2_CMD_TEST))"]

.PHONY: paxeer-x-build-104.30.2
paxeer-x-build-104.30.2: $(PX_104_30_2_MANIFEST)

$(PX_104_30_2_SANDBOX_LIB): $(PX_104_30_2_RUST_INPUTS)
	$(PX_104_30_2_CMD_LIB)

$(PX_104_30_2_MONETARY): tests/programs/test_monetary_law.c $(TEST_LIBRARY) $(PX_104_30_2_SANDBOX_LIB)
	@mkdir -p $(@D)
	$(PX_104_30_2_CMD_CC)

# Runtime lib unit tests (src/transfer.rs) and the monetary_law integration test,
# which prepares real Wasm guests at run time. --no-run compiles without executing.
$(PX_104_30_2_RUST_EXES): $(PX_104_30_2_RUST_INPUTS)
	@mkdir -p $(@D)
	$(PX_104_30_2_CMD_TEST) > '$(abspath $@).cargo'
	python3 -c 'import json,sys; \
	rows=[json.loads(l) for l in open(sys.argv[1]) if l.startswith("{")]; \
	exes=[{"target":r["target"]["name"],"kind":r["target"]["kind"],"executable":r["executable"],"features":r["features"]} \
	      for r in rows if r.get("reason")=="compiler-artifact" and r.get("executable") and "layerx-programs-runtime" in r["package_id"]]; \
	names=sorted({(e["target"],"test" if e["kind"]==["test"] else "lib") for e in exes}); \
	need={("layerx_programs_runtime","lib"),("monetary_law","test")}; \
	missing=[n for n in need if n not in names]; \
	sys.exit("missing rust executables: %r" % missing) if missing else None; \
	json.dump(exes,open(sys.argv[2],"w"),indent=2,sort_keys=True)' '$(abspath $@).cargo' '$(abspath $@)'
	rm -f '$(abspath $@).cargo'

$(PX_104_30_2_MANIFEST): $(PX_104_30_2_MONETARY) $(PX_104_30_2_SANDBOX_LIB) $(PX_104_30_2_RUST_EXES) $(PX_104_30_2_INPUTS)
	python3 -c 'import hashlib,json,os,subprocess,sys; \
	sha=lambda p: hashlib.sha256(open(p,"rb").read()).hexdigest(); \
	run=lambda *a: subprocess.run(a,capture_output=True,text=True,check=True).stdout.strip(); \
	out,native,lib,exes_file,cc=sys.argv[1:6]; cmds=json.loads(sys.argv[6]); inputs=sys.argv[7:]; \
	rust=json.load(open(exes_file)); \
	pick=lambda name,kind: [e["executable"] for e in rust if e["target"]==name and ("test" if e["kind"]==["test"] else "lib")==kind][-1]; \
	bins={"native_monetary_law":native,"runtime_unit":pick("layerx_programs_runtime","lib"),"monetary_law":pick("monetary_law","test"),"sandbox_staticlib":lib}; \
	m={"task":"104.30.2","source_revision":run("git","rev-parse","HEAD"), \
	   "source_dirty":bool(run("git","status","--porcelain")), \
	   "rustc":run("rustc","-vV").splitlines()[0],"cargo":run("cargo","-V"),"cc":run(cc,"--version").splitlines()[0], \
	   "env":{k:os.environ.get(k,"") for k in ("CARGO_INCREMENTAL","CARGO_PROFILE_DEV_DEBUG","CARGO_PROFILE_TEST_DEBUG")}, \
	   "features":["layerx-programs-sandbox/host-ffi"],"commands":cmds, \
	   "inputs":{p:sha(p) for p in inputs},"binaries":bins,"binary_sha256":{k:sha(v) for k,v in bins.items()}, \
	   "rust_targets":rust,"test_library":{"path":os.path.abspath("$(TEST_LIBRARY)"),"sha256":sha("$(TEST_LIBRARY)")}}; \
	json.dump(m,open(out+".tmp","w"),indent=2,sort_keys=True); os.replace(out+".tmp",out)' \
		'$(abspath $@)' '$(abspath $(PX_104_30_2_MONETARY))' '$(PX_104_30_2_SANDBOX_LIB)' \
		'$(abspath $(PX_104_30_2_RUST_EXES))' '$(CC)' \
		'$(subst ','\'',$(PX_104_30_2_COMMANDS_JSON))' $(PX_104_30_2_INPUTS)
	@echo "104.30.2 artifact manifest: $(abspath $@)"
