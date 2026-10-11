# Sequencer contract (the sequencer as the chain and its clients see it)

| Item | Value |
|---|---|
| Version | 1.0.1 |
| Revision read | 52b672ac4 |
| Binding inputs | ADR D7, D14, D15, D17, D18, D19 |
| Change control | Change control: changes only by amendment; each amendment raises the version. |
| Conventions | "u64be" = big-endian unsigned; "arr32" = layerx-wire bounded array: u32be length (=32) then 32 bytes (agent/crates/layerx-wire/src/encode.rs:106-118); "addr32" = 12 zero bytes then the 20-byte EVM address; NEW = construct that does not exist at 52b672ac4 |

## (a) Signed receipt: canonical ProtocolReceipt v2 envelope carrying the EVM result, ed25519-signed by the separate receipt key (D15 A)

### (a1) Envelope fields in canonical order (encoder agent/crates/layerx-wire/src/receipt.rs:928-980, decoder :730-800)
| # | Wire field (receipt.rs:190-220) | Type and encoding | Paxeer value | Value source hook (file:line) |
|---|---|---|---|---|
| 1 | header | u16be protocol_version = 2, u16be structure tag 0x5201 | fixed; tag 0x5202 (supply) never used | encode.rs:187-202; receipt.rs:10; limits.rs:12 PROTOCOL_VERSION |
| 2 | protocol_version | u16be = 2 | fixed; must satisfy supported_protocol_version | agent/crates/layerx-proof/src/receipt.rs:25-27; layerx-wire/src/limits.rs:29-33 |
| 3 | activity_id = tx hash | arr32 | EVM tx: keccak256 of the raw signed tx; Cosmos tx: sha256 of tx bytes; never zero | modules/evm/types/tx.pb.go:84 Hash; zero refused at layerx-proof/src/receipt.rs:648 |
| 4 | global_sequence = sequence position | u64be | seq, gap-free, starts at 1 at the activation height, +1 per accepted tx | NEW node/sequencer Accept; committed counterpart modules/sequencer lastSeq (section c) |
| 5 | previous_state_root = prevSeqHash | arr32 | seqHash of seq-1 (genesis value at activation: 32 zero bytes) | NEW node/sequencer; checked on chain by MsgSequenceBatch.prevSeqHash (c2) |
| 6 | resulting_state_root = seqHash | arr32 | sha256("PAXEER/v1/sequence\0" ‖ prevSeqHash ‖ u64be(seq) ‖ activity_id) | NEW node/sequencer; recomputed by ProcessProposal (c3) |
| 7 | activity_root = return-data digest | arr32 | sha256(returnData); returnData from the EVM response | modules/evm/types/tx.pb.go:83 ReturnData |
| 8 | result_status | i32be | 0 when EVM status 1; 1 when EVM status 0 (revert or VM error; revert reason in returnData as Error(string) per D6) | modules/evm/types/receipt.pb.go:111 Status, :114 VmError |
| 9 | effects | u32be count (max 512, receipt.rs:15) then effects (a2) | effect 0 = execution summary; effects 1..n = logs | receipt.rs:479-513 decode_effect |
| 10 | fee_charged = fee amount | u128be | gasUsed × effectiveGasPrice converted into the payer's fee denom (base units of that denom) | modules/evm/keeper/feetoken.go:92 GetFeeTokenCharge; modules/evm/ante/fee.go:72-107 |
| 11 | batch_id = pending block height | arr32 | 24 zero bytes ‖ u64be(height of the open block the tx executed in); receipts bind order, not height: after a short failover slice or a stall the tx lands at a later height in receipt order (c4) | NEW node/sequencer L_open open; header height consensus/internal/state/execution.go:101 |
| 12 | module_id | u16be | next free id after LXP_MODULE_RESERVED_COUNT (include/layerx/lxp_module.h:14-27), set at activation rehearsal | include/layerx/lxp_module.h:26 |
| 13 | module_version | u32be = 1 | Paxeer receipt layout version 1 of this contract | this contract |
| 14 | parameter_version | u32be = 0 | fixed | this contract |
| 15 | operation = tx type | u8 | EVM tx type (0, 1, 2, 4); Cosmos tx = 0xFF | modules/evm/types/receipt.pb.go:103 TxType |
| 16 | asset = fee denom id | arr32 | sha256(fee denom string as UTF-8); the string itself is in the summary effect | modules/evm/types/params.pb.go:470-475 AllowedFeeDenom.Denom; feetoken.go:92 |
| 17 | amount | u128be | tx value in wei; a tx whose value exceeds 2^128-1 is refused at admission before Accept (above total supply, so unreachable for a funded tx) | rpc/send.go:189 tx.Value() |
| 18 | from | addr32 | EVM sender (recovered) | rpc/send.go:162 getSender; modules/evm/types/receipt.pb.go:112 |
| 19 | from_balance_before / from_balance_after | u128be each | sender native balance in wei before and after, read on the per-tx branch of L_open | NEW node/sequencer per-tx CacheTxContext (sdk/baseapp/baseapp.go:822 pattern) |
| 20 | from_sequence = nonce | u64be | tx nonce | modules/evm/ante/sig.go:70-80 |
| 21 | to | addr32 | recipient, or created contract address for a create | modules/evm/types/receipt.pb.go:105, :113 |
| 22 | to_balance_before / to_balance_after | u128be each | recipient native balance in wei before and after | NEW node/sequencer per-tx branch |
| 23 | transfer_set_root | arr32 = zero | no program outcome on this shape | receipt.rs:777-790 (outcome only when bytes remain) |
| 24 | authorization_hash | arr32 | keccak256 of the RLP of the EIP-7702 authorization list for type-4 txs, else zero | modules/evm/types/receipt.pb.go:103 TxType 4 |
| 25 | context_hash = receipt key id | arr32 | sequencer_id of the SequencerAuthorization the signing host's ed25519 receipt key is pinned under (one key per host) | agent/crates/layerx-proof/src/inclusion.rs:14-19 |
| 26 | timestamp | u64be, unix milliseconds | open time of the pending block (D7 A): fixed when L_open opens, equal to the block header time; EVM block.timestamp = timestamp / 1000; a tx re-proposed at a later height (c4) keeps its signed timestamp while block.timestamp follows the landing block | consensus/internal/state/state.go:279 (header time becomes the L_open open time); kernel parity include/layerx/lxp_batch.h:32 timestamp_ms |
| 27 | signature flag + signature | u8 (1) then u32be 64 then 64 bytes | ed25519 signature (a3) | receipt.rs:707-716, :975-978 |

