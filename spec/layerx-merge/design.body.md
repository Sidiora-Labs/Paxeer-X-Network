## One execution domain

After activation the Paxeer X Network runs one execution domain: paxd, with the Cosmos SDK modules, the EVM, CosmWasm and optimistic concurrency control (OCC) in one state machine. The LayerX kernel's ledger, payments, authority, programs and trading functions stop being a second ledger beside the chain. They become chain modules and precompiles next to the existing bank, addr, tokenfactory, evm, feetoken, layerxexchange, layerxbridge, launchpad, xweb and layerxgov constructs. Users sign ordinary EVM transactions. Each one is executed by the proposer at receipt and committed by the validator set.

### Kernel modules hosted inside paxd (D1)

The kernel C modules (ledger and assets, escrow, budget, stream and service payments, perps and spot, programs) and the Rust program runtime are hosted through cgo inside the keepers. They are not ported.

- Every kernel C module already executes through an `lxp_module_ctx` execute function, for example `programs_execute` (src/modules/programs/registration.c:164) and `execute_deploy` (src/modules/programs/deploy.c:485). The context type is declared at include/layerx/lxp_module.h:31 and defined at include/layerx/lxp_kernel.h:162.
- The node already links C through cgo for CosmWasm (wasm-runtime/version_cgo.go:1, wasm-runtime/internal/api/link_system.go:5-6). The kernel host follows the same pattern.
- Each hosting keeper owns a native/ package that links the C module. That package implements `lxp_module_ctx` over the module's `sdk.KVStore`, with an effect buffer and a transfer set. Kernel effects become store writes and bank moves inside the same transaction. The shim's receipt record is the sequencer receipt (see the proposer section).
- Parity is byte-exact. The existing C unit tests, the Rust runtime tests and the kernel replay goldens run through the shim unchanged.
- A determinism gate comes before any hosting task lands. It passes when all of the following hold:
  - the same block replayed through the shim on every validator build gives an identical app hash and identical per-transaction results;
  - OCC execution with forced aborts and re-execution gives the same state as strictly sequential execution;
  - no kernel in-memory state survives outside the committed KV store between transactions, or that state is proved reset per transaction;
  - the existing C tests, Rust runtime tests and replay goldens pass byte-exact;
  - no execute path reachable from a keeper depends on wall-clock time, allocator addresses, threads or floating point.

### OCC

Validators keep the OCC block executor unchanged:

- OCC is on by default (sdk/server/config/config.go:29).
- Workers are clamped to 2 × NumCPU within [10, 128] (sdk/server/config/config.go:41-48).
- The scheduler is built at node/abci.go:247 and driven by `ProcessTXsWithOCCV2` (node/app.go:1888) under `ExecuteTxsConcurrently` (node/app.go:1861).

Hosted kernel modules run inside that executor, so their store access must stay key-scoped. The launchpad protocol-fee total moves to a per-market counter so swaps on different markets no longer share a key. The exchange's global intent count leaves the call path with the intent store (see the native constructs section).

### CosmWasm

CosmWasm stays what it is today:

- native CosmWasm contracts in wasm/x/wasm;
- the message bridge in node/wasmbinding (node/wasmbinding/message_plugin.go:19);
- the wasmd precompile at 0x1002.

It is not the program host. Kernel programs import the `layerx_v1` to `layerx_v5` host modules (programs/crates/layerx-programs-runtime/src/abi/manifest.rs:8-12), not the CosmWasm environment. They run under layerxprograms at 0x101D.

### EVM

The EVM is the user entry point for every write.

- The ante handler authorises the user's own signature. Deliver mode requires the exact next nonce (modules/evm/ante/sig.go:75).
- Kernel operations are precompile calls inside ordinary EVM transactions. Precompiles are served from `GetCustomPrecompiles` (precompiles/setup.go:63-97).
- Precompile failures revert. Revert data is `Error(string)` when a precompile opts into revert reasons (precompiles/common/precompiles.go:53, :82-92).
- Every new precompile returns `Error("LXP_<code>: <message>")` (D6). bank 0x1001 and addr 0x1004 return the same text through an opt-in reason path in the shared dynamic-gas precompile base.
- One shared table maps kernel result codes to `LXP_` strings. Errors raised by the xweb Go keeper use `LXP_XWEB_*` codes. Errors raised inside a kernel module keep their kernel code.

## Native constructs

The table lists every precompile address the chain serves after activation. "Existing" means the address keeps its current behaviour; when the activation adds the `LXP_` revert text, the Role column says so. "Extended" means the activation adds or changes methods. "New" means the address is served from the activation height. "Retired at activation" means the address stays registered, but each method returns `Error("LXP_MODULE_DISABLED: <precompile> retired")` from the activation height. An unregistered address would return empty success instead.

