# LayerX Programs

**Deterministic guest execution for the LayerX kernel domain of Paxeer X Network, with no balance-writing authority.**

This workspace holds the programs surface of the LayerX kernel: a deterministic WASM
runtime, the registry that proves what a program deployed and what it holds, the C↔Rust
bridge into the kernel, reference programs, developer SDKs, porting kits, and the
adversarial test corpus.

Programs are a **first-class execution surface**, not one of the kernel's economic
modules. The kernel modules live in [`src/modules`](../src/modules) and are numbered in
[`include/layerx/lxp_module.h`](../include/layerx/lxp_module.h). Guest execution is
dispatched under the programs module (`LXP_MODULE_PROGRAMS = 9`), but a program is not a
module of its own and writes no balances. Every monetary effect a program produces
compiles to an authenticated `402LXP` transfer set applied by the kernel - the same
single money doorway every module uses.

Where the rest of the system fits:

- **Identity and authority are kernel**, not a program concern. A program executes inside
  the authority of the activity that invoked it and can never widen it.
- **`402LXP` is the sole balance writer.** Programs emit transfer sets; they do not call
  `set_balance` (no such function exists).
- **Oracle prices** enter through the Crossverse adapter
  ([`src/network/lx_oracle_adapter.c`](../src/network/lx_oracle_adapter.c)) as signed
  activities, outside execution. A state transition never dials out.
- **Settlement** happens on the Paxeer X chain (EVM chain ID `125`). LayerX orders and
  executes activity; periodic checkpoints settle on the chain, which lives in this same
  repository at the [repository root](..) with its own trust and build boundary (see
  [`docs/MONOREPO.md`](../docs/MONOREPO.md) and the root [`README.md`](../README.md)).

Normative behavior lives in [`spec/paxeer-x/spec.kvx`](../spec/paxeer-x/spec.kvx). This
document is the human read of the programs workspace and is not a substitute for the spec.

---

## Workspace layout

The programs workspace is a Cargo workspace (`programs/Cargo.toml`) with a strict lint
profile: `unsafe_code` is denied by default, Clippy `all` and `pedantic` are denied, and
`unwrap_used`, `expect_used`, and float arithmetic are denied across the tree.

| Path | Purpose |
| --- | --- |
| `crates/layerx-programs-runtime` | Deterministic WASM runtime: validation, metering, the ABI/capability boundary, cross-program calls, transfers, occupancy accounting, replay, and the FFI bridge into the C kernel |
| `crates/layerx-programs-registry` | Receipt-bound registry: deployment journal, program interfaces, program value-account bindings, real-balance proofs, wind-down/deprecation, and the reference-program interface generators |
| `crates/layerx-programs-protocol-adapter` | Thin C↔Rust adapter exposing receipt-verified program state reads to the rest of the kernel |
| `crates/layerx-programs-interpreter` | A bounded deterministic scripting program: scripts are submitted as canonical bytes and fully validated before any effect is staged |
| `crates/layerx-programs-market` | Compute-lease market program: usage claims with a challenge window, provider stake and slashing, attester policy, and on-chain bisection disputes |
| `crates/layerx-programs-arbiter` | Verified replay of sandbox traces and the market-step verdict that judges the single step a dispute bisection collapses to |
| `crates/layerx-programs-sandbox` | Protocol-state models for bounded, ephemeral program sandboxes |
| `sdk/rust`, `sdk/c`, `sdk/assemblyscript` | Guest program SDKs; Rust ships `escrow`, `naming`, `nft-lxt721`, `payments-merchant`, `swap-cpmm`, `token-lxt20`, `vault`, and `web-reader` examples, while C and AssemblyScript ship `paid-counter` |
| `porting/evm`, `porting/solana`, `porting/cosmwasm` | Migration crates and `MIGRATION.md` guides mapping Solidity / Anchor / CosmWasm vocabulary onto the programs ABI |
| `fuzz` | Structure-aware fuzz target and corpus for the runtime |
| `benches` | Interpreter benchmarks and their compiled ABI-v2 equivalents |
| `fixtures` | Committed reference-program artifacts, interfaces, registry values, and native activity/receipt fixtures |
| `../scripts/programs` | Dependency policy, immutable ABI-vector generation, and reference-fixture build scripts |
| `../tests/programs` | Native C tests, ABI-drift and runtime module-boundary checks |
| `tests` | Cross-implementation vectors, the hostile-program `gauntlet`, conservation and parallel-differential checks, and calldata fixtures |
| `vendor` | Vendored, pinned dependencies for a hermetic build |