### (a2) Effects
| Effect | module_id | ordinal | event_type | kind (include/layerx/lxp_receipt.h:14-18) | monetary | transfer_set_root | body (max 256 bytes, receipt.rs:16) |
|---|---|---|---|---|---|---|---|
| 0 execution summary | same as a1 #12 | 0 | 0 | 1 STATE | 0 | zero | gasUsed u64be ‖ cumulativeGasUsed u64be (within the pending block) ‖ effectiveGasPrice u64be ‖ txIndex u32be (position in the block's tx list, MsgSequenceBatch at 0) ‖ contractAddress 20 bytes (zero if none) ‖ sponsor 20 bytes (zero if not sponsored) ‖ sha256(vmError) 32 bytes ‖ returnData length u32be ‖ log count u32be ‖ feeDenom length u8 ‖ feeDenom UTF-8 (max 64) — at most 191 bytes |
| 1..n log i | same as a1 #12 | log index within the tx (u16be) | 0 | 3 EVENT | 0 | zero | address 20 bytes ‖ topic count u8 (0-4) ‖ topics 32 bytes each ‖ sha256(data) 32 bytes — at most 181 bytes |
| Sources | modules/evm/types/receipt.pb.go:104-115 (CumulativeGasUsed, ContractAddress, GasUsed, EffectiveGasPrice, TransactionIndex, VmError, Logs); :26-31 Log | | | | | | Sponsor = quote.sponsor of the Sponsored event, contracts/src/BatchCallAndSponsor.sol:71, :166 |
| Bound | Extend the effect count bound for this receipt shape so a tx with more than 511 logs is encoded in full; never refused (refusal would reject valid EVM txs); NEW bound in layerx-wire beside MAX_EFFECTS 512 (receipt.rs:15), carried as module_version 1 of this layout | | | | | | |

### (a3) Signature scheme and verification hooks
| Item | Rule | Hook (file:line) |
|---|---|---|
| Scheme | Pure ed25519 (RFC 8032) over the 32-byte digest as the message | agent/crates/layerx-crypto/src/ed25519.rs:54-60 verify_digest → verify_message |
| Unsigned preimage | Canonical bytes of a1 with signature flag 0 and no signature | layerx-wire/src/receipt.rs:1009-1018 encode_unsigned |
| Digest | sha256("LXP/v1/receipt\0" ‖ unsigned preimage) | layerx-wire/src/hash.rs:330-335 receipt_digest, :132-141 domain, :69 tag |
| Key | Separate ed25519 receipt key (D15 A; never secp256k1, never the validator consensus key consensus/privval/file.go); held in its own key file beside, not inside, the validator key file on each sequencer host; NEW loader in node/sequencer | D15 A |
| Verifier | verify_sequencer_signature(receipt_bytes, pinned_key): decode, byte-exact re-encode, version 2 or 3, non-zero activity_id, signature present, ed25519 over digest | agent/crates/layerx-proof/src/receipt.rs:631-667 |
| Pin: authority service | LAYERX_AUTHORITY_SEQUENCER_PUBLIC_KEY (64 hex), compared in the replica document | platform/hosted/authority/src/main.rs:75, :307; src/lib.rs:281-285, :500; src/native_state.rs:94 |
| Pin: /v1/settle | LAYERX_GATEWAY_SEQUENCER_{PUBLIC_KEY,ID,FIRST_BATCH,LAST_BATCH}_FILE → SequencerAuthorization (weak and non-canonical keys refused) | platform/hosted/gateway/src/main.rs:115-118, :1030-1036; src/lib.rs:905 route, :1285-1304 configured_sequencer; src/settlement.rs:189 |
| Pin: chain | modules/sequencer param ReceiptKeys: list of {keyId arr32, publicKey 32 bytes (ed25519), firstHeight u64, lastHeight u64}, changed only by governance param change; the primary and standby keys hold overlapping ranges | NEW modules/sequencer (section c) |
| Custody across primary and standby | One ed25519 receipt key per host (primary, standby), each pinned with its own keyId and height range in ReceiptKeys; the authority service and the settle endpoint hold the same list (NEW: they hold one key today, authority main.rs:307, gateway main.rs:1030-1036); receipts issued before a failover stay byte-identical because the standby serves the signed bytes replicated in WAL ACCEPT records (e1) | platform/hosted/authority/src/main.rs:307; platform/hosted/gateway/src/main.rs:1030-1036 |