| Address | Name | Role | Owning module path | Kind of change | Registration (path:line) |
|---|---|---|---|---|---|
| 0x1001 | bank | Native balances. Holds every former kernel balance, including derived sub-account addresses. Gains the `send(address,address,string,uint256,bytes32)` overload carrying a LayerX context hash and its `LayerXReceipt` log. Revert text with the `LXP_` prefix. | sdk/x/bank | extended | precompiles/setup.go:68; address precompiles/bank/bank.go:33 |
| 0x1002 | wasmd | CosmWasm execute and query | wasm/x/wasm, node/wasmbinding | existing | precompiles/setup.go:69; address precompiles/wasmd/wasmd.go:30 |
| 0x1003 | json | JSON utility | precompiles/json | existing | precompiles/setup.go:70; address precompiles/json/json.go:28 |
| 0x1004 | addr | Pax and EVM address association, and the DID binding used by every unified-account flow and by principal resolution on 0x101A-0x101D and 0x0901-0x0908. Revert text with the `LXP_` prefix. | modules/evm (keeper modules/evm/keeper/layerx_binding.go:18, :30) | extended | precompiles/setup.go:71; address precompiles/addr/addr.go:57 |
| 0x1005 | staking | Validator staking | sdk/x/staking | existing | precompiles/setup.go:72; address precompiles/staking/staking.go:49 |
| 0x1006 | gov | Governance | sdk/x/gov | existing | precompiles/setup.go:73; address precompiles/gov/gov.go:30 |
| 0x1007 | distribution | Rewards distribution | sdk/x/distribution | existing | precompiles/setup.go:74; address precompiles/distribution/distribution.go:37 |
| 0x1008 | oracle | Validator price oracle. Not the perps price source. | modules/oracle | existing | precompiles/setup.go:75; address precompiles/oracle/oracle.go:26 |
| 0x1009 | ibc | IBC transfer | precompiles/ibc | existing | precompiles/setup.go:76; address precompiles/ibc/ibc.go:30 |
| 0x100A | pointerview | ERC-20 and pointer views | precompiles/pointerview | existing | precompiles/setup.go:78; address precompiles/pointerview/pointerview.go:24 |
| 0x100B | pointer | Pointer registration | precompiles/pointer | existing | precompiles/setup.go:77; address precompiles/pointer/pointer.go:29 |
| 0x100C | solo | Solo utility | precompiles/solo | existing | precompiles/setup.go:80; address precompiles/solo/solo.go:32 |
| 0x1011 | p256Verify | P-256 signature verification | precompiles/p256 | existing | precompiles/setup.go:79; address precompiles/p256/p256.go:24 |
| 0x1012 | layerxVerify | Former kernel receipt and state-proof verifier. Replaced by abci_query prove=true, eth_getProof and receipt inclusion. | none | retired at activation | precompiles/setup.go:81; address precompiles/layerxverify/layerxverify.go:52-53 |
| 0x1013 | layerxCustody | Former deposit, withdrawal, forced-exit and deposit-root surface (all twenty methods). Funding a sub-account becomes a bank or layerxledger move. | modules/layerxcustody | retired at activation | precompiles/setup.go:82; address modules/layerxcustody/types/ids.go:17, precompiles/layerxcustody/layerxcustody.go:83-84 |
| 0x1014 | layerxAnchor | Former checkpoint and guarantor surface. The twelve write methods retire at activation. The nine read methods stay served until every posted guarantor bond and open challenge deposit has been returned; then the store is deleted and the reads retire too. Finality is block height and commit. | modules/layerxanchor | retired at activation | precompiles/setup.go:83; address precompiles/layerxanchor/layerxanchor.go:65-66 |
| 0x1015 | layerxExchange | Native perps and spot engine (hosted kernel engine):<br>• margin deposit becomes an in-transaction bank transfer, replacing the custody hop at modules/layerxexchange/keeper/intents.go:63<br>• `withdrawMargin` is a bank release behind a free-margin check<br>• new methods: market creation, oracle push, positions, funding, liquidation, ADL, spot book<br>• `requestSettlement` and `getIntent` retire<br>• `intentNonce` stays as the nonce input of chain-derived order ids<br>• per-module `Paused` param | modules/layerxexchange | extended | precompiles/setup.go:84; address modules/layerxexchange/types/ids.go:15, precompiles/layerxexchange/layerxexchange.go:71-72 |
| 0x1016 | layerxBridge | Inbound and outbound bridge with attestor threshold (keeper modules/layerxbridge/keeper/bridge.go:65). Revert text with the `LXP_` prefix. | modules/layerxbridge | existing | precompiles/setup.go:85; address modules/layerxbridge/types/keys.go:27, precompiles/layerxbridge/layerxbridge.go:54-55 |
| 0x1017 | launchpad | Bonding-curve markets. The protocol cut moves to a per-market counter read by the new `getProtocolFees(address)`, and the `getConfig` tuple drops `protocolFeesPending`. | modules/launchpad | extended | precompiles/setup.go:86; address modules/launchpad/types/keys.go:26, precompiles/launchpad/launchpad.go:84-85 |
| 0x1018 | feeToken | Per-account fee denomination: `setFeeDenom`, `getFeeDenom`, `clearFeeDenom`. SID is paid as gas through it. Revert text with the `LXP_` prefix. | modules/evm (fee-token keeper) | existing | precompiles/setup.go:88-92; address precompiles/feetoken/feetoken.go:22-23 |
| 0x1019 | xweb | Paid web requests. Adds `fulfilProgram(bytes)`, `getProgramResult(bytes32,uint64)` and `getKernelNetworkId()` for program-origin requests. One attestor set serves both origins: the existing registry (modules/xweb/types/keys.go:21) admits at most 16 for program-origin requests (include/layerx/lx_web.h:29). `refund` serves EVM-origin requests only. | modules/xweb | extended | precompiles/setup.go:93-95; address precompiles/xweb/xweb.go:58-59 |
| 0x101A | layerxledger | Kernel asset ledger and payment grants:<br>• `register`, `pause`, `unpause`, `openAccount`, `send`, `mint`, `burn`<br>• `issueGrant`, `revokeGrant`, `draw`, `payExact`<br>• one `draw` per paid web request, emitting `PaymentDrawn`<br>• `payExact` is a typed send whose payment reference is its idempotency key, emitting `PaymentExact`<br>The `ILxPay` interface is this precompile's grant and draw subset. | modules/layerxledger (new) | new | entry added to precompiles/setup.go:63-97 |
| 0x101B | layerxpay | Escrow, budget, stream and service payments. Service disputes go to the linked escrow's arbiter, which settles the escrow on the ruling. Its stream keeper holds the only stream state. | modules/layerxpay (new) | new | entry added to precompiles/setup.go:63-97 |
| 0x101C | layerxauth | Session and capability grants on authz `LayerXScopeAuthorization`, the shared idempotency index and `resultByIdempotency`. | modules/layerxauth (new) | new | entry added to precompiles/setup.go:63-97 |
| 0x101D | layerxprograms | Kernel program host (hosted C programs module plus Rust runtime): deploy, upgrade, call, simulate, registry, wind-down, sandbox, fee and metering schedules, program-origin web requests | modules/layerxprograms (new) | new | entry added to precompiles/setup.go:63-97 |
| 0x0901 | orob resolver | Pure offset resolution | precompiles/layerxagent/orob (new) | new | entry added to precompiles/setup.go:63-97; route human/wallet/gateway/src/precompiles.ts:18 |
| 0x0902 | batch clearing | Pure clearing computation | precompiles/layerxagent/clearing (new) | new | entry added to precompiles/setup.go:63-97; route human/wallet/gateway/src/precompiles.ts:19 |
| 0x0903 | oracle aggregator | Aggregation. Price submission writes and reads the 0x1015 oracle store, not 0x1008. | precompiles/layerxagent/oracle (new) | new | entry added to precompiles/setup.go:63-97; route human/wallet/gateway/src/precompiles.ts:20 |
| 0x0904 | pofq scorer | Pure fill-quality scoring | precompiles/layerxagent/pofq (new) | new | entry added to precompiles/setup.go:63-97; route human/wallet/gateway/src/precompiles.ts:21 |
| 0x0905 | scheduler | Job store run in receipt order | precompiles/layerxagent/scheduler (new) | new | entry added to precompiles/setup.go:63-97; route human/wallet/gateway/src/precompiles.ts:22 |
| 0x0906 | payment streams | Agent stream methods. Keeps no store and executes through the 0x101B stream keeper. | precompiles/layerxagent/streams (new) | new | entry added to precompiles/setup.go:63-97; route human/wallet/gateway/src/precompiles.ts:23 |
| 0x0907 | tee attestor | Attestation verification against governance-set roots. Signer rules use the embedded-wallet attestor set. | precompiles/layerxagent/teeattestor (new) | new | entry added to precompiles/setup.go:63-97; route human/wallet/gateway/src/precompiles.ts:24 |
| 0x0908 | eip712 helper | EIP-712 hashing and typed-signer recovery | precompiles/layerxagent/eip712 (new) | new | entry added to precompiles/setup.go:63-97; route human/wallet/gateway/src/precompiles.ts:25 |

