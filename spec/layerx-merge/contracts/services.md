# services.md off-chain service contracts (revision 52b672ac4)

| Item | Value |
|---|---|
| Contract | one frozen chain-facing contract per operator-run off-chain service after the merge |
| Version | 1.0.1 |
| Change control | changes only by amendment; each amendment raises the version. |
| Binding inputs | ADR D7, D10, D11, D12, D13, D14, D15, D17, D20; README.md |
| Conformance | CONFORMING file: method names, signatures, selectors, receipt shape and error codes are owned by the construct contracts (ledger-auth.md, pay.md, exchange-launchpad.md, programs.md, web.md, agent-functions.md) and sequencer.md; this file takes theirs; section 3 cites the construct of every chain method a service calls |
| Scope | 36 services: 33 retained, 3 new (perps keeper, AI evaluator service, archive node) |
| Construct tags | every read or write is tagged with its construct (bank, addr, evm, feetoken, layerxexchange, layerxbridge, xweb, layerxledger, layerxauth, layerxpay, layerxprograms, layerxagent, sequencer, oracle, BatchCallAndSponsor) or "node RPC" = sequencer.md section (b) |
| Path roots | all file:line citations are relative to the repository root at 52b672ac4 |

## 1. Service contracts
One block per service.

### S01 Sequencer (primary and hot standby)
| Field | Value |
|---|---|
| service | Sequencer, primary plus hot standby (follower mode) |
| package path | new: node/sequencer in daemon/paxd (sequencer mode, follower mode); module modules/sequencer (new) |
| reads | all committed state L_c plus its own L_N/L_open; pending reads served for others through node RPC on the L_open snapshot (hook node/app.go:2895 RPCContextProvider; today rpc/utils.go:66 maps pending to latest) |
| writes | the block itself in receipt order (consensus/internal/state/execution.go:101 CreateProposalBlock, Seal() replaces ReapTxs at :126), one per height; modules/sequencer `MsgSequenceBatch{height,fromSeq,toSeq,prevSeqHash,lastSeqHash,txRoot,keyId,sig}` (sequencer.md c2) injected unsigned by the proposer at CreateProposalBlock as tx 0 of every block at height >= H_act, verified in ProcessProposal (sequencer.md c2 Envelope, c3); submission `eth_sendRawTransaction` returning the hash after execution in receipt order, WAL fsync and standby ack, then the signed ProtocolReceipt v2 served by `pax_sequencerReceipt(txHash)` with rung submitted / receipted / committed (sequencer.md a1, b1; today rpc/send.go:76 returns the hash only, :139-159), one per accepted tx |
| signing key | (1) validator consensus key, privval file loaded by consensus/privval/file.go:233 LoadFilePV, authorised by the pinned-leader list in Leader() consensus/internal/consensus/types/round_state.go:422 (primary round 0, standby round 1; D18). (2) receipt key: one separate ed25519 key per host (primary, standby) per D15, own key file beside, not inside, the validator key file, NEW loader in node/sequencer (sequencer.md a3 Key); each pinned with keyId and height range in the modules/sequencer param ReceiptKeys list (sequencer.md a3 Pin), held as the same list by the authority service and settle endpoint (today one key: platform/hosted/authority/src/main.rs:307 LAYERX_AUTHORITY_SEQUENCER_PUBLIC_KEY), the x-websearch config (interop/crates/x-websearch/src/config.rs:874), agentd (agent/crates/layerx-agentd/src/main.rs:483) and the seller middleware (platform/middleware/seller/src/commitment.ts:96); verifier agent/crates/layerx-proof/src/receipt.rs:631 verify_sequencer_signature; rotation per sequencer.md a4. (3) MsgSequenceBatch.sig: ed25519 by the proposing host's receipt key over sha256("PAXEER/v1/sequence-batch\0" ‖ chain-id ‖ height ‖ fromSeq ‖ toSeq ‖ prevSeqHash ‖ lastSeqHash ‖ txRoot) (sequencer.md c2) |
| liveness requirement | every write path stops without it: no receipt, no pending read, no block in round 0. Chain tolerance: standby proposes the WAL slice at round 1 (one consensus round of delay); receipts fail closed when the standby ack does not arrive |
| failure mode and recovery | primary down: standby takes round 1 from the replicated WAL; standby ack absent: sends return an error, never a receipt; WAL tail gone on both: the only receipt-breaking case, excluded by synchronous replication; block out of receipt order: refused in ProcessProposal (node/app.go:1457) and alerted, not retried |
| on-chain evidence | MsgSequenceBatch per block (store lastSeq, lastSeqHash, ReceiptKeys in modules/sequencer, sequencer.md c2); block header time = L_open open time (D7) |
| monitoring signal | sequencer readyz; receipt seq vs committed index divergence (new alert); WAL fdatasync latency; standby ack lag; replaces KernelReadyzDown (tools/ops/monitoring/rules.yml:56) |
| reports that define it | sequencer report (rows 1-8), kernel, trading, web, agent, platform, human reports; ADR D15, D18, D19, D7 |

### S02 Archive node (new)
| Field | Value |
|---|---|
| service | Archive node |
| package path | new: daemon/paxd run with pruning off (deployment, no new code path); none deployed today |
| reads | all historical committed state; serves node RPC (sequencer.md b2, committed tags only) `eth_getProof` (rpc/state.go:110 GetProof), `eth_getLogs` (rpc/filter.go:569), `eth_getBlockByNumber` (rpc/block.go:207) at past heights; ICS23 `abci_query prove=true` for module state: sequencer.md b1 node RPC row |
| writes | none |
| signing key | none (read-only node; node identity key only) |
| liveness requirement | historical proofs and old-height reads stop; current-state reads unaffected. Chain tolerance: unlimited (no chain effect); AI job verification and storage retirement history (programs report rows 13, 42) wait |
| failure mode and recovery | down: historical reads return an error, never a current-height substitute; recover by restart from its own data or a snapshot copy |
| on-chain evidence | none |
| monitoring signal | earliest retained height equals genesis or the activation height; head lag vs a full node |
| reports that define it | platform report (archive need), programs report rows 13, 42, kernel report (historical IAVL proofs), agent report |

### S03 Unified router
| Field | Value |
|---|---|
| service | Unified router (public entry) |
| package path | platform/hosted/gateway |
| reads | sequencer pending state via node RPC (sequencer.md b2: `eth_call` rpc/simulate.go:151, `eth_getTransactionCount` pending rpc/tx.go:358, `eth_getTransactionReceipt` committed rpc/tx.go:106); pending-tag calls sent to the sequencer, other nodes answer -32000 (sequencer.md b2); sequencer `pax_sequencerReceipt(txHash)` (sequencer.md b1); addr 0x1004 `getLayerXDid`, `getEvmAddrByLayerX`, `getUnifiedAccount` (precompiles/addr/addr.go:40-43); bank 0x1001 `balance`, `all_balances` (precompiles/bank/bank.go:24-25); asset catalogue for px_listAssets: layerxledger 0x101A `getAsset(bytes32 assetId)` (ledger-auth.md layerxledger B) joined with bank denom metadata; new router px_ routes, added to the px_ dispatch (platform/hosted/gateway/src/paxeer.rs:790-795) and the route catalogue (rpc.rs:1140-1148), none present at 52b672ac4: `px_getReceiptKeys` proxies the modules/sequencer params query and returns the ReceiptKeys list {keyId, publicKey, firstHeight, lastHeight} (sequencer.md a3, c); `px_getCommit(height)` and `px_getValidators(height)` proxy CometBFT `commit` and `validators` (consensus/internal/rpc/core/routes.go:49, :54) from an operator full node, validators stay private; `px_register` forwards to S04 and `px_requestFunds` to S05, replacing `lx_register` and `lx_requestFunds` (rpc.rs:340; rpc_register.rs:399; rpc_faucet.rs:358) when lx_* ingress switches off |
| writes | none of its own; relays user-signed raw EVM txs to sequencer `eth_sendRawTransaction`, then serves the receipt from `pax_sequencerReceipt` (sequencer.md b1); lx_sendActivity and every lx_* ingress switched off at activation (sequencer.md b1, D17); `eth_subscribe` relay to the sequencer for `logs` and `newPendingTransactions` (sequencer.md b2; today refused, platform/hosted/gateway/src/paxeer.rs:172) |
| signing key | none; the chain authorises the user's EVM signature |
| liveness requirement | the public entry: all router-path reads and writes stop. Chain tolerance: unlimited (chain unaffected; direct node RPC still works for operators) |
| failure mode and recovery | sequencer unreachable: unavailable error, writes refused with -32003, never relayed unsigned; unprobed write target: refused; recover by restart, stateless |
| on-chain evidence | none (user txs carry their own) |
| monitoring signal | probe of eth_chainId and a pending-read round trip; write refusal rate |
| reports that define it | platform report rows 1-7, 13, 14, 16; agent, human, web, programs reports |