### (a4) Key rotation procedure
| Step | Action | Hook (file:line) |
|---|---|---|
| 1 | Generate ed25519 key K2 on the sequencer host whose key rotates (each host rotates separately); assign keyId2 (32 bytes, distinct from every earlier keyId) | inclusion.rs:14-19 sequencer_id |
| 2 | Pick rotation height H2 > current height; K1 range ends at H2-1, K2 range starts at H2 | inclusion.rs:17-18 first/last; from_config refuses first > last :68-70 |
| 3 | Governance param change adds {keyId2, K2, H2, max} and closes K1 at H2-1 in modules/sequencer ReceiptKeys before H2 | NEW modules/sequencer; gov param path modules/evm/gov.go:26-41 pattern |
| 4 | Add K2 and its range to the ReceiptKeys list held by the authority service and the settle endpoint before H2; receipts on both sides of H2 verify against the list | NEW list loader; today one key and range: authority main.rs:307; gateway main.rs:1030-1036 |
| 5 | The sequencer signs every receipt whose batch_id height ≥ H2 with K2 and sets context_hash = keyId2 | a1 #11, #25 |
| 6 | Receipts below H2 remain verifiable under K1 forever; K1 is destroyed after H2 commits | inclusion.rs:14-19 range |
| 7 | Validator consensus keys are untouched | consensus/privval/file.go |

### (a5) Client verification
| Step | Check | Hook (file:line) |
|---|---|---|
| 1 | Fetch pax_sequencerReceipt(txHash) (b1) | rpc/server.go:134 pax namespace |
| 2 | verify_sequencer_signature(receipt bytes, pinned key) | layerx-proof/src/receipt.rs:631 |
| 3 | context_hash equals a pinned keyId from the ReceiptKeys list and batch_id height is within that key's [first, last] | inclusion.rs:14-19 |
| 4 | activity_id equals keccak256 of the raw tx the client signed | |
| 5 | JSON fields equal decoded fields; sha256(returnData) = activity_root; sha256(log.data) and topics equal each log effect | a1 #7, a2 |
| 6 | seqHash = sha256("PAXEER/v1/sequence\0" ‖ prevSeqHash ‖ u64be(seq) ‖ activity_id) | a1 #6 |
| 7 | Committed rung: eth_getTransactionReceipt returns equal status, gasUsed, logs at a block number ≥ batch_id height (equal unless re-proposed after failover or stall); the block's MsgSequenceBatch covers seq | rpc/tx.go:106-110; section c |
| 8 | Equivocation proof: two valid receipts under one keyId with equal global_sequence and different activity_id | The receipt authority service records the pair and raises a monitoring alert; no on-chain consumer; NEW beside platform/hosted/authority/src/lib.rs:281-285 |

## (b) Submission and read RPC (D17 B: signed receipt never on eth_getTransactionReceipt; rung names submitted / receipted / committed)