Addresses 0x100D to 0x1010 are not assigned. The four addresses 0x0901 to 0x0904 have no execution function in this repository. They are specified anew from the original paxspot design of the earlier implementation, and nothing is copied from it. 0x0905 to 0x0908 are real precompiles behind the gateway routes that already target them.

The non-precompile constructs that carry kernel state are:

- tokenfactory (modules/tokenfactory): denom registry, pause and issuer;
- layerxgov (modules/layerxgov): the kernel fee table, the programs fee and the per-module `Paused` params;
- authz (sdk/x/authz): the base of `LayerXScopeAuthorization`;
- the new modules/sequencer: the on-chain order commitment;
- BatchCallAndSponsor (contracts/src/BatchCallAndSponsor.sol:16): the EIP-7702 delegate and paymaster.

## The proposer is the executor

One node runs paxd in sequencer mode, and a second validator follows it as hot standby. The sequencer keeps three layers of `CacheMultiStore` over the committed store:

- L_c: committed state;
- L_N: the sealed block N waiting for commit;
- L_open: the open block N+1.

Every received transaction executes at receipt on a per-transaction branch of L_open. The sequencer returns a signed receipt, serves reads from L_open, and proposes blocks in exactly that order. The other validators re-execute each block with OCC and commit it.

### The eight properties