### S04 Identity registration service
| Field | Value |
|---|---|
| service | Identity registration (lx_register until activation, then router `px_register` (S03); agent.register) |
| package path | platform/hosted/identity; router handler platform/hosted/gateway/src/rpc_register.rs:96 binding |
| reads | addr 0x1004 `getLayerXDid`, `getEvmAddrByLayerX`, `layerXBindNonce` |
| writes | none itself; the user submits addr 0x1004 `bindLayerX` (precompiles/addr/addr.go:38), once per identity; sponsored-bind flow through S11 (wave 5) |
| signing key | none of its own; bind authorised by the user's ed25519 DID signature over LX:PAXEER-BIND:v1 plus the EVM sender |
| liveness requirement | new registrations stop (503); existing bindings unaffected. Chain tolerance: unlimited |
| failure mode and recovery | down: 503, no partial registration; a bind tx already submitted lands independently |
| on-chain evidence | addr binding record and its bind event from bindLayerX |
| monitoring signal | registration success rate; bind tx receipt status |
| reports that define it | platform report row 4; agent report (agent.register; required change: the agentd path completes agent.register on the D3 authorisation and 0x1004 paths, agent/crates/layerx-agentd/src/agent_rpc_dispatch.rs:1355-1359) |

### S05 Faucet
| Field | Value |
|---|---|
| service | Faucet (lx_requestFunds until activation, then router `px_requestFunds` (S03); faucet.claim) |
| package path | platform/hosted/faucet; router platform/hosted/gateway/src/rpc_faucet.rs |
| reads | faucet account balance (bank 0x1001 `balance`) and pending nonce (node RPC `eth_getTransactionCount` pending, rpc/tx.go:358) |
| writes | one EVM transfer (bank 0x1001 `send`) per admitted claim, bounded by identity, address and network quotas (platform/hosted/faucet/src/main.rs:273-334) |
| signing key | faucet secp256k1 account key, ordinary EVM signature and balance, loaded by the one shared keystore-or-KMS EVM sender loader selected by env, sender address registered with gas-station sponsorship; today the faucet holds no chain key: it posts to a funding endpoint (main.rs:903 fund, config LAYERX_TESTNET_FUNDING_URL main.rs:310) |
| liveness requirement | new claims stop (503); no chain effect. Chain tolerance: unlimited |
| failure mode and recovery | down or empty: 503, no credit; pending or refused claim never reported as funded (rpc_faucet.rs test at :510); idempotency key replays the same funding id |
| on-chain evidence | EVM transfer receipt from the faucet account |
| monitoring signal | faucet account balance vs floor; claim refusal rate |
| reports that define it | platform report row 4; agent report |

### S06 Receipt authority (verifier)
| Field | Value |
|---|---|
| service | Receipt authority |
| package path | platform/hosted/authority |
| reads | sequencer `pax_sequencerReceipt(txHash)` (sequencer.md b1); node RPC `eth_getTransactionReceipt` committed rung (sequencer.md b1, rpc/tx.go:106) on one full node for inclusion |
| writes | none |
| signing key | none; verifies the ed25519 receipt signature against the ReceiptKeys list (keyId, publicKey, firstHeight, lastHeight; two overlapping entries, primary and standby) per sequencer.md a3 Pin and a5; today one key loaded at platform/hosted/authority/src/main.rs:307; records equivocation evidence and alerts (sequencer.md a5 step 8) |
| liveness requirement | /v1/settle, /v1/authorized-batches, webhooks and interop verification stop returning verified. Chain tolerance: unlimited |
| failure mode and recovery | unverified or sequencer down: 202 pending, never a false success; recover by restart, stateless |
| on-chain evidence | none |
| monitoring signal | verify latency; pending-to-verified age |
| reports that define it | platform report row 17; kernel report |

### S07 Core boundary (treasury)
| Field | Value |
|---|---|
| service | Core boundary: treasury SEND, withdrawal config and caps reads |
| package path | platform/hosted/core |
| reads | withdrawal config and caps: held in the core boundary service config, no chain read; bank 0x1001 `balance` of the treasury account (precompiles/bank/bank.go:24) |
| writes | EVM transfer from the treasury account (bank 0x1001 `send`, precompiles/bank/bank.go:22, or a plain value transfer), per operator request, no batching; caps enforced by the service before signing |
| signing key | secp256k1 treasury key, ordinary EVM signature, loaded by the shared keystore-or-KMS EVM sender loader selected by env. Today: ed25519 treasury signer over a socket (platform/hosted/core/src/custody.rs:28 TreasurySigner, socket at main.rs:306 LAYERX_CORE_TREASURY_SIGNER_SOCKET) |
| liveness requirement | treasury sends stop; no chain effect. Chain tolerance: unlimited |
| failure mode and recovery | down: no send and no partial send; retry by operator |
| on-chain evidence | EVM transfer receipt from the treasury address |
| monitoring signal | signer socket reachability; treasury balance |
| reports that define it | platform report row 19 |

### S08 Indexer
| Field | Value |
|---|---|
| service | Indexer (Paxeer half; LayerX half retired) |
| package path | platform/hosted/indexer |
| reads | node RPC committed blocks and receipts (sequencer.md b2: rpc/block.go:207, rpc/tx.go:106), `eth_getLogs` (rpc/filter.go:569); CometBFT `tx_search`: sequencer.md b1 node RPC row; layerxprograms 0x101D logs `ProgramGuestEvent`, `ProgramCallOutcome` and the other programs.md section C logs through eth_getLogs (no Query ProgramEvents); decoded logs of layerxexchange (exchange-launchpad.md C), layerxledger and layerxauth (ledger-auth.md C, incl. PaymentDrawn and PaymentExact), layerxpay (pay.md C), xweb (web.md C), layerxbridge (precompiles/layerxbridge/layerxbridge.go:49-50) |
| writes | none (own database) |
| signing key | none |
| liveness requirement | history, AI market views (/v1/ai/markets) and rating projections go stale; no chain effect. Chain tolerance: unlimited |
| failure mode and recovery | stale reads served with a freshness head; resumes from its last indexed height |
| on-chain evidence | none |
| monitoring signal | indexed head vs chain head lag |
| reports that define it | platform report rows 12, 15, 21; programs report rows 14, 38, 41; agent, kernel reports |

### S09 Webhooks
| Field | Value |
|---|---|
| service | Webhooks (merchant and AI market notifications) |
| package path | platform/hosted/webhooks |
| reads | authority routes (S06) and indexer projections (S08); no direct chain read |
| writes | none |
| signing key | none (chain); delivery signing is service-side |
| liveness requirement | notifications late or undelivered; chain unaffected. Chain tolerance: unlimited |
| failure mode and recovery | redelivery from the authority and indexer cursor |
| on-chain evidence | none |
| monitoring signal | delivery backlog age |
| reports that define it | platform report; programs report row 41 |

### S10 Program registry read cache
| Field | Value |
|---|---|
| service | Program registry service as read cache (D20 A) |
| package path | platform/hosted/registry (read path platform/hosted/registry/src/program_state.rs:5, to be re-pointed) |
| reads | layerxprograms 0x101D views `getProgram(bytes32)` (0xb1ffb3d4), `getInterface(bytes32)` (0x8efc7240), `getStateHead(bytes32)` (0x0833650b) (programs.md B); no `registryLookup` view and `registryRead` is a write (programs.md A 0xfb28c4b1), so neither is called; ICS23 proofs of those reads: sequencer.md b1 node RPC row `abci_query prove=true` |
| writes | none |
| signing key | none |
| liveness requirement | catalogue, naming and LXT20/LXT721 views go stale; 0x101D views stay authoritative. Chain tolerance: unlimited |
| failure mode and recovery | stale catalogue; rebuild from module queries |
| on-chain evidence | none |
| monitoring signal | cache height vs chain head |
| reports that define it | platform report row 18; programs report rows 17, 18; ADR D20 |