Cargo promotes path dependencies below the workspace root into workspace members.
Consequently `--workspace` builds exercise the vendored `parity-wasm`, `wasm-instrument`,
and `wasmi` unit and documentation tests as well as every programs package. The vendored
crates also remain subject to `../scripts/programs/dependency-policy.sh`.

### The core crates

**`layerx-programs-runtime`** is the deterministic WASM foundation for guest programs.
It runs on a pinned `wasmi` interpreter with floating point and other nondeterminism
removed. The module map (see `src/lib.rs`) separates concerns deliberately:

- `validate.rs`, `limits.rs` - static module validation and structural bounds (module
  bytes, function count, stack height, call depth).
- `budget.rs`, `meter.rs` - caller-declared activity ceilings, the admitted budget token,
  and per-execution resource metering (CPU fuel, memory, storage read/write, output).
- `abi/` - the transaction boundary. `abi/capability.rs` owns capability grants, their
  canonical encoding, and downward-only narrowing; `abi/response.rs` owns response and
  refusal transport; `abi/storage_ops.rs` owns namespaced storage; `abi/codec.rs` owns
  the canonical calldata encoding.
- `accounts.rs` - deterministic derivation of program-owned accounts.
- `transfer.rs`, `ffi_transfer.rs` - the sole monetary exit: typed `402LXP` requests
  bound to invocation authority and submitted as one atomic set to the kernel primitive.
- `occupancy.rs` - deterministic, receipt-bound storage-occupancy accounting.
- `calls.rs`, `engine.rs`, `execute.rs`, `entrypoint.rs`, `lifecycle.rs` - cross-program
  calls, execution, and program lifecycle.
- `host/` - linker orchestration; each host-function family is registered in its own
  unit and reaches execution state only through `RuntimeState`.
- `ffi.rs`, `ffi_call.rs`, `ffi_interface.rs` - the FFI bridge the C programs module calls
  into.

**`layerx-programs-registry`** (`#![forbid(unsafe_code)]`) turns protocol receipts into
answers about programs: the `DeploymentJournal`, `ProgramValueAccountBinding` records, and
`VerifiedAccountSnapshot`/`ValueAccount` types that resolve a program's real balance from
account-tree Merkle proofs rather than a declared bookkeeping column. It also owns
deprecation and wind-down (`AuthorizedExit`, `ExitRoute`, `WindDownView`) and the
`lxt20`, `lxt721`, `naming`, and `swap` reference interfaces.

**`layerx-programs-protocol-adapter`** is the narrow read bridge: `read_program_state`
exposes receipt-verified program state (`ProtocolProgramStateRead`) to the C kernel and
the hosted read surfaces without granting any write authority.

---

## Kernel surfaces

The surfaces below are described as implemented; the authoritative sources are the code
and [`spec/paxeer-x/spec.kvx`](../spec/paxeer-x/spec.kvx).

### Program-owned accounts

A program can own accounts that no principal can claim. An account id is derived
deterministically from the program and a seed - a pure function of public inputs, with no
host state, clock, or entropy involved:

```
account_id = SHA-256(
    "LayerX/programs/program-account/v1\0"   // domain tag, distinct from principal ids
    || program_id                            // 32 bytes, bound before the seed
    || u32_be(seed_len)                      // length prefix removes concatenation ambiguity
    || seed                                  // up to 128 bytes
)
```

The construction is domain-separated: the tag is disjoint from the `LX:ACCOUNT:v1` domain
used for principal/named account ids, so a principal cannot present a public key whose
identifier collides with a derived program account without breaking SHA-256 preimage
resistance. The C kernel (`src/modules/programs/accounts.c`) and the Rust runtime
(`accounts.rs`) agree byte-for-byte, and frozen golden vectors pin the digests.

Deriving an account conveys **no authority**. Program value accounts are registered as
`LX_ACCOUNT_MODULE_VALUE` accounts that carry no authority key, so:

- the ordinary account-open path refuses the `module:programs:value:…` namespace;
- registration is gated on the program owner, not first-caller squatting;
- debits require program authority bound to the deriving program's own frame and exact
  seed (re-derived and checked at transfer time), never a principal signature.

A principal can **fund** a program account (an ordinary `402LXP` credit leg) but can never
**authorize debits** as if it owned the account.

### Downward-only spending grants

Spending authority narrows, never widens. The `ProgramSpend` capability
(`abi/capability.rs`) conveys a bounded grant over accounts the granting program itself
derives - distinct from the `Transfer402` grant over the invoking principal's balance. It
binds `owner_program`, `seed`, `source_account`, `asset`, `to`, and a `maximum_amount`.

Across a program-to-program call edge the grant may only be narrowed:

- any change to identity fields (`owner_program`, `seed`, `source_account`, `asset`, `to`)
  produces a different capability key, so the parent lookup fails and the edge is refused
  with the same typed capability-escalation error principal grants already use;
- an attempt to raise `maximum_amount` above the parent's is refused; a child amount must
  be less than or equal to the parent's;
- only the **owner program** may originate a fresh `ProgramSpend` over its own account on
  an edge; a callee cannot mint authority it was not given.

At transfer time the grant is checked cumulatively against the actual legs, and the
deriving program's frame is the only frame that may stage a program-account debit. There
is no silent widen and no partial transfer set survives a refused escalation.

### Occupancy settlement

State that persists is paid for as long as it persists. Occupancy is deterministic,
batch-indexed rent for persistent program storage - it meters **namespace bytes held
across protocol batches**, priced by the fee schedule. There is no wall-clock component:
"time" here is the protocol batch sequence.

For each occupied storage namespace over a batch interval:

```
byte_batches = recorded_bytes × (to_batch − from_batch)
accrued_fee  = byte_batches × occupancy_byte_batch_price
amount_due   = prior_arrears + accrued_fee
```

Before writing storage a call must establish a signed responsibility mandate that names
the payer and caps both the bytes and the lifetime charge; the occupancy fee budget is
carved out of the activity's signed fee limit, separate from the execution meter. Each
charge resolves to a disposition - `Paid`, `ChargeCeilingExceeded` (namespace frozen),
`ScheduleCeilingExceeded`, `InsufficientFunds`, or `MigrationRequired` (pre-upgrade legacy
bytes, frozen at price zero until the owner migrates). Settlement is charged to the
declared responsible account through ordinary `402LXP` transfer legs and bound into the
batch receipt as canonical, replay-checkable evidence. The principal-scoped namespace is
charged to its principal; a shared namespace is charged to the program owner.

### Protocol-backed program balances

A program's balance is real kernel state, not a registry counter. Registry value
accounts are the same `LX_ACCOUNT_MODULE_VALUE` accounts that live in the kernel account
tree; the registry reads a balance by verifying the account leaf through the account root,
the universal subtree root, and the receipt state root before surfacing it. Wind-down
refuses to strand value: a deprecation cannot complete while a bound account still carries
a non-zero balance without an authorized exit route.

The invariant that ties all of this together: **`402LXP` remains the sole balance
writer.** Programs emit transfer sets - they never set a balance directly. As the spec
puts it, *"No program ever receives balance-writing authority. Every monetary effect
a program produces is expressed as authenticated 402LXP transfers applied by the kernel
transfer primitive… a balance change outside a 402LXP transfer aborts the transition."*

---

## SDKs and porting kits

The runtime admits guest ABI versions 1 through 5, and the checksums of their frozen
vectors (`tests/vectors/abi-v1.hex` to `abi-v5.hex`) are pinned in `abi-frozen.sha256`.
The host imports of ABI 1 to 4 are listed in `sdk/abi-manifest.tsv`: ABI 2 adds scoped
storage, responses and refusals, program-account transfers, context, balance, hash,
signature and 256-bit integer host calls; ABI 3 adds `oracle_read`; ABI 4 adds
`web_read`. ABI 5 adds `market_step_adjudicate` in the `layerx_v5` import module. Guest
program SDKs include:

- **`sdk/rust`** (`layerx-program-sdk`) - the Rust guest SDK.
- **`sdk/c`** - a C guest SDK with headers, sources, a determinism lint, and a toolchain
  manifest.