### (b1) Methods
| Method | Request | Response (JSON) | Rule | Hook (file:line) |
|---|---|---|---|---|
| eth_sendRawTransaction | [rawTx hex] | tx hash | On the sequencer: decode, node fee floor (f), nonce rule (b3), execute in receipt order, WAL fsync and standby ack (e), then return the hash; executable tx is receipted before the call returns; future-nonce tx returns the hash at rung submitted; any refusal is a JSON-RPC error and no receipt; on other nodes: forwarded to the sequencer, never broadcast to the local mempool | rpc/send.go:76-160 (proxy pattern :94-113 EvmProxy, today BroadcastTx :150) |
| pax_sequencerReceipt | [txHash] | {rung: "submitted" or "receipted" or "committed", txHash, seq, height, index, timestamp, status, returnData, logs: [{address, topics, data, logIndex}], gasUsed, cumulativeGasUsed, effectiveGasPrice, feeDenom, feeAmount, sponsor, from, to, contractAddress, nonce, vmError, keyId, receipt (hex canonical bytes, a1), blockHash (only at committed)}; numbers are 0x-hex quantities, hashes 0x-hex 32 bytes, addresses 0x-hex 20 bytes | Unknown hash: null; submitted: only txHash, nonce, rung; receipted and committed: every field, receipt bytes identical across rungs | NEW in rpc/, registered beside rpc/server.go:134 pax namespace |
| eth_getTransactionReceipt | [txHash] | standard Ethereum receipt | Committed only, unchanged; null before commit | rpc/tx.go:106-110 |
| eth_sendRawTransaction same-nonce replacement | — | error | Refused: a tx whose (sender, nonce) is already receipted or submitted is refused, never Superseded (D17) | modules/evm/ante/sig.go:70-80 (deliver mode nonce == next) |
| lx_* ingress | — | — | Switched off at activation (D17) | platform/hosted/gateway/src/paxeer.rs:172 (router relay point) |
| tx_search (CometBFT RPC, node RPC row) | {query, prove, page, per_page, order_by} | {txs: [{hash, height, index, tx_result, tx, proof (only when prove=true)}], total_count} | Committed txs only, unchanged by the sequencer; needs the KV event sink, else error "transaction searching is disabled"; query length bounded; order_by asc or desc; prove=true adds the merkle proof of the tx in its block's tx list | route consensus/internal/rpc/core/routes.go:52; TxSearch consensus/internal/rpc/core/tx.go:56-131 (sink check :57-58, prove :112-115); request consensus/rpc/coretypes/requests.go:51-56; response consensus/rpc/coretypes/responses.go:307-320 |
| abci_query with prove=true (CometBFT RPC, node RPC row) | {path, data, height, prove} | {response: {code, log, key, value, proofOps, height, codespace}} | Committed state at height; prove=true returns ICS23 proof ops only for a "/store/<name>/key" path (RequireProof), the store proof plus the multistore commit-info proof (ics23 CommitmentProof); prove with height <= 1 refused; a pruned height returns "proof is unexpectedly empty"; the proof verifies against the app hash of header height+1 | route consensus/internal/rpc/core/routes.go:70; ABCIQuery consensus/internal/rpc/core/abci.go:13-25; sdk/baseapp/abci.go:902-908; sdk/store/rootmulti/store.go:574-626; sdk/store/rootmulti/proof.go:10-16; CommitInfo.ProofOp sdk/store/types/commit_info.go:44-65 |
| header / commit app hash (CometBFT RPC, node RPC row) | header {height}; commit {height} | header: {header: {..., app_hash, ...}}; commit: {signed_header: {header, commit}, canonical} | app_hash in header H is the state after the txs of block H-1; commit at the latest height returns the seen commit with canonical=false, otherwise the canonical commit from block H+1; unchanged by the sequencer | routes consensus/internal/rpc/core/routes.go:44, :49; Header consensus/internal/rpc/core/blocks.go:188-200; Commit :216-245; AppHash field consensus/types/block.go:416; responses consensus/rpc/coretypes/responses.go:78-86 |