### S11 Gas station relayer
| Field | Value |
|---|---|
| service | Gas station: sponsored submission (/quote, /submit, /status, /retry at interop/crates/layerx-gas-station/src/service.rs:270-274) |
| package path | interop/crates/layerx-gas-station |
| reads | sponsor pending nonce from the sequencer next-nonce map (node RPC `eth_getTransactionCount` pending, sequencer.md b2, b3; rpc/tx.go:358); addr 0x1004 `getLayerXDid` at pending state for the per-DID budget key (D14; sequencer.md f; precompiles/addr/addr.go:40); BatchCallAndSponsor `currentRate` (contracts/src/BatchCallAndSponsor.sol:113), `quoteDigest` (:123), `sponsoredBatchDigest` (:129) |
| writes | type-4 tx carrying the user's EIP-7702 authorisation calling BatchCallAndSponsor `executeSponsored` (:135), one per quote, one paymaster nonce per account (D14; sequencer.md f), submitted by `eth_sendRawTransaction` (sequencer.md b1); no cancellation or same-nonce replacement: a send is receipted or refused, the Replacement path (station.rs:391-406, journal.rs:94-99) is deleted (sequencer.md b3); journal QuoteRecord gains the DID budget key (sequencer.md f); also sponsors the feeder, perps keeper, attestor submitter, worker, agentd, faucet, treasury and onboarding sender addresses registered with it, and the wallet gateway and human service onboarding writes (D14) |
| signing key | relayer secp256k1 key from the env variable named by `relayer_key_env` (interop/crates/layerx-gas-station/src/signer.rs:32-41 from_config/from_env; config field config.rs:104, validated :164); the chain authorises the user's 7702 delegation and account signature over the sponsored batch; the contract checks quote, expiry, nonce and rate (:169 _checkPrice) |
| liveness requirement | sponsored writes stop; budgets fail closed (D14); self-paid users unaffected. Chain tolerance: unlimited for the chain; feeder and keeper txs must then be self-funded or they stop (see S19, S20 tolerances) |
| failure mode and recovery | budget (per_quote, per_account, per_interval: config.rs:99-101, validated :152-159) or balance floor (config.rs:103) hit: quote refused before signing; refused send: no receipt, the nonce stays free for the next quote (no same-nonce replacement, sequencer.md b3); sequencer down: intent held in the journal, recover() reconciles against pax_sequencerReceipt (sequencer.md b1) |
| on-chain evidence | `Sponsored(sponsor, token, tokenAmount, quoteNonce)` event (BatchCallAndSponsor.sol:71), `BatchExecuted` (:179), SID pulled to the sponsor |
| monitoring signal | sponsor balance vs balance_floor (new alert); quote refusal rate per reason; journal unresolved count |
| reports that define it | platform report row 23; sequencer report row 8 and fee table; kernel, programs report row 43, agent, human report rows 14, 46; ADR D14 |

### S12 Gas station SID rate publisher
| Field | Value |
|---|---|
| service | SID/PAX paymaster rate publisher |
| package path | interop/crates/layerx-gas-station (rate.rs:367 publish) |
| reads | BatchCallAndSponsor `currentRate` (:113); operator rate file (rate.rs:118 load); operator pending nonce and balance |
| writes | BatchCallAndSponsor `setRate(uint256)` (:88), every `rate_cadence_seconds` (rate.rs:190), journaled before broadcast, within `rate_gas_budget_per_day` (:191) |
| signing key | separate operator secp256k1 key from the env variable named by `rate_owner_key_env` (rate.rs:189), validated distinct from the relayer key (rate.rs:225-227); contract onlyOwner on setRate |
| liveness requirement | publications stop: the old rate stays; sponsored quotes are refused once the rate is older than `max_rate_age` (config.rs:147-148, at most 300 s); cadence strictly below max_rate_age (rate.rs:168-170). The chain feetoken rate (self-paid SID via 0x1018) is not published by this service: it moves only by governance (modules/evm/gov.go:26-41) and goes stale after `MaxFeeTokenRateAge` blocks (default 1000, modules/evm/types/params.go:39; read modules/evm/keeper/params.go:310); D7 raises it by governance (sequencer.md f, g); the value is set at activation rehearsal |
| failure mode and recovery | operator balance below `rate_balance_floor`, budget exhausted or fee above `rate_max_fee_per_gas`: publication refused (rate.rs:360-367); unconfirmed publication retried after `rate_confirmation_retry_seconds` |
| on-chain evidence | `RateUpdated(rate, updatedAt)` event (BatchCallAndSponsor.sol:73) |
| monitoring signal | now minus last RateUpdated updatedAt vs max_rate_age (new alert); chain feetoken rate age vs MaxFeeTokenRateAge |
| reports that define it | platform report row 24; sequencer report row 8 and fee table; ADR D7 |

### S13 Interop service and protocol adapters
| Field | Value |
|---|---|
| service | Interop gateway/service and adapters (x402, AP2, UCP, Visa TAP, fiat, portable receipts) |
| package path | interop/crates/layerx-interop-gateway, layerx-interop-service, layerx-x402, layerx-ap2, layerx-ucp, layerx-visa-tap, layerx-fiat, layerx-portable |
| reads | sequencer `pax_sequencerReceipt(txHash)` ProtocolReceipt v2, rung submitted, receipted or committed (sequencer.md a1, b1); node RPC `eth_getTransactionReceipt` (rpc/tx.go:106) for committed inclusion only, never the signed receipt (sequencer.md b, D17 B); layerxledger 0x101A `getPayerGrant(bytes32)` returns (grant, drawnTotal, drawnThisPeriod, windowStart, revokedAt, revoked, invoiceSettled) (ledger-auth.md B), `getAsset(bytes32)` (ledger-auth.md B) |
| writes | none of its own; settlement legs are user-signed EVM txs to layerxledger 0x101A `draw(bytes32 from, bytes32 assetId, uint128 amount, bytes32 grantId, uint64 receiverSequence, bytes32 idempotencyKey, bytes32 contextHash)` (ledger-auth.md A; D10) or `payExact(bytes32 from, bytes32 to, bytes32 assetId, uint128 amount, uint64 expiresAt, bytes32 paymentReference)` (ledger-auth.md A), relayed by `eth_sendRawTransaction` then `pax_sequencerReceipt` (sequencer.md b1) |
| signing key | none for user effects; the user's EVM signature authorises |
| liveness requirement | payment protocols are not accepted; chain unaffected. Chain tolerance: unlimited |
| failure mode and recovery | down: payment refused, never relayed unsigned; no receipt: retry with the same idempotencyKey (draw) or paymentReference (payExact), no same-nonce replacement (sequencer.md b3); portable receipts verify offline against the ReceiptKeys list, key chosen by keyId and height range (sequencer.md a3, a4) |
| on-chain evidence | user tx receipts; layerxledger logs `PaymentDrawn(bytes32 indexed grantId, bytes32 indexed from, bytes32 indexed to, bytes32 assetId, uint128 amount, bytes32 contextHash)` and `PaymentExact(bytes32 indexed from, bytes32 indexed to, bytes32 indexed paymentReference, bytes32 assetId, uint128 amount)`, emitted by the precompile from the applied transfer set (ledger-auth.md C) |
| monitoring signal | adapter refusal rate; receipt verification failures |
| reports that define it | platform report rows 26-28; web report rows 15, 17; agent report |

### S14 x402 seller middleware
| Field | Value |
|---|---|
| service | x402 seller middleware |
| package path | platform/middleware/seller (rpc.ts, commitment.ts) |
| reads | sequencer `pax_sequencerReceipt(txHash)` (sequencer.md b1); layerxledger logs `PaymentDrawn` and `PaymentExact` with the ledger-auth.md C fields via node RPC `eth_getLogs` |
| writes | none |
| signing key | none; verifies the ed25519 receipt signature over sha256("LXP/v1/receipt\0" + unsigned envelope) (sequencer.md a3) against the ReceiptKeys list {keyId, publicKey, firstHeight, lastHeight} (sequencer.md a4), replacing the single pinned key (platform/middleware/seller/src/commitment.ts:96 sequencerPublicKey) |
| liveness requirement | merchant requests are not served; chain unaffected. Chain tolerance: unlimited |
| failure mode and recovery | unverifiable receipt: request not served; payer retries with the same idempotency key |
| on-chain evidence | none of its own |
| monitoring signal | verify failure rate per merchant |
| reports that define it | agent report (x402 verification); platform report row 27; web report row 15 |