| # | Property | Code point (path:line) | What the activation adds |
|---|---|---|---|
| 1 | Execute at receipt on a speculative branch, in receipt order | `DeliverTx` node/abci.go:169 in deliver mode (nonce must equal next, modules/evm/ante/sig.go:75); BeginBlock vote reads node/legacyabci/begin_block.go:60-61; ante `LatestCtxGetter` node/app.go:1122-1124 | node/sequencer `Accept`: decode, per-transaction branch of L_open, deliver-mode ante, write on success. `LatestCtxGetter` returns the L_open context in sequencer mode. A per-sender future queue holds gapped nonces until they are executable. The sequencer's `FinalizeBlocker` (node/app.go:1551) adopts L_N when the hash matches. |
| 2 | Signed receipt returned immediately | `SendRawTransaction` rpc/send.go:76 returns only a hash after broadcast (rpc/send.go:139-159); receipt construction read by rpc/tx.go:106 | `eth_sendRawTransactionSync` and `pax_sequencerReceipt` (receipt shape below) |
| 3 | Reads from pending state | `RPCContextProvider` node/app.go:2895; pending mapped to latest at rpc/watermark_manager.go:196 and rpc/utils.go:66; pending nonce rpc/tx.go:370; gas estimate against latest rpc/send.go:171 | A pending context height that resolves to a read-only snapshot of L_open; pending nonce from the sequencer's next-nonce map; a log stream fed by `Accept`; estimates against pending |
| 4 | Blocks contain exactly the executed sequence | `CreateProposalBlock` consensus/internal/state/execution.go:101 with mempool reap at :126; header time consensus/internal/state/state.go:279; `checkTotalBlockGas` node/app.go:1477; block limits consensus/types/params.go:135-143; upgrade stop node/app.go:1500 | In sequencer mode, `Seal()` replaces the mempool reap. Header time is the time L_open opened (D7), and `proposalIsTimely` (consensus/internal/consensus/state.go:1347) accepts it. Seal closes within the block gas limits. Speculation stops at an upgrade height. |
| 5 | Pinned proposer; the others execute and commit | `Leader()` consensus/internal/consensus/types/round_state.go:422; `ProcessBlock` node/app.go:2091 | Leader list rule: round r is proposed by list[r mod len], with the primary at round 0 and the standby at round 1. The validator set and OCC replay are unchanged. |
| 6 | A block that deviates from sequencer order is refused | `ProcessProposalHandler` node/app.go:1457, before `checkTotalBlockGas` at :1477; base `ProcessProposal` sdk/baseapp/abci.go:968; height-gate pattern modules/layerxcustody/keeper/keeper.go:42 | The proposer address must be in the leader list. The first transaction must be `MsgSequenceBatch{fromSeq, toSeq, prevSeqHash, txRoot, sig}` of modules/sequencer, with continuity against the stored `lastSeq` and `lastSeqHash` and `txRoot` equal to the hash of the remaining transactions. |
| 7 | WAL and hot standby | Consensus WAL autofile group consensus/internal/consensus/wal.go; per-node signing state consensus/privval/file.go | node/sequencer WAL (see below) |
| 8 | Fee policy | modules/evm/ante/fee.go:53-56; modules/evm/types/params.go:39, :52; daemon/paxd/cmd/root.go:397 | See the fee policy section |

### Receipt shape and key