### (b2) Pending tag, per call (today getBlockNumber and ResolveHeight map pending to latest: rpc/utils.go:66, :99; rpc/watermark_manager.go:196). Every "error" in the other-node column is JSON-RPC -32000 "pending served by sequencer"; the router sends pending-tag calls to the sequencer
| Call | Rule on the sequencer and standby | Rule on every other node | Hook (file:line) |
|---|---|---|---|
| eth_call | pending = read-only snapshot of L_open after the last receipted tx | error -32000 "pending served by sequencer", never silent latest; the router sends pending-tag calls to the sequencer | rpc/simulate.go:151; node/app.go:2895 RPCContextProvider (NEW PendingCtxHeight) |
| eth_estimateGas | default tag becomes pending (today latest) | default latest; explicit pending: error | rpc/simulate.go:107-126 |
| eth_createAccessList | pending (already its default tag) against L_open | error | rpc/simulate.go:86-91 |
| eth_getBalance | pending from L_open snapshot | error | rpc/state.go:45 |
| eth_getCode | pending from L_open snapshot | error | rpc/state.go:62 |
| eth_getStorageAt | pending from L_open snapshot | error | rpc/state.go:79 |
| eth_getProof | pending refused (no committed root for L_open) | refused | rpc/state.go:110 |
| eth_getTransactionCount | pending = sequencer next-nonce map (b3), not mempool | forwarded to the sequencer (pattern :370-389), never mempool fallback | rpc/tx.go:358-393; consensus/internal/mempool/mempool.go:234 EvmNextPendingNonce (retired for pending on the sequencer) |
| eth_getBlockByNumber("pending") | the open block: number, timestamp (a1 #26), receipted tx hashes; no hash, no stateRoot | latest committed resolution unchanged | rpc/block.go:207, :257 |
| eth_blockNumber | latest committed height (pending never reported) | unchanged | rpc/info.go:52 |
| eth_getLogs | toBlock = pending includes logs of receipted txs in L_N and L_open | toBlock pending: error | rpc/filter.go:569 |
| eth_newFilter / eth_getFilterChanges / eth_getFilterLogs | filters with toBlock pending return receipted logs once, then the committed copy is not repeated | pending filters: error | rpc/filter.go:397, :458, :529 |
| eth_newBlockFilter | committed blocks only | unchanged | rpc/filter.go:422 |
| eth_newPendingTransactionFilter | receipted tx hashes in receipt order | stays unsupported | rpc/filter.go:445-455 |
| eth_subscribe newHeads | committed headers only | unchanged | rpc/subscribe.go:107, :181 |
| eth_subscribe logs | NEW logs-from-receipts stream fed by Accept, each log carries seq; committed logs not re-emitted | committed only | rpc/subscribe.go:226 |
| eth_subscribe newPendingTransactions | NEW handler on the sequencer: receipted tx hashes in receipt order, under the standard name | unsupported | no handler today in rpc/subscribe.go (newHeads :107, logs :226) |
| eth_getTransactionByHash | receipted tx returned with blockNumber = pending height, blockHash null | committed only | rpc/tx.go:269 |
| debug_trace* on pending | refused | refused | rpc/simulate.go:395-396 |
| eth_gasPrice / eth_feeHistory / eth_maxPriorityFeePerGas | base fee of the open block (fixed at L_open open) | committed values | rpc/info.go:100, :142, :292; modules/evm/keeper/fee.go:100 |
| pax_sequencerReceipt | served | forwarded to the sequencer | b1 |

### (b3) Nonce, estimation, replacement
| Rule | Value | Hook (file:line) |
|---|---|---|
| Executable | nonce == next(sender) on L_open: executed at receipt | |
| Future | nonce > next: held in a per-sender future queue at rung submitted; executed in nonce order the moment the gap fills, its seq is the fill moment; queue bound per sender and expiry: set at activation rehearsal | consensus/internal/mempool/tx.go:152 ready/pending split (pattern) |
| Too low | nonce < next: refused, "nonce too low" | sig.go:70-80 |
| Concurrent senders | ordering across senders = arrival order at Accept; one sender's txs never reorder | NEW node/sequencer Accept |
| Same-nonce replacement | refused at submitted and at receipted (D17) | |
| Gas estimation | eth_estimateGas against pending; the sequencer's own send path uses executed gasUsed, not simulateTx | rpc/send.go:124-128, :166-200 |
| Gas-station fee bump | Refused: the same-nonce Replacement path (zero-value self-transfer at the dropped submission's nonce) is deleted under the sequencer; a send is receipted or refused (D17) | interop/crates/layerx-gas-station/src/station.rs:391-406; journal.rs:94-99 Replacement |

## (c) Ordering and consensus (D18 A: one activation-height fork)

### (c1) Activation
| Item | Rule | Hook (file:line) |
|---|---|---|
| Activation height H_act | set at activation rehearsal | D18 follow-up |
| App-side gate | IsUpgradeActiveAtHeight(ctx, sequencer activation, height), the per-height gate pattern | modules/layerxcustody/keeper/keeper.go:42 |
| Consensus-side gate | Leader list and H_act in node config, identical on every validator; a node whose list or H_act differs must refuse to start | consensus/config/config.go (new fields beside :1160-1187) |

### (c2) modules/sequencer MsgSequenceBatch (NEW module, new store key)
| Field | Type | Meaning |
|---|---|---|
| height | u64 | height of the block carrying it; equals header height |
| fromSeq | u64 | first seq in the block; = stored lastSeq + 1 |
| toSeq | u64 | last seq; toSeq = fromSeq + (txCount - 1); empty block: toSeq = fromSeq - 1 |
| prevSeqHash | bytes32 | = stored lastSeqHash |
| lastSeqHash | bytes32 | seqHash of toSeq (a1 #6), = prevSeqHash when empty |
| txRoot | bytes32 | sha256 over the concatenation of activity_id (a1 #3) of block txs 1..n in order |
| keyId | bytes32 | receipt keyId whose ReceiptKeys range contains height |
| sig | bytes64 | ed25519 by the proposing host's receipt key (never secp256k1) over sha256("PAXEER/v1/sequence-batch\0" ‖ chain-id UTF-8 ‖ u64be height ‖ u64be fromSeq ‖ u64be toSeq ‖ prevSeqHash ‖ lastSeqHash ‖ txRoot) |
| Store | lastSeq u64, lastSeqHash bytes32, ReceiptKeys (a3), set in the Msg handler |
| Position | always tx 0 of every block at height ≥ H_act |
| Envelope | Injected unsigned by the proposer at proposal build (no app PrepareProposal handler exists at 52b672ac4; injection point consensus/internal/state/execution.go:101 CreateProposalBlock), authenticated by the ed25519 sig above with the receipt key pinned in modules/sequencer ReceiptKeys, verified in ProcessProposal (c3 check 9); no fee, no signer account |

### (c3) Leader, proposal, ProcessProposal, BeginBlock
| Item | Rule | Hook (file:line) |
|---|---|---|
| Leader() | height ≥ H_act: Leader(h, r) = L[r mod len(L)] with L = [primary, standby] (validator pubkeys, each in the validator set); below H_act the sha256(height, round) power-weighted rule is unchanged | consensus/internal/consensus/types/round_state.go:422-437; callers consensus/internal/consensus/state.go:1101, :1154, :2077, :2232, :2707 |
| Proposal content | In sequencer mode the block = [MsgSequenceBatch] + Seal() slice of the WAL in seq order, replacing mempool ReapTxs | consensus/internal/state/execution.go:101, :126 |
| Header time | = open time of L_open (a1 #26) instead of tmtime.Now(); accepted while within Precision 505 ms + MessageDelay 12 s | consensus/internal/state/state.go:279; consensus/types/params.go:168-176; consensus/internal/consensus/state.go:1347 proposalIsTimely |
| ProcessProposal compares | (1) tx 0 decodes as MsgSequenceBatch; (2) header.ProposerAddress ∈ L; (3) height field = header height; (4) fromSeq = lastSeq + 1; (5) prevSeqHash = lastSeqHash; (6) toSeq - fromSeq + 1 = len(txs) - 1; (7) txRoot recomputed from txs 1..n; (8) lastSeqHash recomputed by folding seqHash over txs 1..n; (9) keyId range contains height and sig verifies; then the existing gas check | node/app.go:1457 ProcessProposalHandler, before :1477 checkTotalBlockGas; clean context sdk/baseapp/abci.go:968, :992-1003 |
| ProcessProposal rejects | any check failing → REJECT (validators prevote nil, next round goes to the next L entry); alert, never retry the same block | node/app.go:1457-1480 (REJECT shape :1462-1468) |
| BeginBlock vote feed | height ≥ H_act: BeginBlock(N+1) receives the votes of N-1, stored in modules/sequencer at the end of block N from that block's DecidedLastCommit; at H_act itself BeginBlock receives an empty vote list and stores the votes of H_act-1 (no height double-counted, rewards and liveness lag one height) | node/abci.go:60 call site; node/legacyabci/begin_block.go:43-73; FinalizeBlocker node/app.go:1551 |
| Upgrade heights | the sequencer never opens L_open at or past a height with a scheduled plan | node/app.go:1500 plan.ShouldExecute |
| Validators | execute the block with OCC unchanged and commit | node/app.go:2091 ProcessBlock; :1888 ProcessTXsWithOCCV2 |
| Sequencer's own commit | adopts L_N as stateToCommit when the block equals the sealed slice, else re-executes | node/app.go:1551, optimistic path :1495-1541 |

### (c4) Failover with a sequencer-signed but unproposed sequence
| Case | Validator action | Hook (file:line) |
|---|---|---|
| Primary silent at round 0 | Propose timeout, prevote nil; round 1 leader = standby, which proposes the same height with the WAL slice lastSeq+1..its last seq and the recorded open time; validators apply c3 checks only | round_state.go:422; consensus/types/params.go:194-203 |
| Slice shorter than receipted seqs | Accepted (prefix keeps continuity); receipts bind order, not height: the next leader replays the WAL and proposes every remaining receipted tx in receipt order at the next height; receipts are not re-issued, batch_id and timestamp stay as signed | e3 Resume; a1 #11, #26 |
| Slice differs from receipts | Validators cannot see receipts and accept a continuous slice; deviation is proven only off chain by equivocation (a5 step 8) | a5 step 8: authority records and alerts |
| Primary returns | Follows the standby's WAL, proposes at round 0 only a slice continuous with stored lastSeq; a non-continuous proposal is refused by c3 (4)-(5) | node/app.go:1457 |
| Stall longer than Precision + MessageDelay | Header time of the recorded open time fails proposalIsTimely; the leader opens a fresh L_open at the next height with a new open time and proposes the receipted txs in receipt order; the user keeps the issued receipt (seq, signature unchanged) and sees the committed rung at the landing height | consensus/internal/consensus/state.go:1347 |

## (d) Execution mode (D19 B, strictly sequential first in wave 0)

| Item | Rule | Hook (file:line) |
|---|---|---|
| Executor interface | Accept(txBytes) → (canonical signed receipt, error); Seal(height) → (txs, lastSeq, lastSeqHash); Open(height, timestamp_ms); two implementations: Sequential, MicroBatch | NEW node/sequencer |
| Sequential | each tx: per-tx CacheTxContext over L_open, app.DeliverTx in deliver mode, write on success, one receipt | node/abci.go:169 DeliverTx; sdk/baseapp/baseapp.go:822 CacheTxContext pattern |
| MicroBatch | txs that arrived inside one window, in arrival order, run through DeliverTxBatch with the OCC scheduler on a branch of L_open; receipts released in seq order after the batch and one WAL fsync | node/abci.go:230 DeliverTxBatch, :247 tasks.NewScheduler; worker count sdk/server/config/config.go:41-48 |
| Micro-batch bound | window about 1 ms (D19 B); maximum txs per batch: set at activation rehearsal | D19 |
| Invariant | for the same seq order, MicroBatch receipts are byte-identical to Sequential receipts and L_open state hashes equal; a mismatch stops the sequencer (fail closed) | occ_tests/occ_test.go:108-180 (OCC equals sequential) re-targeted |
| Block parity invariant | validators' OCC execution of the sealed block yields the same app hash and identical eth_getTransactionReceipt fields (except blockHash) as the receipts | node/app.go:2091; rpc/tx.go:106 |
| Licensing measurement | same recorded workload in both modes: throughput (tx/s) and receipt latency p50 and p99 at the sequencer, plus the invariant check; switch to MicroBatch only when its throughput is higher and its p99 rise is within the window bound; thresholds set at activation rehearsal | D19 follow-ups |
| Check-mode branches | never used on the sequencer: Accept always runs deliver mode | modules/evm/ante/fee.go:114, :130; sdk/x/auth/ante/validator_tx_fee.go:33 |
| Ante read context | in sequencer mode LatestCtxGetter returns the L_open context, not checkCtx | node/app.go:1122-1124 |

## (e) Durability

### (e1) WAL record
| Layer | Format | Hook (file:line) |
|---|---|---|
| Frame | u32be crc32c(entry) ‖ u32be len(entry) ‖ entry; file group and size limit reused | consensus/internal/libs/wal/writer.go:94-95; reader.go:16, :65-70; log.go:36-38 |
| Entry | u8 version = 1 ‖ u8 kind ‖ payload | NEW node/sequencer/wal |
| kind 1 OPEN | u64be height ‖ u64be timestamp_ms | a1 #11, #26 |
| kind 2 ACCEPT | u64be seq ‖ u64be height ‖ u32be txLen ‖ tx bytes ‖ u32be receiptLen ‖ canonical signed receipt (a1) | a1 |
| kind 3 SEAL | u64be height ‖ u64be toSeq ‖ lastSeqHash 32 bytes | c2 |
| kind 4 COMMITTED | u64be height ‖ u64be lastSeq | FinalizeBlocker node/app.go:1551 |
| kind 5 LEASE | u64be epoch ‖ keyId 32 bytes of the holding host ‖ u64be expiry_ms; epoch rises by one per handover (fencing) | NEW node/sequencer/wal |
| Truncation | records at or below the last COMMITTED are deleted only after the standby has also recorded COMMITTED | NEW |

### (e2) fsync and replication
| Rule | Value | Hook (file:line) |
|---|---|---|
| fsync | group commit: Sync (fdatasync) of every ACCEPT in the group before any of its receipts is released | consensus/internal/libs/wal/writer.go:110; log.go:195-198 |
| Standby ack | standby appends and syncs the same records, then acks; no ack → no receipt, send returns an error (fail closed) | NEW node/sequencer follower |
| Order | the receipt is returned only after local fsync and standby ack | b1 |

### (e3) Hot-standby handover
| Item | Rule | Hook (file:line) |
|---|---|---|
| Who is leader | round 0 primary, round 1 standby, by the pinned list (c3); the node holding the open-block lease accepts txs; the lease is a LEASE record (e1 kind 5) in the replicated WAL with a rising epoch; the standby takes over only after the lease expires and writes the next epoch; a record with a lower epoch is refused (fencing); lease duration equals the round-0 propose timeout (g) | round_state.go:422; consensus/types/params.go:194-203 |
| Standby state | replays every ACCEPT through the same executor into its own L_N / L_open, so its state and receipts match | node/abci.go:169 |
| Resume | on round-1 leadership: seals lastSeq+1..its last ACCEPT with the recorded OPEN timestamp (a fresh OPEN when that time fails proposalIsTimely, c4), proposes, then accepts new txs at seq = last + 1 | c2, c3, c4 |
| Receipts re-signed | none: issued receipts stay valid as issued; pax_sequencerReceipt on the standby serves the signed bytes from the replicated ACCEPT records, byte-identical under the primary's keyId; new receipts carry the standby's keyId | a3; e1 kind 2 |
| Split brain | the loser's proposal fails c3 (4)-(5) continuity; each node signs consensus with its own privval state, so no double sign | consensus/privval/file.go |

### (e4) User-visible guarantee after an unplanned stop
| Event | Guarantee |
|---|---|
| Primary stops unexpectedly after receipt | every receipted tx is on two synced disks and lands at its seq; extra delay is one propose timeout (round 0 → 1) |
| Unplanned stop before receipt | no receipt; the send returned an error or timed out; the client re-sends the same raw tx (same hash, same nonce) |
| Standby unreachable | sends refused with an error; no receipt is issued unreplicated |
| Both lose the WAL tail | the only receipt-breaking case; excluded by the ack rule |

## (f) Fee policy (D14 A, D7 A, D10 A)

| Item | Value | Where set (file:line) | Enforced by |
|---|---|---|---|
| EVM minimum gas price | at most 0.067 gwei (67 000 000 wei) per D10, one global param; the D10 draw sender class is met by this global floor at or below 0.067 gwei, no class param; exact value set at activation rehearsal; default today 1 gwei | modules/evm/types/params.go:52 DefaultMinFeePerGas, key :21 KeyMinFeePerGas; checked modules/evm/ante/fee.go:54-56 | chain ante, deliver mode on the sequencer |
| Base fee | dynamic, fixed per open block at L_open open; gasFeeCap must be ≥ base fee and ≥ minimum | modules/evm/keeper/fee.go:100; ante/fee.go:51-56, :154-167 | chain ante |
| Node floor (Cosmos txs) | default "0.01uhpx"; the sequencer applies its configured floor explicitly before Accept (deliver mode skips the CheckTx check); value set at activation rehearsal | daemon/paxd/cmd/root.go:397; sdk/x/auth/ante/validator_tx_fee.go:33 | sequencer node |
| SID self-paid | payer selects SID via feetoken setFeeDenom at 0x…1018; charge converted at the governed rate | precompiles/feetoken/feetoken.go:19, :22; modules/evm/keeper/feetoken.go:92; ante/fee.go:72-107 | chain ante |
| SID rate | 3 114 000 SID base units (6 decimals) per PAX = 3.114 SID/PAX; changed by governance ParameterChangeProposal, each change within the 5 % spread | modules/evm/types/params.go:36, :42; params.pb.go:470-475 AllowedFeeDenom{Denom, Rate, RateUpdateHeight}; modules/evm/gov.go:26-41 | chain (gov handler) |
| SID rate age | in blocks; default 1000; raised by governance (D7 A); value set at activation rehearsal, in the same proposal that re-tunes the AI and market height windows and xweb TimeoutBlocks | modules/evm/types/params.go:39, key :16; modules/evm/keeper/params.go:310-336 (stale → ErrFeeTokenRateStale) | chain ante |
| Paymaster rate (sponsored) | setRate; staleness block.timestamp - rateUpdatedAt > maxRateAge reverts; service refuses quotes older than max_rate_age ≤ 300 s | contracts/src/BatchCallAndSponsor.sol:88-95, :113-120; interop/crates/layerx-gas-station/src/config.rs:147-148; rate.rs:440 | contract + gas station |
| Sponsorship path | every sponsored write = type-4 tx from the relayer calling executeSponsored on the user's 7702-delegated account | BatchCallAndSponsor.sol:135-167 | contract |
| executeSponsored checks | quote sponsor/token=SID/gasCost/amount; quote expiry vs block.timestamp (= pending-block open time, D7); amount ≤ max; quoteNonce unused per sponsor; account signature over SponsoredBatch(nonce, callsHash, quoteDigest); relayer signature over quoteDigest; price within 5 % spread; SID transfer to sponsor | BatchCallAndSponsor.sol:141-166, :169-174, :123-132 | contract |
| Paymaster nonce rule | one nonce per account: the contract's nonce slot at the delegated account, read from pending state, consumed in _executeBatch; one sponsored batch per nonce value; quote nonce is per sponsor and separate | BatchCallAndSponsor.sol:20, :196-198, :50; gas station batch_nonce station.rs:331-333 | contract |
| Relayer EVM nonce | relayer account nonce from the sequencer pending map (b3) | rpc/tx.go:358 | sequencer |
| Per-identity budget key | DID from addr precompile 0x1004 getLayerXDid at pending state; unbound account = its EVM address | precompiles/addr/addr.go:40, :172 | gas station |
| What the gas station checks | per-quote, per-DID (D14), per-interval limits and sponsor balance floor before signing; refusal consumes no budget | interop/crates/layerx-gas-station/src/policy.rs:76-125 reserve | gas station |
| What the gas station records | journal QuoteRecord {sponsor, quoteNonce, chainId, paymaster, account, token, maximum, amount, expiry field, gasCost, issuedAt, signature, fees}, Submission {nonce, hash, raw}; NEW DID field in QuoteRecord carrying the budget key from 0x1004 (D14) | interop/crates/layerx-gas-station/src/journal.rs:30-50, :85-92 | gas station |
| What the chain sees | an ordinary type-4 tx, the Sponsored(sponsor, token, tokenAmount, quoteNonce) event and the SID transfer; no budget state on chain | BatchCallAndSponsor.sol:71, :166 | chain |
| Gas station down | no quote, no sponsored write (fail closed); the user may self-pay | D14 consequence | gas station |

## (g) Timing under D18 A

| Quantity | Value | Source (file:line or record) |
|---|---|---|
| Receipt latency target | p50 ≤ 10 ms: decode + ante + execute + WAL group fsync + one standby round trip; independent of block time and consensus round (D18 A pipelining) | e2 |
| Receipt latency today | none at send: hash after CheckTx; result only after commit | rpc/send.go:139-159 |
| Inclusion latency | from receipt to the commit of its pending height: at most one block interval, recorded ~175 ms header-timestamp block time | recorded block-time measurement |
| Finality | at commit (2/3+ precommits of the validator set); no reorg after commit; inclusion and finality coincide | consensus BFT commit |
| Failover added delay | one propose timeout: 1 s (+500 ms per further round) | consensus/types/params.go:194-203 |
| Default timeouts | Propose 1 s, ProposeDelta 500 ms, Vote 50 ms, VoteDelta 500 ms, Commit 50 ms, BypassCommitTimeout false | consensus/types/params.go:194-203 |
| Node overrides | UnsafeProposeTimeoutOverride, UnsafeVoteTimeoutOverride, UnsafeCommitTimeoutOverride; commit 5 ms rolled out (recorded) | consensus/config/config.go:1160-1187 |
| Header-time tolerance | Precision 505 ms, MessageDelay 12 s | consensus/types/params.go:168-176 |
| Block limits | MaxGas 100 000 000, MaxGasWanted 50 000 000, MaxBytes 22 020 096 | consensus/types/params.go:135-143 |
| Block-path costs (settlement only) | ApplyBlock 174/45 ms mean/median, synchronous prune 120/30 ms | consensus/internal/state/execution.go:409-424; recorded |