- **`sdk/assemblyscript`** - an AssemblyScript SDK (`abi`, `capability`, `transfer`,
  `storage`, `event`, `call`, `receipt` bindings) with a determinism lint.

The Rust SDK ships `escrow`, `naming`, `nft-lxt721`, `payments-merchant`, `swap-cpmm`,
`token-lxt20`, `vault`, and `web-reader` examples; the C and AssemblyScript SDKs ship
`paid-counter` examples. ProgramSpend (tag 9) and BalanceView (tag 10) are admitted from
ABI 2 on; the runtime crate defines their canonical encoding and amount-monotone
narrowing rules. ABI 1 does not admit these grants.

`sdk/rust` also ships the `lxt20` and `lxt721` request codecs (`src/lxt20.rs`,
`src/lxt721.rs`) and program-account preparation and merchant-split helpers
(`src/payments.rs`). See
[`docs/wiki/PaymentsQuickstart.md`](../docs/wiki/PaymentsQuickstart.md)
and [`docs/wiki/Assets.md`](../docs/wiki/Assets.md).

LXT20 is ABI-v2 guest state backed by one native Asset. Its interface has
`initialize`, `transfer`, `approve`, `transfer_from`, `balance_of`,
`allowance`, `total_supply`, and `metadata`. Request bytes are
`[0x4c,0x58,0x14,method] || [1,0x20] || payload_len:u32_be || payload`.
Recipients approve once, including zero, to register their derived account
storage. The reference program has fixed supply and no mint, burn, permit, or
nested-call surface. See
[`sdk/rust/examples/token-lxt20`](sdk/rust/examples/token-lxt20/README.md).

The porting kits map familiar contract vocabularies onto the programs ABI and are explicit
about what does not carry over:

- **`porting/evm`** - porting a Solidity contract.
- **`porting/solana`** - porting a Solana / Anchor program.
- **`porting/cosmwasm`** - porting a CosmWasm contract.

Each has a `MIGRATION.md` written for a developer who already knows the source chain.

---

## Fuzzing, tools, and tests

- **`fuzz/`** - a structure-aware fuzz target (`src/main.rs`) with `validation`,
  `instantiation`, and `execution` corpora.
- **`../scripts/programs/dependency-policy.sh`** - enforces the vendored-dependency policy (`deny.toml`).
- **`../tests/programs/runtime-module-boundaries.sh`** - enforces the runtime's module-boundary rules.
- **`tests/gauntlet/`** - the hostile-program gauntlet (cross-program derivation, callee
  spend attempts, escalation across depth/fan-out/repeated visits) with an
  `attack-inventory.tsv`.
- **`tests/vectors/`** - cross-implementation ABI vectors and
  [calldata fixtures](tests/vectors/calldata/README.md) that keep the C and Rust surfaces
  byte-identical.

---

## Building and testing

The programs workspace builds with the pinned Rust toolchain declared in
`programs/Cargo.toml` (`rust-version = 1.91.1`) against vendored dependencies.
`programs/.cargo/config.toml` points Cargo at `vendor/`, keeps it offline, and sets the
`wasm32-unknown-unknown` code-generation flags; Cargo reads it only when it runs from
inside `programs/`. From the repository root:

```sh
make programs-build                # cargo build --workspace
make programs-lint                 # module boundaries, clippy, dependency policy, cargo deny
make programs-abi-drift            # ABI manifest drift and linker check
make programs-test                 # the full programs test set
make programs-fuzz-smoke           # replay the fuzz corpora
make programs-reference-fixtures   # rebuild the committed reference-program fixtures
```

Runtime and kernel share golden vectors so the C and Rust implementations stay in
lockstep. Deterministic cross-architecture replay and fault injection are described in
[`docs/QUALIFICATION.md`](../docs/QUALIFICATION.md).

## Status

The limited beta has not opened yet. The gateway API becomes available when it does. This
is a mainnet beta on real value, so there is no faucet for general use; approved developers
receive test allocations from the team. Source is licensed under the Apache License,
Version 2.0. Hosted documentation is at [docs.paxeer.app](https://docs.paxeer.app/).

A successful local build is development evidence, not authorization to deploy, move
custody, or handle real assets.

---

Paxeer X Network is developed by [Sidiora Labs](https://github.com/Sidiora-Labs/Paxeer-X-Network).