- `eth_sendRawTransactionSync` returns the full Ethereum receipt.
- `pax_sequencerReceipt` returns `{txHash, seq, height, index, status, gasUsed, cumulativeGasUsed, logs, revertReason, effectiveGasPrice, feeDenom, prevSeqHash}`. `blockHash` is omitted because the header of N+1 depends on the commit of N.
- The signed receipt is a separate method. `eth_getTransactionReceipt` keeps its standard meaning: it answers once the block is committed, with the same fields plus `blockHash`.
- The signature is ed25519, made with a receipt key that is separate from the validator key (D15). The receipt authority and the settle endpoint pin the key, and modules/sequencer holds it as a governance-set param, so the key rotates without touching validator keys.
- Verifiers reuse `verify_sequencer_signature` (agent/crates/layerx-proof/src/receipt.rs:631).
- Clients read three status rungs: submitted, receipted and committed (D17).

### Pending reads

Pending reads come from a read-only snapshot of L_open. A read never holds the L_open write lock. Exchange order-book views are `eth_call` into 0x1015, so they follow the pending context with no module change. Full nodes have no pending state. The unified router sends `pending` reads to the sequencer and returns an explicit error instead of silently serving latest.

### Order check

ProcessProposal on every validator checks two things:

1. The proposer is in the leader list. This check needs no app state.
2. The leading `MsgSequenceBatch` continues the stored sequence and commits to the remaining transactions. This check lives in app state behind the activation height.

A receipt holder can prove a deviation from `seq` and `prevSeqHash` against the on-chain `lastSeqHash`. A refused block costs one round and raises an alert; it is not retried.

### WAL and standby

The sequencer appends `(seq, txBytes, receiptHash)` to node/sequencer/wal before it releases a receipt. Entries are written with group-commit fdatasync on the autofile group that consensus/internal/consensus/wal.go already uses.

The standby is the second entry in the leader list.

- It receives each WAL entry synchronously and acknowledges it before the primary releases the receipt.
- It replays every entry through the same `Accept` path, so its receipts match the primary's byte for byte.
- If the primary stops, the standby proposes the next WAL slice at round 1.
- Each node keeps its own key and last-sign state (consensus/privval/file.go).

Without a standby acknowledgement, receipts stop and sends return an error (fail closed). Round-based leadership together with `prevSeqHash` continuity refuses a second primary's block.

### Execution mode (D19)

The strictly sequential executor lands first. The target is OCC micro-batches executed in receipt order on the existing OCC engine (node/app.go:1888). The switch is made on measured throughput and receipt latency.

### Activation of the fork (D18)

The consensus scope is one fork at one activation height H_act. At H_act:

1. **BeginBlock votes.** BeginBlock(N+1) is fed the votes of N-1. Today distribution and slashing read the LastCommit votes of N (node/legacyabci/begin_block.go:60-61), and N+1 is already executing while N is in consensus. With this change, receipt latency no longer depends on consensus.
2. **Sequence commitment.** modules/sequencer is mounted with `MsgSequenceBatch`, `lastSeq`, `lastSeqHash` and the receipt key param, and the ProcessProposal check above becomes active.
3. **Leader list.** The `Leader()` list rule (consensus/internal/consensus/types/round_state.go:422) becomes active. H_act and the leader list live in node config and must be identical on every validator. A node whose list or H_act differs refuses to start, so a mixed rollout cannot split prevotes.

The merge upgrade plan's height equals H_act.

## Fee policy

Fees are policy. Execution runs no fee logic beyond the existing ante path.