### S15 x-websearch sidecar (per node)
| Field | Value |
|---|---|
| service | x-websearch: crawl, index, search, attest, signature exchange, fulfil, paid /search and /fetch |
| package path | interop/crates/x-websearch (RequestWatcher watch.rs:299; submit.rs:65-184) |
| reads | xweb 0x1019 `getRequest(uint64)` 0xcbbf67ec, `getAttestors()` 0x1a15922d, `threshold()` 0x42cde4e8, `fee()` 0xddca3f43, `getParams()` 0x5e615a6b, `getKernelNetworkId()` 0xfc0d26f4 (web.md B1, B3-B6, B8); origin-1 `XWebRequested` logs via node RPC `eth_getLogs`; origin-2 requests are layerxprograms 0x101D `ProgramGuestEvent` logs under topic PAXEERX_WEB_REQUEST_V1, no xweb event (web.md A9, C origin 2; programs.md C); layerxledger `getPayerGrant(bytes32)` (ledger-auth.md B); sequencer `pax_sequencerReceipt` (sequencer.md b1) |
| writes | xweb `fulfil(uint64 requestId, bytes response, bytes32 contentDigest, uint32 fullLength, bytes[] signatures)` 0xaeb8a7ec (web.md A2; modules/xweb/keeper/fulfil.go:93) per origin-1 request at threshold; xweb `fulfilProgram(bytes observation)` 0x2d4bf7ad (web.md A10, height-gated) per origin-2 request, network checked against Params kernelNetworkId uint32 (web.md F3), fee in one asset of Params programFeeDenoms PAX, SID, USDC, USDL (web.md F4); layerxledger `draw(bytes32,bytes32,uint128,bytes32,uint64,bytes32,bytes32)` (ledger-auth.md A; D10) once per paid request at USD 0.001, under the one global EVM minimum gas price set at or below 0.067 gwei (sequencer.md f); refusals are LXP_XWEB_* codes (web.md A) |
| signing key | attestor secp256k1 key (X_WEBSEARCH_ATTESTOR_KEY_FILE, interop/crates/x-websearch/src/keys.rs:8, loaded :133) in the ONE xweb attestor set for both origins (web.md A4, F6), registered by Cosmos `MsgRegisterAttestor` (modules/xweb/keeper/msg_server.go:71), registration capped at 16 (web.md A4: today's MaxAttestors 64, modules/xweb/types/keys.go:21, lowered to 16 for registration), threshold strict majority (modules/xweb/types/state.go:171), 3 of 4 today; submitter EVM key (keys.rs:9), sponsored by S11 as a registered sender; receiver key (keys.rs:10) = grant recipient for draw |
| liveness requirement | below threshold: no answer, origin-1 request refundable by `refund(uint64)` 0xd7194ccb (web.md A3) after `TimeoutHeight` (modules/xweb/keeper/refund.go:28; default timeout 3600 blocks modules/xweb/types/state.go:69, block-based so its wall time shrinks under the sequencer cadence, re-tuned under D7, set at activation rehearsal); origin 2 has no refund and no timeout (web.md F4; D11); paid HTTP: no receipt, no release |
| failure mode and recovery | partition: fulfil stalls, origin 1 refunds on expiry; sequencer down: no receipt, no release, retry by idempotency key; search fault: 5xx before payment |
| on-chain evidence | `XWebFulfilled`, `XWebRefunded` (modules/xweb/types/events.go:5-6) for origin 1; origin-2 result readable by `getProgramResult(bytes32,uint64)` 0x2b2e13fc (web.md B7); `PaymentDrawn` per paid request (ledger-auth.md C) |
| monitoring signal | attestors live vs threshold; requests nearing TimeoutHeight unfulfilled; draw receipt latency |
| reports that define it | web report rows 1, 2, 7, 8, 11-14, 17-21; programs report rows 25-29; platform report rows 29, 30; human report row 7; ADR D10, D11, D12 |

The kernelNetworkId value is set at activation rehearsal.

### S16 Bridge relayer (EVM and Solana)
| Field | Value |
|---|---|
| service | Bridge relayer |
| package path | interop/crates/layerx-bridge-relayer |
| reads | layerxbridge 0x1016 `getChain`, `getAttestors`, `getCap`, `isPaused`, `isNullified` (code-owned, outside the construct contracts: precompiles/layerxbridge/layerxbridge.go:41-50, README.md (d)); `BridgeOut` logs via node RPC `eth_getLogs`; remote vault `BridgeDeposit` logs; sequencer `pax_sequencerReceipt` for confirmation (sequencer.md b1) |
| writes | layerxbridge `bridgeIn` (code-owned: precompiles/layerxbridge/layerxbridge.go:41-50; modules/layerxbridge/keeper/bridge.go:65) once per attested remote deposit with the attestor signature set, sent by `eth_sendRawTransaction` (sequencer.md b1); remote vault `release` per BridgeOut |
| signing key | bridge attestor secp256k1 key and per-chain fee keys held by the remote signer (S18 load_bridge interop/crates/layerx-mirror-signer/src/lib.rs:456); relayer config holds handles and public keys only (interop/crates/layerx-bridge-relayer/src/config.rs:320 load); chain checks the threshold signatures over the inbound digest (bridge.go:33 verifySignatures); any submitter |
| liveness requirement | deposits wait, outbound releases delay; never dropped (nullifier). Chain tolerance: unlimited |
| failure mode and recovery | relayer down: resume from the relayer journal; duplicate submit refused by isNullified |
| on-chain evidence | `BridgeIn`, `BridgeOut` events (precompiles/layerxbridge/layerxbridge.go:49-50); nullifier record |
| monitoring signal | oldest unrelayed deposit age; journal backlog |
| reports that define it | human report rows 50, 51, 54, 55; platform report row 31 |

### S17 Mirror publisher (conditional)
| Field | Value |
|---|---|
| service | Mirror publisher (retained and re-pointed) |
| package path | interop/crates/layerx-mirror |
| reads | committed header and app hash from a full node: header and block via node RPC `eth_getBlockByNumber` finalized/latest (sequencer.md b2), Comet header app hash: sequencer.md b1 node RPC row header / commit app hash; layerxanchor `statusOf` (interop/crates/layerx-mirror/src/node.rs:624) is read only until bonds are returned, then the store is deleted; from the anchor store-delete step the nine anchor reads return LXP_MODULE_DISABLED (address-table.md c), and S17 takes finality from block height and commit only (`eth_getBlockByNumber` finalized, router `px_getCommit`) |
| writes | none on Paxeer; LayerXMirrorArchive begin/append/finalize on Ethereum and Solana PDA advance |
| signing key | Ethereum and Solana keys via S18 (client key path interop/crates/layerx-mirror/src/signer.rs:51); remote contracts onlyPublisher |
| liveness requirement | archive lag only; no Paxeer chain effect. Chain tolerance: unlimited |
| failure mode and recovery | resume from last finalized archive segment |
| on-chain evidence | none on Paxeer |
| monitoring signal | archived height vs committed height |
| reports that define it | platform report row 42; sequencer report |

### S18 Mirror signer
| Field | Value |
|---|---|
| service | Mirror signer (private socket signer) |
| package path | interop/crates/layerx-mirror-signer |
| reads | none |
| writes | none (signs for S17 and, through load_bridge, for S16) |
| signing key | Ethereum secp256k1 key (LAYERX_MIRROR_SIGNER_ETHEREUM_KEY_FILE, lib.rs:44, loaded :428), Solana key (lib.rs:46), bridge keys (load_bridge lib.rs:456); operator-only socket |
| liveness requirement | mirror stops and bridge attestation and release signing stop; no Paxeer chain effect. Chain tolerance: unlimited (bridge deposits wait, see S16) |
| failure mode and recovery | down: callers fail closed; restart with the same key files |
| on-chain evidence | none |
| monitoring signal | socket reachability; signature request error rate |
| reports that define it | platform report row 43 |

### S19 Price feeder (perps)
| Field | Value |
|---|---|
| service | Perps price feeder |
| package path | platform/hosted/feeder |
| reads | layerxexchange 0x1015 `getOracle(bytes32 market)` returns (uint64 sequence, uint256 price, uint64 observedAtMs), `getMarket(bytes32 market)` returns PerpsMarket incl. permittedOracleKeys (exchange-launchpad.md B, E9) |
| writes | layerxexchange 0x1015 `pushOracle(bytes observation, bytes signature)` (exchange-launchpad.md P3): observation exactly 72 bytes, signature 64-byte ed25519 over "LXP:ORACLE:OBSERVATION:v1" + observation, one per market per round every `cadence_ms` (platform/hosted/feeder/src/lib.rs:88); today submitted over the LNI socket (main.rs:44 LAYERX_FEEDER_LNI_SOCKET), retargeted to an EVM tx through S11; there is no Msg form |
| signing key | ed25519 oracle key (LAYERX_FEEDER_ORACLE_KEY_FILE, platform/hosted/feeder/src/main.rs:47; load_key lib.rs:479) in the market's permittedOracleKeys (1..8), checked in intake (src/modules/perps/lx_oracle_intake.c:114); EVM sender MUST be the 0x1004-bound address of that oracle key, else LXP_UNAUTHORIZED_ORACLE (exchange-launchpad.md P3, H5); sponsorship through S11 keeps that sender; EVM sender key through the shared loader |
| liveness requirement | `cadence_ms` must stay below each market's `maximum_oracle_staleness_ms` (include/layerx/lx_perps.h:178). Chain tolerance: maximum_oracle_staleness_ms; beyond it ORACLE_STALE (src/modules/perps/lx_perps_dispatch.c:306-307): place, open, increase, funding, liquidate and ADL refused; cancel and margin add allowed |
| failure mode and recovery | stops: markets go stale until the next accepted push; fresh observation at block-open timestamp can hit TIMESTAMP_REGRESSION (D7 follow-up) |
| on-chain evidence | oracle record per market (price, observed_at, sequence) in layerxexchange oracle store |
| monitoring signal | per-market now minus observed_at vs maximum_oracle_staleness_ms; health endpoint ready window 3 x cadence (main.rs:55-58) |
| reports that define it | trading report row 3; kernel report |

### S20 Perps keeper (new)
| Field | Value |
|---|---|
| service | Perps keeper: funding ticks, liquidation scanner, ADL ranking |
| package path | new: platform/hosted/perps-keeper |
| reads | pending state via sequencer reads (sequencer.md b2): layerxexchange 0x1015 `getMarket(bytes32)` (E9), `getPosition(bytes32 market, bytes32 positionId)` (E11), `getMargin(address,bytes32)` (E12), `getPerpsBook(bytes32 market)`, `getFunding(bytes32)` returns (int256 index, uint64 lastTickMs), `getOracle(bytes32)`, `getDeficit(bytes32)` (exchange-launchpad.md B); open-position enumeration for scanning and ADL ranking by layerxexchange 0x1015 view `getPerpsPositions(bytes32 market)` (0xc0451774) returns Position[] (at most 128, positionId order; exchange-launchpad.md B), a 0x1015 view rather than a gRPC query |
| writes | layerxexchange 0x1015 `fundingTick(bytes32 market)` (P9) per market every `funding_interval_ms` (include/layerx/lx_perps.h:177); `liquidate(bytes32 market, bytes32 positionId, bytes32 liquidatorAccount)` returns covered (P10) when a position crosses maintenance; `adl(bytes32 market, bytes32[] positionIds)` returns deficit (P11, 1..16 ids strictly ascending) when a deficit remains (exchange-launchpad.md A); no Msg forms; no batching across markets. This keeper is the only driver of fundingTick and adl; there is no EndBlock driver (exchange-launchpad.md H6) |
| signing key | any EVM key (permissionless; liquidatorAccount == caller DID main, exchange-launchpad.md P10), loaded by the shared keystore-or-KMS loader and registered with S11 sponsorship |
| liveness requirement | funding: skipped ticks catch up whole elapsed intervals at the then-current rate (src/modules/perps/lx_perps_funding.c:142); liquidation: late liquidation grows bad debt absorbed by insurance then ADL; with no EndBlock driver nothing ticks while this service is down (H6); chain tolerance: no hard bound, loss bounded by insurance fund |
| failure mode and recovery | down: funding stale, deficit stays recorded; restart scans pending state and submits; duplicate tick refused by validate_funding_tick (src/modules/perps/lx_perps_dispatch.c:790) |
| on-chain evidence | funding index state (lx_perps_state.c:167); events FundingSettled, Liquidated, Deleveraged (exchange-launchpad.md P9-P11) from execute_funding_tick (lx_perps_dispatch.c:1468), execute_liquidate (:1583) and execute_adl (:1676) |
| monitoring signal | now minus last funding tick per market vs funding_interval_ms; positions below maintenance not liquidated; insurance fund balance |
| reports that define it | trading report rows 9, 10, 11, 26; kernel report |

### S21 paxai-worker and runner
| Field | Value |
|---|---|
| service | paxai-worker and runner (inference and embedding) |
| package path | platform/hosted/paxai-worker |
| reads | ai-market program state (roster, tasks) at committed height through layerxprograms 0x101D `getStateHead(bytes32 programId)` 0x0833650b (programs.md B) |
| writes | layerxprograms 0x101D `call(bytes32 programId, uint16 abiVersion, string entrypoint, bytes calldata, bytes capabilities, bytes accessDeclaration, uint32 responseCapacity, uint64[7] budget, uint128 feeLimit)` 0x54bd2b48 (programs.md A) to ai-market with ACCEPT_TASK, COMMIT_TASK_RESULT, Heartbeat (programs/crates/layerx-programs-ai-market/src/admission.rs:823), SealEvidence, EnrollWorker (workers.rs:827); one tx per op, sponsored by S11 |
| signing key | admitted delegate ed25519 key (delegate_seed, platform/hosted/paxai-worker/src/main.rs:43, read :73) signs JobResult (src/jobs.rs:226-228); EVM sender key bound to the worker DID via addr 0x1004, loaded by the shared keystore-or-KMS loader and registered with S11 sponsorship |
| liveness requirement | task timeout means CANCEL with no reward; absent heartbeats remove admission; AI height windows re-tuned under D7, set at activation rehearsal |
| failure mode and recovery | runner refuses: no commit; worker exit: fenced re-dispatch; stale metadata: WRONG_ROSTER until next epoch |
| on-chain evidence | layerxprograms `ProgramCallOutcome` and `ProgramGuestEvent` logs for each op (programs.md C; `ProgramCalled` is never emitted); task and roster records in ai-market state |
| monitoring signal | heartbeat age vs window; task timeout count |
| reports that define it | programs report rows 31, 32, 33, 39; trading report row 28 |

### S22 ai-artifacts
| Field | Value |
|---|---|
| service | ai-artifacts (encrypted model, dataset and evidence storage) |
| package path | platform/hosted/ai-artifacts |
| reads | sealed evidence digests and block commit for retention through layerxprograms 0x101D `getStateHead(bytes32)` 0x0833650b (programs.md B) |
| writes | none (chain holds digests only) |
| signing key | none |
| liveness requirement | reveals and challenges needing payloads fail; chain never accepts a missing digest. Chain tolerance: bounded by the ai-market reveal and challenge windows (D7 re-tune, set at activation rehearsal) |
| failure mode and recovery | unavailable evidence: challenge or refusal on reveal; restore from replicas |
| on-chain evidence | none of its own |
| monitoring signal | retention ledger vs sealed digests; fetch error rate |
| reports that define it | programs report rows 30, 40 |

### S23 AI evaluator service (new)
| Field | Value |
|---|---|
| service | AI evaluator service (F03 assessment signer, F04 score commit and reveal, F05 aggregation driver) |
| package path | new service package in wave 1, proposed platform/hosted/ai-evaluator over agent/sdk/python/layerx_sdk/ai_market.py and agent/sdk/typescript/src/ai_market.ts (today SDK processes only) |
| reads | evaluator grants and epoch state through layerxprograms 0x101D `getStateHead(bytes32)` 0x0833650b (programs.md B) |
| writes | layerxprograms 0x101D `call(...)` 0x54bd2b48 with trailing uint128 feeLimit (programs.md A) to ai-market: CommitScore (0x0401) and RevealScore (0x0402) (programs/crates/layerx-programs-ai-market/src/commit_reveal.rs:564, :598), aggregation steps; one per epoch per grant |
| signing key | evaluator ed25519 key granted by operator ScheduleEvaluator 0x0301 (src/evaluators/authority.rs:656), SignedReport bound to grant and key version; SDK signer interface ai_market.py:798; keys through the shared loader; EVM sender bound via 0x1004, sponsored by S11 |
| liveness requirement | absent evaluator excluded; below quorum 3 (src/aggregation.rs:55) no result, funds stay refundable; reveal window height-based (D7 re-tune, set at activation rehearsal) |
| failure mode and recovery | absent reveal: commitment forfeits; recover secrets by the SDK |
| on-chain evidence | ai-market score commitment and reveal records; `ProgramGuestEvent` and `ProgramCallOutcome` logs (programs.md C) |
| monitoring signal | evaluators active per market vs quorum; unrevealed commitments near window end |
| reports that define it | programs report rows 34, 35, 36 |

### S24 Stream metering provider
| Field | Value |
|---|---|
| service | Stream metering provider |
| package path | third-party provider, not shipped here; the repository ships the protocol and the signer interface |
| reads | layerxpay 0x101B `getStream(bytes32 streamId)` (pay.md B), meter authority list |
| writes | layerxpay 0x101B `streamMeter(bytes32 streamId, uint64 cumulativeReading, bytes32 authorityKey, bytes signature)` (pay.md A.3) per metering period; one stream state: agent streams 0x0906 run through this same 0x101B keeper (pay.md A.7) and 0x0906 has no meter method, so metering always targets the 0x101B ABI |
| signing key | ed25519 key in the stream's meter authority list, signature exactly 64 bytes (pay.md D), verified by src/modules/stream/lx_stream_meter.c:27 lx_stream_meter_authority_check; any relayer sender; loader external |
| liveness requirement | accrual stops while no attestation lands (same as today, lx_stream_meter.c:58 lx_stream_metered_accrue). Chain tolerance: unlimited |
| failure mode and recovery | resume with the next attestation covering the gap |
| on-chain evidence | stream accrual record; `StreamMetered(bytes32 indexed streamId, uint64 cumulativeMeter, uint128 accrued, uint128 accruedTotal)` (pay.md C) |
| monitoring signal | per-stream last meter age |
| reports that define it | kernel report (stream metering) |

### S25 Compute-lease providers
| Field | Value |
|---|---|
| service | Compute-lease providers |
| package path | guest programs/crates/layerx-programs-market (dispatch src/lib.rs:846), deployed by the operator at activation (programs.md F; D13); provider binaries are third-party |
| reads | market program state (offers, leases, disputes) through layerxprograms 0x101D `getStateHead(bytes32)` 0x0833650b (programs.md B) |
| writes | layerxprograms 0x101D `call(...)` 0x54bd2b48 with trailing uint128 feeLimit (programs.md A) to the market guest ops 1-17 (offers, leases, attest, settle, dispute moves) |
| signing key | provider key staked in the market program (programs/crates/layerx-programs-market/src/stake.rs); loader external |
| liveness requirement | a move not made by its height limit loses; height limits re-tuned with the block cadence (D7), set at activation rehearsal |
| failure mode and recovery | slash per stake.rs:256 on a dispute decided against the provider |
| on-chain evidence | offer, lease, claim, settlement records (src/lib.rs:48-51 prefixes) |
| monitoring signal | open disputes near their height limit |
| reports that define it | programs report rows 20, 21; trading report row 29 |

### S26 agentd and agentd-host
| Field | Value |
|---|---|
| service | agentd and agentd-host (policy, approvals, sessions, outbox, web payer) |
| package path | agent/crates/layerx-agentd, agent/crates/layerx-agentd-host |
| reads | sequencer pending nonce and estimate: node RPC `eth_getTransactionCount` and `eth_estimateGas` at pending, served by the sequencer (sequencer.md b2, b3; rpc/tx.go:358, rpc/simulate.go:151); addr 0x1004 `getLayerXDid` (code-owned, precompiles/addr/addr.go:40); layerxauth 0x101C `resultByIdempotency(bytes32 didId, bytes32 idempotencyKey)` returns receipt (ledger-auth.md layerxauth B); layerxledger 0x101A `getPayerGrant(bytes32)` (ledger-auth.md B); layerxprograms 0x101D `getProgram(bytes32)` 0xb1ffb3d4, `getInterface(bytes32)` 0x8efc7240 (programs.md B); layerxpay 0x101B `getBudget(bytes32)` (pay.md B) |
| writes | user or agent EVM txs, sent by `eth_sendRawTransaction` then `pax_sequencerReceipt` (sequencer.md b1): layerxprograms 0x101D `call(...)` 0x54bd2b48, `simulateCall(...)` 0x3044bd56 (both with trailing uint128 feeLimit), `deploy(bytes32 programId, uint16 abiVersion, uint8 policy, bytes32 authority, bytes32 codeHash, bytes interfaceEncoding, bytes wasm)` 0xbde3ff66, `upgrade(..., uint128 feeLimit)` 0xa860929f (programs.md A); wind-down is `setExitRoute(bytes32,bytes32,bytes32,bytes32,bytes)` 0x23330dd5, `deprecate(bytes32,bytes32,uint64)` 0x86ba1a99, `tombstone(bytes32)` 0xa512f1a9, `exitValue(bytes32,bytes32)` 0x4dc50730, `exitValueBounded(bytes32,bytes32,uint128)` 0xade5d9e6 (programs.md A; no `windDown` method); layerxledger 0x101A `issueGrant(bytes payerGrant)`, `revokeGrant(bytes32 grantId, uint64 revocationSequence)`, `payExact(bytes32,bytes32,bytes32,uint128,uint64,bytes32)` (ledger-auth.md A); layerxpay 0x101B `budgetCreate(...)`, `budgetFund(bytes32,uint128)`, `budgetRevoke(bytes32 budgetId, uint64 revocationSequence)` (pay.md A.2); layerxauth 0x101C `grantSession(bytes grant, uint64 expirySequence, bytes32 actionKey)`, `grantCapability(bytes grant)`, `revokeGrant(bytes32 grantId, uint8 reason, uint64 sequence)` (ledger-auth.md layerxauth A); all sponsored through S11 executeSponsored (D14) |
| signing key | agent EVM secp256k1 key bound through 0x1004, loaded by the shared keystore-or-KMS loader selected by env and registered with S11 sponsorship; session keys under LAYERX_AGENT_HUMAN_SESSION_KEY_ROOT (agent/crates/layerx-agentd/src/main.rs:862). Web payer: the payer grant is signed by the embedded-wallet attestor set, threshold 3 of 5 (human/wallet/attestor/internal/tss/eddsa/eddsa.go:21-22), sign kind `lx_grant` (internal/server/sign.go:37, internal/policy/kinds.go:16), ed25519 curve at the gateway client (human/wallet/gateway/src/attestor/client.ts:69), quorum ATTESTOR_QUORUM (env.ts:217); today ProductionWebPayer holds the KMS `AttestorSigner` (agent/crates/layerx-agentd-host/src/payer.rs:45-48) and grant() (:357-391) signs through `sign_payment_grant` (human/crates/layerx-human-kms/src/attestor.rs:594), pay() at :393: that KMS path is RETIRED for this use |
| liveness requirement | agent writes stop; chain unaffected. Chain tolerance: unlimited |
| failure mode and recovery | sequencer down: prepare refused; no answer: outbox Unknown with the exact raw tx kept, resolved by layerxauth idempotency index |
| on-chain evidence | user tx receipts, layerxauth idempotency record |
| monitoring signal | outbox Unknown count; approval queue age |
| reports that define it | agent report; programs report rows 24, 43; web report row 22. Required change: agentd completes session.open and agent.register on the D3 authorisation and 0x1004 paths, replacing the `Operation::AgentRegister \| Operation::SessionOpen` refusal "refused_pending_bootstrap_artifact" (agent/crates/layerx-agentd/src/agent_rpc_dispatch.rs:1355-1359) |

### S27 Embedded wallet gateway
| Field | Value |
|---|---|
| service | Embedded wallet gateway |
| package path | human/wallet/gateway |
| reads | sequencer health, addr 0x1004 `getLayerXDid`, bank 0x1001 `balance` (code-owned: precompiles/addr/addr.go:40, precompiles/bank/bank.go:24; RPC_URLS human/wallet/gateway/src/env.ts:221); agent streams 0x0906 `accrued(uint256)` 0xf52981f4, `getStream(uint256)` 0x894e9a0d (agent-functions.md B; routes/agentPrecompiles.ts:226, :234); scheduler 0x0905 `getJob` 0xbf22c457, `pending(address)` 0x5eebea20 (agent-functions.md B); 0x0906 holds no store of its own, so stream state is read from the 0x101B stream keeper (address-table.md a 0x0906 row); gateway route map human/wallet/gateway/src/precompiles.ts fixed per address-table.md route-map section: staking 0x0800 to 0x1005 (line 26; precompiles/staking/staking.go:49), p256 0x0100 to 0x1011 (line 28; precompiles/p256/p256.go:24), bech32 0x0400 to addr 0x1004 `getPaxAddr`/`getEvmAddr` (line 27; precompiles/addr/addr.go:33-34) |
| writes | user EVM txs and addr 0x1004 `bindLayerX` at provisioning (code-owned, precompiles/addr/addr.go:38); agent streams 0x0906 `open(address,address,uint256,uint64,uint64,uint256)` 0x93e5b08b, `settle(uint256)` 0x8df82800, `close(uint256)` 0x0aebeb4e, `updateRate(uint256,uint256)` 0x405abb41 (agent-functions.md A.1; human/wallet/gateway/src/precompiles.ts:326, routes/agentPrecompiles.ts:159, :189-190, :211), executed through the 0x101B stream keeper, one stream state (pay.md A.7), updateRate has no kernel operation; scheduler 0x0905 `schedule` 0x849ba0a3, `cancel` 0x40e58ee5, `reschedule` 0x08e6f770 (agent-functions.md A.1; routes/agentPrecompiles.ts:73-149); every sponsored write goes through S11 (D14) |
| signing key | user's sharded key via attestor quorum (ATTESTOR_ENDPOINTS env.ts:201, ATTESTOR_QUORUM default 3 env.ts:217); session gates the request; chain sees an ordinary user signature. The gateway sponsor key (SPONSOR_PRIVATE_KEY_FILE env.ts:240, read routes/wallet.ts:37-38) is RETIRED; sponsorship goes through S11 (D14) |
| liveness requirement | in-app custody signing stops; MetaMask self-custody unaffected. Chain tolerance: unlimited |
| failure mode and recovery | attestor quorum loss: no signing (fail closed); sequencer down: send refused |
| on-chain evidence | user tx receipts; addr binding |
| monitoring signal | attestors healthy vs quorum (ATTESTOR_HEALTH_INTERVAL_MS env.ts:218); sign refusal rate |
| reports that define it | human report rows 1, 3, 9, 17, 18, 25; web report rows 22, 24 |

A stream rate change settles the stream at the old rate up to the block-open time and then applies the new rate, through the rate-change activity type added to the kernel stream module (include/layerx/lx_stream.h:26-32).

### S28 Wallet attestors (TSS)
| Field | Value |
|---|---|
| service | Wallet attestors (key shards, threshold signing, calldata decode policy) |
| package path | human/wallet/attestor |
| reads | none on chain (ATTESTOR_RPC_URL config.go:42 for policy decode only) |
| writes | none (returns signature shares) |
| signing key | key shares in ATTESTOR_DATA_DIR (human/wallet/attestor/internal/config/config.go:29, required :147; loader Load :108); threshold 3 (internal/tss/ecdsa/ecdsa.go:26 secp256k1, internal/tss/eddsa/eddsa.go:21-22 ed25519, 3 of 5) for bind and for the agent-host web payer grant, sign kind `lx_grant` (internal/server/sign.go:37, internal/policy/kinds.go:16), one attestor set for all custody; chain verifies the combined signature |
| liveness requirement | below threshold no signature; chain unaffected. Chain tolerance: unlimited |
| failure mode and recovery | quorum loss: fail closed; share restore from backup (ATTESTOR_BACKUP_KEY_FILE config.go:40) |
| on-chain evidence | none |
| monitoring signal | live attestors vs threshold 3 |
| reports that define it | human report rows 17, 20, 21, 22; web report row 22 |

### S29 Human service
| Field | Value |
|---|---|
| service | Human service (web home, moves, agents, approvals, statements, native send, onboarding sponsor) |
| package path | human/crates/layerx-human-service |
| reads | bank 0x1001 `balance`; history from S08 and S31 |
| writes | user EVM transfers via the sequencer (`eth_sendRawTransaction`, sequencer.md b1); onboarding sponsored through S11: the service's own sponsor key field (human/crates/layerx-human-service/src/onboarding/native.rs:28) is RETIRED (D14) |
| signing key | none of its own on Paxeer: user key via S27, gas via S11 |
| liveness requirement | web journeys unavailable; chain unaffected. Chain tolerance: unlimited |
| failure mode and recovery | restart; plans rebuilt from chain state |
| on-chain evidence | user tx receipts |
| monitoring signal | journey error rate |
| reports that define it | human report rows 11, 36-40, 44, 46 |

### S30 Human identity and security providers, KMS
| Field | Value |
|---|---|
| service | Identity provider, security provider, KMS (Unix-socket providers) |
| package path | human/crates/layerx-human-identity-provider, layerx-human-security-provider, layerx-human-kms |
| reads | none |
| writes | user or agent EVM txs signed by the KMS (human/crates/layerx-human-kms/src/evm.rs:178 sign) |
| signing key | KMS-held key bound through 0x1004, signing through the attestor client (human/crates/layerx-human-kms/src/attestor.rs:505 AttestorSigner; config load human/crates/layerx-human-kms/src/config.rs:86); the KMS `sign_payment_grant` path (attestor.rs:594) is RETIRED for the agent-host web payer, which the embedded-wallet attestor set signs (see S26, S28) |
| liveness requirement | no sign-in, step-up or KMS signing; chain unaffected. Chain tolerance: unlimited |
| failure mode and recovery | fail closed |
| on-chain evidence | user or agent tx receipts |
| monitoring signal | socket reachability |
| reports that define it | human report rows 41, 42, 48; agent report |

### S31 Explorer fork and explorer index
| Field | Value |
|---|---|
| service | Explorer fork and explorer index |
| package path | explorer/; human/crates/layerx-explorer-index |
| reads | node RPC blocks and logs (rpc/block.go:207, rpc/filter.go:569); layerxprograms 0x101D logs `ProgramDeployed`, `ProgramGuestEvent`, `ProgramCallOutcome` via `eth_getLogs` (programs.md C; no `ProgramEvents` query); layerxexchange (exchange-launchpad.md C), layerxbridge (layerxbridge.go:49-50), launchpad logs |
| writes | none |
| signing key | none |
| liveness requirement | no history view; balances still correct. Chain tolerance: unlimited |
| failure mode and recovery | re-index from last height |
| on-chain evidence | none |
| monitoring signal | indexed head lag |
| reports that define it | human report rows 10, 13, 15, 45, 72-78; programs report row 18 |

### S32 Wallet app backends
| Field | Value |
|---|---|
| service | Wallet app backends (pool metadata proxy, PNS, points, FX, candles, icons, push, chat) |
| package path | human/apps/wallet/src/app/api, server/ |
| reads | Paxeer swap and token contracts (pool reads); source not in this repository |
| writes | none |
| signing key | none |
| liveness requirement | no quotes or metadata; swaps by direct signing still settle. Chain tolerance: unlimited |
| failure mode and recovery | restart |
| on-chain evidence | none |
| monitoring signal | backend probe |
| reports that define it | human report rows 2, 16 |

### S33 Validator oracle price feeders
| Field | Value |
|---|---|
| service | Validator oracle price feeders (not the perps price source) |
| package path | existing modules/oracle feeders on validators; feeder binary not in this repository |
| reads | oracle module params and vote targets |
| writes | oracle `MsgAggregateExchangeRateVote` (modules/oracle/keeper/msg_server.go:23) per vote period |
| signing key | validator feeder key, delegated by `MsgDelegateFeedConsent` (msg_server.go:70), checked by ValidateFeeder (modules/oracle/keeper/keeper.go:386) |
| liveness requirement | absent votes incur oracle module penalties (modules/oracle/keeper/slash.go); perps unaffected |
| failure mode and recovery | restart; penalties per window |
| on-chain evidence | oracle vote records, miss counters |
| monitoring signal | ValidatorMissedBlocks (tools/ops/monitoring/rules.yml:11) plus miss counter |
| reports that define it | trading report row 24 |

### S34 Monitoring
| Field | Value |
|---|---|
| service | Monitoring |
| package path | tools/ops/monitoring (rules.yml, prometheus.yml, blackbox.yml) |
| reads | metrics endpoints; no chain write |
| writes | none |
| signing key | none |
| liveness requirement | no alerts; no chain effect. Chain tolerance: unlimited |
| failure mode and recovery | restart |
| on-chain evidence | none |
| monitoring signal | new alerts: receipt-vs-block order divergence, sponsor floor, rate age, oracle staleness, funding age, attestor threshold; KernelReadyzDown (rules.yml:56) replaced by sequencer readyz |
| reports that define it | platform report row 47; sequencer report |

### S35 Rollout controller
| Field | Value |
|---|---|
| service | Rollout controller |
| package path | tools/rollout (rollout.py, rollout.sh) |
| reads | none on chain |
| writes | none |
| signing key | no chain key (deploy credentials only) |
| liveness requirement | wave halts; no chain effect. Chain tolerance: unlimited |
| failure mode and recovery | resume the halted wave |
| on-chain evidence | none |
| monitoring signal | wave status |
| reports that define it | platform report row 48 |

### S36 One-shot merge tools
| Field | Value |
|---|---|
| service | One-shot merge tools (account mapping v2, activation bring-up) |
| package path | interop/crates/layerx-migrate (mapping_v2.rs:60); activation upgrade handler node/upgrades_<plan>.go (plan name set at activation rehearsal) |
| reads | addr 0x1004 bindings; no kernel final state to export |
| writes | upgrade handler state at the activation height (new module stores, params, layerxanchor bond return); operator `deploy(bytes32 programId, uint16 abiVersion, uint8 policy, bytes32 authority, bytes32 codeHash, bytes interfaceEncoding, bytes wasm)` 0xbde3ff66 of the three reference guests at activation (programs.md A, F; D13) |
| signing key | operator via upgrade handler or governance; user signatures for binds |
| liveness requirement | run once at activation; failure aborts the activation height, no partial state |
| failure mode and recovery | abort and re-run at a new height |
| on-chain evidence | upgrade plan applied; `ProgramDeployed(bytes32 programId, bytes32 codeHash)` logs with expected code hashes (programs.md C; D13) |
| monitoring signal | activation runbook checklist |
| reports that define it | platform report row 32; kernel report; human report row 63; ADR D13 |

## 2. Summary
One row per service; wave "inferred" where none is named and the row follows its caller re-point rule.

| Service | Key kind | Threshold | Cadence | Chain tolerance | Wave it lands |
|---|---|---|---|---|---|
| S01 Sequencer | consensus privval + ed25519 receipt key (D15) | pinned leader list, primary and standby | every tx at receipt; one batch per block | one round (standby at round 1) | 0 |
| S02 Archive node | none | n/a | continuous | unlimited | 1 |
| S03 Unified router | none | n/a | per request | unlimited | 5 |
| S04 Identity registration | none (user ed25519 + EVM) | n/a | per registration | unlimited | 5 |
| S05 Faucet | secp256k1, shared loader | n/a | per claim | unlimited | 5 (inferred) |
| S06 Receipt authority | none (pinned receipt key) | n/a | per verification | unlimited | 5 |
| S07 Core treasury | secp256k1, shared loader | n/a | per operator request | unlimited | 5 (inferred) |
| S08 Indexer | none | n/a | per block | unlimited | 5 |
| S09 Webhooks | none | n/a | per event | unlimited | 5 (inferred) |
| S10 Registry cache | none | n/a | per block | unlimited | 5 (inferred) |
| S11 Gas station relayer | secp256k1 relayer | n/a | per quote | unlimited; feeder and keeper then unsponsored | 0 |
| S12 SID rate publisher | secp256k1 operator (distinct) | n/a | rate_cadence_seconds < max_rate_age | max_rate_age <= 300 s; feetoken MaxFeeTokenRateAge blocks (gov, set at activation rehearsal) | 0 |
| S13 Interop service | none | n/a | per payment | unlimited | 5 |
| S14 x402 seller | none (pinned receipt key) | n/a | per request | unlimited | 5 (inferred) |
| S15 x-websearch | secp256k1 attestor + submitter + receiver | strict majority of one set for both origins, registration cap 16 (web.md A4, F6); 3 of 4 today | per request | origin 1 TimeoutHeight (default 3600 blocks, set at activation rehearsal); origin 2 no timeout | 4 |
| S16 Bridge relayer | secp256k1 attestor + fee keys, ed25519 Solana fee payer | bridge attestor set threshold | per deposit or BridgeOut | unlimited (nullifier) | 5 |
| S17 Mirror publisher | secp256k1 + Solana via S18 | n/a | per committed segment | unlimited | 7 (retained, re-pointed) |
| S18 Mirror signer | secp256k1 + Solana + bridge keys | n/a | per request | unlimited | 5 (inferred, bridge keys) |
| S19 Price feeder | ed25519 oracle key + its 0x1004-bound EVM sender | market oracle key set | cadence_ms per market | maximum_oracle_staleness_ms | 3 |
| S20 Perps keeper | any EVM key, shared loader | n/a | funding_interval_ms; liquidation at receipt; only driver (H6) | insurance fund then ADL | 4 |
| S21 paxai-worker | ed25519 delegate + EVM sender, shared loader | n/a | per task, heartbeat window | task timeout window (set at activation rehearsal) | 4 |
| S22 ai-artifacts | none | n/a | per artifact | reveal and challenge windows (set at activation rehearsal) | 4 (inferred) |
| S23 AI evaluator | ed25519 evaluator grant key, shared loader | quorum 3 | per epoch | reveal window (set at activation rehearsal) | 1 |
| S24 Stream metering | ed25519 meter authority (external) | stream authority list | per meter period | unlimited (accrual stops) | 2 (inferred) |
| S25 Compute-lease providers | staked provider key (external) | n/a | per move | move limit in heights (set at activation rehearsal) | 4 |
| S26 agentd | secp256k1 agent key, shared loader; web payer grant by the embedded-wallet attestor set | n/a | per agent action | unlimited | 5 |
| S27 Wallet gateway | user key via attestor quorum; no sponsor key | 3 | per user action | unlimited | 5 |
| S28 Wallet attestors | TSS shares secp256k1 + ed25519 | 3 | per signing | unlimited | 5 |
| S29 Human service | none | n/a | per journey | unlimited | 5 |
| S30 Human providers, KMS | KMS key via attestors; web payer path retired | 3 (attestor) | per action | unlimited | 5 |
| S31 Explorer | none | n/a | per block | unlimited | 5 |
| S32 Wallet app backends | none | n/a | per request | unlimited | none (unchanged) |
| S33 Validator oracle feeders | validator feeder key | oracle vote threshold | per vote period | oracle miss window | none (existing, unchanged) |
| S34 Monitoring | none | n/a | scrape interval | unlimited | 0 (inferred) |
| S35 Rollout controller | none | n/a | per wave | unlimited | 0 (inferred) |
| S36 Merge tools | operator (upgrade handler, deploy() 0xbde3ff66) | n/a | once | activation height | 6 |

## 3. Chain method ownership
Every chain method a service calls, with its construct. the construct contracts/sequencer.md rows are by exact signature; code-owned rows are existing constructs outside the construct contracts (README.md (d)).

| Method | Services | Construct |
|---|---|---|
| eth_sendRawTransaction, pax_sequencerReceipt(txHash) | S01, S03, S11, S13, S15, S16, S26, S29 | sequencer.md b1 |
| eth_getTransactionCount, eth_estimateGas, eth_call, eth_getBalance, eth_getCode at pending | S03, S11, S26 | sequencer.md b2, b3 |
| eth_getTransactionReceipt, eth_getLogs, eth_getBlockByNumber, eth_getProof, eth_subscribe | S02, S03, S08, S13, S14, S15, S16, S17, S31 | sequencer.md b2 |
| CometBFT tx_search, abci_query prove=true, header app hash | S02, S08, S17 | sequencer.md b1 node RPC rows |
| MsgSequenceBatch | S01 | sequencer.md c2 |
| draw(bytes32,bytes32,uint128,bytes32,uint64,bytes32,bytes32) | S13, S15 | ledger-auth.md layerxledger A |
| payExact(bytes32,bytes32,bytes32,uint128,uint64,bytes32) | S13, S26 | ledger-auth.md layerxledger A |
| issueGrant(bytes), revokeGrant(bytes32,uint64) | S26 | ledger-auth.md layerxledger A |
| getPayerGrant(bytes32), getAsset(bytes32) | S03, S13, S15, S26 | ledger-auth.md layerxledger B |
| PaymentDrawn, PaymentExact logs | S13, S14, S15 | ledger-auth.md layerxledger C |
| grantSession(bytes,uint64,bytes32), grantCapability(bytes), revokeGrant(bytes32,uint8,uint64) | S26 | ledger-auth.md layerxauth A |
| resultByIdempotency(bytes32,bytes32) | S26 | ledger-auth.md layerxauth B |
| budgetCreate(...), budgetFund(bytes32,uint128), budgetRevoke(bytes32,uint64) | S26 | pay.md A.2 |
| getBudget(bytes32), getStream(bytes32) | S24, S26 | pay.md B |
| streamMeter(bytes32,uint64,bytes32,bytes), StreamMetered log | S24 | pay.md A.3, C |
| 0x0906 open 0x93e5b08b, settle 0x8df82800, close 0x0aebeb4e, accrued 0xf52981f4, getStream 0x894e9a0d | S27 | agent-functions.md A.1, B; executed through pay.md A.7 |
| 0x0906 updateRate(uint256,uint256) 0x405abb41 | S27 | agent-functions.md A.1; stream-keeper rate-change operation (required change) |
| 0x0905 schedule 0x849ba0a3, cancel 0x40e58ee5, reschedule 0x08e6f770, getJob 0xbf22c457, pending 0x5eebea20 | S27 | agent-functions.md A.1, B |
| call 0x54bd2b48, simulateCall 0x3044bd56, deploy 0xbde3ff66, upgrade 0xa860929f | S21, S23, S25, S26, S36 | programs.md A |
| setExitRoute 0x23330dd5, deprecate 0x86ba1a99, tombstone 0xa512f1a9, exitValue 0x4dc50730, exitValueBounded 0xade5d9e6 | S26 | programs.md A |
| getProgram 0xb1ffb3d4, getInterface 0x8efc7240, getStateHead 0x0833650b | S10, S21, S22, S23, S25, S26 | programs.md B |
| ProgramDeployed, ProgramGuestEvent, ProgramCallOutcome logs | S08, S15, S21, S23, S31, S36 | programs.md C |
| fulfil 0xaeb8a7ec, refund 0xd7194ccb, fulfilProgram 0x2d4bf7ad | S15 | web.md A2, A3, A10 |
| getRequest 0xcbbf67ec, getAttestors 0x1a15922d, threshold 0x42cde4e8, fee 0xddca3f43, getParams 0x5e615a6b, getProgramResult 0x2b2e13fc, getKernelNetworkId 0xfc0d26f4 | S15 | web.md B1-B8 |
| MsgRegisterAttestor | S15 | web.md A4 |
| pushOracle(bytes,bytes) | S19 | exchange-launchpad.md P3 |
| fundingTick(bytes32), liquidate(bytes32,bytes32,bytes32), adl(bytes32,bytes32[]) | S20 | exchange-launchpad.md P9, P10, P11 |
| getMarket, getPosition, getMargin (E9, E11, E12), getPerpsBook, getFunding, getOracle, getDeficit | S19, S20 | exchange-launchpad.md A, B |
| getPerpsPositions(bytes32) 0xc0451774 | S20 | exchange-launchpad.md B |
| bindLayerX, getLayerXDid, getEvmAddrByLayerX, layerXBindNonce, getUnifiedAccount | S03, S04, S11, S26, S27, S36 | code-owned addr 0x1004 (precompiles/addr/addr.go:38-43) |
| send, balance, all_balances | S03, S05, S07, S27, S29 | code-owned bank 0x1001 (precompiles/bank/bank.go:22-25) |
| bridgeIn, bridgeOut, getChain, getAttestors, getCap, isPaused, isNullified, BridgeIn/BridgeOut logs | S16, S31 | code-owned layerxbridge (precompiles/layerxbridge/layerxbridge.go:41-50) |
| setRate, currentRate, quoteDigest, sponsoredBatchDigest, executeSponsored, RateUpdated | S11, S12 | code-owned contracts/src/BatchCallAndSponsor.sol:88, :113, :123, :129, :135, :73 |
| MsgAggregateExchangeRateVote, MsgDelegateFeedConsent | S33 | code-owned modules/oracle/keeper/msg_server.go:23, :70 |
| layerxanchor statusOf | S17 | deleted after bond return; LXP_MODULE_DISABLED from the store-delete step (address-table.md c) |
| getPaxAddr, getEvmAddr | S27 | code-owned addr 0x1004 (precompiles/addr/addr.go:33-34) |
| launchpad 0x1017 getProtocolFees(address) (not Config.protocolFeesPending, dropped from getConfig) | none today; any service reading launchpad protocol fees uses it | exchange-launchpad.md B (getConfig, getProtocolFees rows) |
| router px_getReceiptKeys, px_getCommit, px_getValidators, px_register, px_requestFunds | S03 (S04, S05, S17 behind it) | services.md S03; ReceiptKeys source sequencer.md a3, c |