| Item | Code today (path:line) | Target |
|---|---|---|
| EVM minimum gas price | `DefaultMinFeePerGas` 1 gwei modules/evm/types/params.go:52; base-fee and minimum-fee checks modules/evm/ante/fee.go:53-56; dynamic base fee modules/evm/keeper/fee.go:100 | A near-zero `MinimumFeePerGas`, set by governance param change. The base fee is fixed per open block when L_open opens, so receipts inside one block quote one base fee. |
| Node floor | `minimum-gas-prices` "0.01uhpx" daemon/paxd/cmd/root.go:397, checked only in CheckTx sdk/x/auth/ante/validator_tx_fee.go:33 | The same near-zero floor, applied by the sequencer before `Accept`, because `Accept` runs the deliver-mode ante |
| Draw sender class | Not present today | Minimum gas price of at most 0.067 gwei for the paid-search `draw` sender class (D10). One draw per request gives one receipt at USD 0.001. |
| SID as second gas token, self-paid | feetoken 0x1018 `setFeeDenom`; `GetFeeTokenCharge` modules/evm/keeper/feetoken.go:92, called at modules/evm/ante/fee.go:74, conversion at :96; rate entries in `AllowedFeeDenoms`, changed by `ParameterChangeProposal` (modules/evm/gov.go:26-41); fee-token routing node/app.go:2165 | SID pays gas at the governance-set rate, initially 3.114 SID per PAX |
| Rate age | Age is counted in blocks (modules/evm/keeper/params.go:318-336), default `DefaultMaxFeeTokenRateAge` 1000 (modules/evm/types/params.go:39), read by `GetMaxFeeTokenRateAge` (modules/evm/keeper/params.go:310) | `MaxFeeTokenRateAge` is raised by governance so a governance-paced rate update stays fresh under the sequencer cadence (D7). The value is set at activation rehearsal. Rate age stays block-based, with no fork. |
| Sponsorship | `executeSponsored` contracts/src/BatchCallAndSponsor.sol:135; gas station requires the 7702 delegate to equal the configured paymaster (interop/crates/layerx-gas-station/src/tx.rs:314) | Every sponsored agent write is a 7702 `executeSponsored` batch through the gas station with one paymaster nonce (D14). The station also sponsors feeder, perps keeper, attestor, worker and onboarding transactions. There is no ante change and no fork. |
| Sponsored SID rate | `setRate` contracts/src/BatchCallAndSponsor.sol:88; publisher call interop/crates/layerx-gas-station/src/rate.rs:440; `max_rate_age` bound interop/crates/layerx-gas-station/src/config.rs:147-148 | The rate publisher refreshes the paymaster rate. The station refuses quotes older than `max_rate_age`. Self-paid SID through 0x1018 does not depend on it. |
| Per-identity budgets | Per-quote, per-account and per-interval limits keyed by EVM address (interop/crates/layerx-gas-station/src/policy.rs:98-107); balance floor :115 | Budgets keyed by DID, resolved through `getLayerXDid` on addr 0x1004 (precompiles/addr/addr.go:391). Unbound accounts are keyed per address. Counters are reconciled from the station journal. The station fails closed when it is unavailable. x/feegrant stays on the Cosmos fee path only (node/ante.go:80). |

## Authority model

### DID binding through 0x1004

A LayerX identity is a `did:layerx` ed25519 key bound to an EVM address on addr 0x1004:

- write methods: `bindLayerX`, `unbindLayerX` (precompiles/addr/addr.go:38-39);
- read methods: `getLayerXDid`, `getEvmAddrByLayerX`, `layerXBindNonce`, `getUnifiedAccount` (precompiles/addr/addr.go:40-43);
- keeper: modules/evm/keeper/layerx_binding.go:18, :30;
- binding message: the binder signs the domain `LX:PAXEER-BIND:v1` (modules/evm/types/layerx_binding.go:18) over the chain id and the EVM address. The bind transaction therefore carries both the DID signature and the EVM sender.

Every write precompile at 0x101A-0x101D and 0x0901-0x0908 resolves its principal by reading the sender's DID from this binding.

Account derivation:

- The main kernel account maps to the bound EVM address.
- Sub-accounts such as margin and agent accounts map to bank addresses derived from the DID and a suffix. Their balances are visible to bank, the explorer and ERC-20 pointers.

The ed25519 key signs only bindings and DID proofs. All other writes use the secp256k1 EVM signature.

### LayerX-scope authorizations (D3)

All eight kernel authority kinds stay (include/layerx/lxp_authority.h:11-20):

1. the account's own key;
2. session key;
3. delegated capability;
4. budget allowance;
5. escrow;
6. protocol module;
7. multisig;
8. timelock.

They live on sdk/x/authz through one `LayerXScopeAuthorization` type registered by layerxauth. That type gives one grant path for kernel calls and program calls. Its Accept rule reproduces the kernel authority check for each kind. For example, the session-key grant rules are at src/state/lxp_authority.c:164.

layerxauth 0x101C exposes:

- `grantSession`, `grantSessionSuccessor`, `grantCapability` and `revokeGrant`;
- views over grants and scopes;
- the shared idempotency index and its view `resultByIdempotency`.

The index refuses a replayed idempotency key even at a new nonce. That is the same outcome as the kernel's `LXP_ERR_IDEMPOTENT_REPLAY` (src/ledger/lxp_apply.c:323).

### One custody set

The embedded-wallet attestors in human/wallet/attestor are the one custody set:

- They hold the key shares and threshold-sign user transactions. The chain sees an ordinary secp256k1 signature, and the ed25519 signature for binds.
- They also sign for the agent-host web payer.
- They are the signer set of the tee attestor at 0x0907.

There is no second custody set. Separately, xweb keeps a single attestor registry for observations from both request origins (see 0x1019 above).

## Off-chain support

Each retained service class writes to the chain only through user-signed transactions, or through keyed EVM transactions that the chain authorises. None relays an unsigned write. The chain-facing contract of each service is in the service contracts document of this feature, under the service name given here.

- **Sequencer and standby.** paxd in sequencer and follower mode runs the proposer path above: receipts, pending reads, WAL, and the standby takeover at round 1. Contract: "Sequencer (primary and hot standby)".
- **Archive node.** A paxd archive deployment serves `eth_getProof`, ICS23 proofs and logs at past heights. It replaces the kernel replica and the relay archive. Contract: "Archive node".
- **Unified router and onboarding.** platform/hosted/gateway is the public entry point. It relays `eth_*` writes to the sequencer and serves compatibility reads. Today `lx_sendActivity` is routed to the kernel component (platform/hosted/gateway/src/rpc.rs:341); the `lx_*` ingress is switched off at activation. platform/hosted/identity and platform/hosted/faucet handle registration and test funds through ordinary user or faucet transactions. Contracts: "Unified router", "Identity registration service", "Faucet".
- **Receipt verification and settlement adapters.** platform/hosted/authority verifies sequencer receipts plus inclusion against the pinned receipt key. interop/crates/layerx-interop-service and platform/middleware carry x402 and the other payment protocols, with the user's own transaction as the settlement leg. platform/hosted/core keeps treasury sends. Contracts: "Receipt authority (verifier)", "Interop service and protocol adapters", "x402 seller middleware", "Core boundary (treasury)".
- **Gas station.** interop/crates/layerx-gas-station sponsors 7702 batches under DID-keyed budgets, and its rate publisher keeps the paymaster SID rate fresh (see the fee policy section). Contracts: "Gas station relayer", "Gas station SID rate publisher".
- **Web search sidecars.** interop/crates/x-websearch runs one sidecar per node. Each crawls and answers queries. Its `RequestWatcher` (interop/crates/x-websearch/src/watch.rs:299) follows requests of both origins and collects attestor signatures. It submits `fulfil` (modules/xweb/keeper/fulfil.go:93) or `fulfilProgram` to 0x1019, settles paid requests by `draw` on 0x101A, and releases results only after the receipt. Contract: "x-websearch sidecar (per node)".
- **Bridge and mirrors.** interop/crates/layerx-bridge-relayer submits `bridgeIn` under attestor threshold signatures (modules/layerxbridge/keeper/bridge.go:65) and releases on remote vaults. interop/crates/layerx-mirror and layerx-mirror-signer publish the chain's own header and app hash to remote archives. Contracts: "Bridge relayer (EVM and Solana)", "Mirror publisher (conditional)", "Mirror signer".
- **Trading drivers.** The price feeder (platform/hosted/feeder) pushes ed25519-signed observations to 0x1015 `pushOracle`. A new perps keeper service submits permissionless `fundingTick`, `liquidate` and `adl` calls from pending reads. Validator oracle feeders keep voting on modules/oracle. Contracts: "Price feeder (perps)", "Perps keeper (new)", "Validator oracle price feeders".
- **AI and program workers.** These services call layerxprograms 0x101D guests with keys bound through 0x1004:
  - platform/hosted/paxai-worker (workers);
  - platform/hosted/ai-artifacts (sealed evidence storage);
  - AI evaluators;
  - compute-lease providers;
  - stream metering providers.

  Contracts: "paxai-worker and runner", "ai-artifacts", "AI evaluator service (new)", "Compute-lease providers", "Stream metering provider".
- **Agent runtime.** agent/crates/layerx-agentd and layerx-agentd-host prepare, sign and submit agent EVM transactions. Pending nonces and estimates come from the sequencer, and the outbox retries by transaction hash. Contract: "agentd and agentd-host".
- **Human surfaces.** These services use the one attestor custody set:
  - human/wallet/gateway: sign, send, provision, review and approve;
  - human/wallet/attestor: threshold signing;
  - human/crates/layerx-human-service: web journeys;
  - human/crates/layerx-human-kms: KMS signing of EVM actions.

  The wallet app backends serve metadata only. Contracts: "Embedded wallet gateway", "Wallet attestors (TSS)", "Human service", "Human identity and security providers, KMS", "Wallet app backends".
- **Read side.** These services serve history, projections, catalogues and notifications from blocks, logs and module queries:
  - platform/hosted/indexer (the Paxeer half);
  - platform/hosted/webhooks;
  - platform/hosted/registry, rebuilt as a read cache over the layerxprograms module;
  - the explorer fork in explorer/.

  Contracts: "Indexer", "Webhooks", "Program registry read cache", "Explorer fork and explorer index".
- **Operations.** tools/ops/monitoring probes the chain, sequencer, router, indexer, gas station, attestors and mirrors. It alerts when receipt order and block order diverge, and when the sponsor balance reaches its floor. tools/rollout deploys the sequencer unit in ordered waves. The one-shot merge tools build the activation state. Contracts: "Monitoring", "Rollout controller", "One-shot merge tools".

## Activation

### One height

The merge is one named upgrade plan, registered beside the existing plans:

- plan constants: node/upgrades.go:82-107;
- handlers: `RegisterUpgradeHandlers`, node/upgrades.go:117-194;
- the plan is appended to `knownUpgradePlans` (node/upgrades.go:416-418);
- the plan name is set at activation rehearsal.

Its height equals the sequencer fork height H_act (D18). The activation is a bring-up at the activation height; no account-state migration is required.

At that height, three things happen:

1. **Store mounts and initialisation.** A store upgrade mounts layerxledger, layerxpay, layerxauth, layerxprograms and sequencer through the store loader, following the pattern at node/app.go:1307-1325. The plan handler runs module migrations and initialises the five new modules from genesis params.
2. **Anchor bond return.** The plan handler returns every posted guarantor bond and open challenge deposit held by layerxanchor.
3. **Precompile serving.** Addresses 0x101A-0x101D and 0x0901-0x0908 join `GetCustomPrecompiles` (precompiles/setup.go:63-97) and are served from the plan height only. Today the evm keeper holds one `CustomPrecompileActivation` (modules/evm/keeper/keeper.go:100, :195-223). It is set at node/app.go:940 from `activationPrecompileGate` (node/upgrades.go:398-411) and becomes a list of (plan name, address set) entries. Each entry is read through `IsUpgradeActiveAtHeight` (sdk/x/upgrade/keeper/keeper.go:296-299).

Reference guest programs (interpreter, compute-lease market, AI market) are deployed through `MsgDeploy` at activation.

### Clean break

The client-facing contract changes in one step (D17):

- agent-api major 2: 65-byte signature, new cursor format, `SettlementAnchored` removed;
- status rungs: submitted, receipted, committed;
- signed receipts only on `pax_sequencerReceipt` and `eth_sendRawTransactionSync`;
- same-nonce replacement refused;
- the `lx_*` ingress names switched off at activation.

Middleware, the x402 seller, the CLI, the agent SDKs, the gateway, and the explorer and wallet status views move to major 2 together.

### Retired executors

A retired method checks `IsUpgradeActiveAtHeight(ctx, <merge plan>, ctx.BlockHeight())`, following the gate at modules/layerxcustody/keeper/keeper.go:38-46. Below the height it behaves as today. At or above the height it returns `Error("LXP_MODULE_DISABLED: <precompile> retired")`.

- 0x1012 retires in full.
- 0x1013 retires in full.
- 0x1014 retires its writes at the height. Its reads retire after the bond return, when a later step deletes the anchor store.
- On 0x1015, `requestSettlement` and `getIntent` retire. The old selectors of the three re-signed methods `withdrawMargin`, `placeOrder` and `cancelOrder` return `LXP_PRECOMPILE_UNKNOWN_METHOD`.
- The addresses stay registered permanently, so no retired call ever returns empty success.

The off-chain components that existed only to bridge two domains retire with them:

- kernel admission and batch sealing (src/sequencer/lxp_admission.c:58, src/sequencer/lxp_batch_seal.c:19);
- the kernel receipt signature (src/protocol/lxp_receipt.c:859);
- the batch WAL (cmd/layerxd/lxp_daemon_batch_wal.c:362);
- the replica (cmd/layerxd/lxp_daemon_replica.c:216);
- the guarantor runtime;
- the intent ingester (interop/crates/layerx-intent-ingester);
- the exchange's custody margin hop (modules/layerxexchange/keeper/intents.go:63);
- the router's cross-domain joins.

### Served afterwards

| Address | Below the activation height | At or above the activation height |
|---|---|---|
| 0x1001 bank, 0x1004 addr, 0x1016 layerxBridge, 0x1018 feeToken | as today | as today, with `LXP_` revert text; bank also serves the context-hash `send` overload and its `LayerXReceipt` log |
| 0x1002, 0x1003, 0x1005-0x100C, 0x1011 | as today | as today |
| 0x1012 layerxVerify, 0x1013 layerxCustody | as today | `LXP_MODULE_DISABLED` |
| 0x1014 layerxAnchor | as today | writes `LXP_MODULE_DISABLED`; reads served until the anchor store is deleted, then `LXP_MODULE_DISABLED` |
| 0x1015 layerxExchange | intent recording | engine execution, new perps and spot methods, direct-state views |
| 0x1017 launchpad | as today | per-market protocol fee counter and `getProtocolFees` |
| 0x1019 xweb | EVM-origin requests | both origins; `fulfilProgram`, `getProgramResult`, `getKernelNetworkId` live |
| 0x101A-0x101D | not served | live |
| 0x0901-0x0908 | not served | live |

From the activation height, users get these from one network through one endpoint:

- one signed receipt per transaction at send;
- pending reads from the sequencer;
- the block at the promised height and index;
- finality at commit.
