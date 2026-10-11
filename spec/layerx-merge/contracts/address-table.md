# address-table.md Precompile address and error registry of the merged Paxeer X chain

Version 1.0.2. Source revision 52b672ac4. Binding inputs: decision D6, decision D10, decision D18, decision D20. Conforming file: method names, signatures, selectors, events and error codes are taken from ledger-auth.md, pay.md, exchange-launchpad.md, programs.md, web.md, agent-functions.md and sequencer.md. Change control: changes only by amendment; each amendment raises the version.

## (a) Address table
One row per address the merged chain serves; existing rows read from precompiles/setup.go:67-95 and each package's address constant; "merge plan" means the single named plan of section (c); method signatures and selectors of changed and new addresses are in (a2).

| Address | Name (GetName) | Package path (address constant; registration) | Existing or new | Status after merge | Activation gate | Construct (merged) |
|---|---|---|---|---|---|---|
| 0x0000000000000000000000000000000000001001 | bank | precompiles/bank (bank.go:33; setup.go:68) | existing | extended: `send` gains the LayerX context-hash overload and LayerXReceipt log used by unified-account transfers; signature frozen by ledger-auth.md section 0-B: `send(address,address,string,uint256,bytes32)` 0x2e4c85ac and log `LayerXReceipt(address,address,string,uint256,bytes32)` (selector and topic0 in a2); every other method live unchanged; errors return Error(string) "LXP_<code>: <message>" from the plan height through the opt-in reason path of the dynamic-gas base (rule row "Dynamic-gas precompiles") | versions by tag (precompiles/bank/setup.go:24-32); new `send` overload and the reason path gated on the merge plan | bank (sdk/x/bank) |
| 0x0000000000000000000000000000000000001002 | wasmd | precompiles/wasmd (wasmd.go:30; setup.go:69) | existing | live, unchanged | versions by tag (precompiles/wasmd/setup.go:25-33); none new | wasm |
| 0x0000000000000000000000000000000000001003 | json | precompiles/json (json.go:28; setup.go:70) | existing | live, unchanged | versions by tag (precompiles/json/setup.go:21-29); none new | json (utility) |
| 0x0000000000000000000000000000000000001004 | addr | precompiles/addr (addr.go:57; setup.go:71); keeper modules/evm/keeper/layerx_binding.go | existing | live; DID binding used by every unified-account flow (`bindLayerX`, `unbindLayerX`, `getLayerXDid`, `getEvmAddrByLayerX`, `layerXBindNonce`, `getUnifiedAccount` addr.go:43,434) and the principal resolution sender→DID of 0x101A-0x101D and 0x0901-0x0908; errors return Error(string) "LXP_<code>: <message>" from the plan height through the opt-in reason path of the dynamic-gas base (decision D6); extensions `rotateLayerXKey`, `setRecoveryRoot`, `recoverLayerX` NOT added by this plan (D16 ruled out of merge scope) | versions by tag (precompiles/addr/setup.go:24-32); reason path gated on the merge plan; no method gated by the merge plan | addr (evm module binding keeper) |
| 0x0000000000000000000000000000000000001005 | staking | precompiles/staking (staking.go:49; setup.go:72) | existing | live, unchanged | versions by tag (precompiles/staking/setup.go:21-29); none new | staking |
| 0x0000000000000000000000000000000000001006 | gov | precompiles/gov (gov.go:30; setup.go:73) | existing | live, unchanged | versions by tag (precompiles/gov/setup.go:21-29); none new | gov |
| 0x0000000000000000000000000000000000001007 | distribution | precompiles/distribution (distribution.go:37; setup.go:74) | existing | live, unchanged | versions by tag (precompiles/distribution/setup.go:21-29); none new | distribution |
| 0x0000000000000000000000000000000000001008 | oracle | precompiles/oracle (oracle.go:26; setup.go:75) | existing | live, unchanged (not the perps price source; agent oracle 0x0903 uses the 0x1015 store) | versions by tag (precompiles/oracle/setup.go:23-31); none new | oracle |
| 0x0000000000000000000000000000000000001009 | ibc | precompiles/ibc (ibc.go:30; setup.go:76) | existing | live, unchanged | versions by tag (precompiles/ibc/setup.go:23-31); none new | ibc |
| 0x000000000000000000000000000000000000100A | pointerview | precompiles/pointerview (pointerview.go:24; setup.go:78) | existing | live, unchanged | versions by tag (precompiles/pointerview/setup.go:20-28); none new | pointerview |
| 0x000000000000000000000000000000000000100B | pointer | precompiles/pointer (pointer.go:29; setup.go:77) | existing | live, unchanged | versions by tag (precompiles/pointer/setup.go:23-31); none new | pointer |
| 0x000000000000000000000000000000000000100C | solo | precompiles/solo (solo.go:32; setup.go:80) | existing | live, unchanged | versions by tag (precompiles/solo/setup.go:14-20); none new | solo |
| 0x0000000000000000000000000000000000001011 | p256Verify | precompiles/p256 (p256.go:24; setup.go:79) | existing | live, unchanged | versions by tag (precompiles/p256/setup.go:15-22); none new | p256 (utility) |
| 0x0000000000000000000000000000000000001012 | layerxVerify | precompiles/layerxverify (layerxverify.go:52-53; setup.go:81) | existing | retired executor: every method (`verifyEd25519`, `verifyReceipt`, `verifyReceiptInclusion`, `verifyStateProof`, `verifyDiscoveryProof`) returns Error("LXP_MODULE_DISABLED: layerxVerify retired") at or above the merge plan height; address stays registered (an unregistered address returns empty success) | served from v6.9 (node/upgrades.go:398-411, module layerxanchor); per-method retire gate on the merge plan | none (retired; replaced by abci_query prove=true, eth_getProof, receipt inclusion) |
| 0x0000000000000000000000000000000000001013 | layerxCustody | precompiles/layerxcustody (modules/layerxcustody/types/ids.go:17, layerxcustody.go:83-84; setup.go:82) | existing | retired executor: every method (`deposit`, `depositToken`, `requestWithdrawal`, `finaliseWithdrawal`, `requestForcedExit`, `executeForcedExit`, `exitEligible`, `registerDepositRoot`, `depositRootAuthority`, `depositRootRegistered`, `depositRegistrationDigest`, `getAsset`, `assetByPointer`, `nativeAssetId`, `getDeposit`, `getDepositByIndex`, `depositNonce`, `depositCount`, `getClaim`, `nullifierStatus`) returns LXP_MODULE_DISABLED at or above the merge plan height; no claim drain window (D4 moot) | served from v6.9 (node/upgrades.go:405); governance content gated on v6.11 (modules/layerxcustody/keeper/keeper.go:42, types/governance_activation.go:5); per-method retire gate on the merge plan | none (retired; funding a sub-account is a bank or layerxledger move) |
| 0x0000000000000000000000000000000000001014 | layerxAnchor | precompiles/layerxanchor (layerxanchor.go:65-66; setup.go:83) | existing | retired executor: writes (`submitCheckpoint`, `submitAvailabilityAttestation`, `finalize`, `registerGuarantor`, `increaseBond`, `beginUnbond`, `completeUnbond`, `activateGuarantor`, `submitEquivocation`, `openChallenge`, `resolveChallenge`, `setSequencerAuthorization`) return LXP_MODULE_DISABLED at or above the merge plan height; reads (`latestFinalized`, `checkpoint`, `finalizedStateRoot`, `finalizedReceiptRoot`, `guarantor`, `threshold`, `statusOf`, `checkpointBatch`, `checkpointGuarantors`; names in precompiles/layerxanchor/abi.json) stay served until every posted guarantor bond and open challenge deposit has been returned by the plan handler (D4); then the layerxanchor store is deleted and the reads also return LXP_MODULE_DISABLED; the address stays registered | served from v6.9 (node/upgrades.go:406); per-method retire gate on the merge plan; read retirement and store delete at the store-delete step after the bond return (step name and height set at activation rehearsal) | none (retired; finality = block height and commit) |
| 0x0000000000000000000000000000000000001015 | layerxExchange | precompiles/layerxexchange (modules/layerxexchange/types/ids.go:15, layerxexchange.go:71-72; setup.go:84) | existing | extended (per-method gate), rows exchange-launchpad.md E1-E12, P1-P3, P6-P12, S1-S4, B: `depositMargin`, `depositMarginToken`, `placeOrder`, `cancelOrder` switch from intent recording to engine execution (E1, E2, E4, E5; `placeOrder` and `cancelOrder` change signature); `withdrawMargin(bytes32 market,uint256 amount)` becomes an immediate bank release from the caller DID's D2 margin address to its D2 main address behind a free-margin check (E3); `requestSettlement`, `getIntent` retired with LXP_MODULE_DISABLED (E6, E7); `intentNonce` kept as the nonce input of the D8 chain-derived order and deposit id (E8); `getMarket`, `getOrder`, `getPosition`, `getMargin` lose the batch/witness arguments (E9-E12); new perps methods `createPerpsMarket`, `setPerpsHalt`, `pushOracle`, `openPosition`, `increasePosition`, `closePosition`, `fundingTick`, `liquidate`, `adl`, `registerMargin`; new spot methods `createSpotMarket`, `placeSpotOrder`, `cancelSpotOrder`, `setSpotHalt`; new views `getPerpsBook`, `getFunding`, `getOracle`, `getDeficit`, `getSpotMarket`, `getSpotOrder`, `getSpotBook`, `getPerpsPositions`; new module Params.Paused refuses every state-changing method except the exit paths `cancelOrder`, `cancelSpotOrder`, `closePosition`, `withdrawMargin` with LXP_PAUSED_SCOPE (exchange-launchpad.md F pause row); its committed oracle store is also read and written by agent oracle 0x0903 | served from v6.9 (node/upgrades.go:407); per-method gate on the merge plan (section c) | layerxexchange |
| 0x0000000000000000000000000000000000001016 | layerxBridge | precompiles/layerxbridge (modules/layerxbridge/types/keys.go:27, layerxbridge.go:54-55; setup.go:85) | existing | live, unchanged methods (`bridgeIn`, `bridgeOut`, `getChain`, `getAttestors`, `getCap`, `isPaused`, `isNullified`) | served from v6.9 (node/upgrades.go:408); none new | layerxbridge |
| 0x0000000000000000000000000000000000001017 | launchpad | precompiles/launchpad (modules/launchpad/types/keys.go:26, launchpad.go:84-85; setup.go:86) | existing | live methods L1-L12 of exchange-launchpad.md (`createMarket`, `buy`, `sell`, `setFeeStrategy`, `claimFees`, `executeBurn`, `executeAirdrop`, `claimAirdrop`, `claimAirdropForEpoch`, `executeLpRewards`, `pause`, `unpause`); `buy` and `sell` move the protocol cut to a per-market counter (D8 B, L2, L3); `getConfig()` keeps its selector but its Config tuple drops the last field protocolFeesPending; new view `getProtocolFees(address token)` is its per-market replacement | served from v6.9 (node/upgrades.go:409); `getProtocolFees` and the Config change gated on the merge plan | launchpad |
| 0x0000000000000000000000000000000000001018 | feeToken | precompiles/feetoken (feetoken.go:22-23; setup.go:88-92) | existing | live, unchanged (`setFeeDenom`, `getFeeDenom`, `clearFeeDenom`) | tag v6.7 (precompiles/setup.go:43,88) and v6.9 gate with layerxbridge (node/upgrades.go:410); none new | feetoken (evm fee-token keeper) |
| 0x0000000000000000000000000000000000001019 | xweb | precompiles/xweb (xweb.go:58-59; setup.go:93-95) | existing | extended (web.md A10, B7, B8): `fulfilProgram(bytes observation)` (raw canonical kernel OBSERVATION payload, origin 2), views `getProgramResult(bytes32,uint64)` and `getKernelNetworkId()` added; one attestor set for both origins, registration capped at 16 (A4, D12); MsgPause stops both origins (A8); `refund` serves origin 1 only: origin-2 requests are never keyed in the refund store, so a refund of such an id returns LXP_XWEB_UNKNOWN_REQUEST (D11, web.md A3, F7); other methods live | tag v6.8 (precompiles/setup.go:37,93) and v6.9 gate (node/upgrades.go:411); `fulfilProgram`, `getProgramResult`, `getKernelNetworkId` gated per method on the merge plan (web.md F11) | xweb |
| 0x000000000000000000000000000000000000101A | layerxledger (D10 "lxpay") | precompiles/layerxledger + modules/layerxledger (new) | new | live at plan height (ledger-auth.md precompile 1): `register`, `pause`, `unpause`, `openAccount`, `send(bytes)`, `draw`, `payExact`, `issueGrant`, `revokeGrant(bytes32,uint64)`, `mint`, `burn`; views `getAsset`, `balanceOf`, `totalUnits`, `accountId`, `accountOwner`, `nextSequence`, `getPayerGrant` (drawn totals carried in it, no separate `drawn` view); events `AssetRegistered`, `AssetPaused`, `AssetUnpaused`, `AccountOpened`, `Transfer`, `PayerGrantIssued`, `PayerGrantRevoked`, `PaymentDrawn`, `PaymentExact`; no `withdraw` (kernel WITHDRAW 0x00010009 not registered) and no `receive` method (the typed `draw` is the receive entry point); D10 per-request draw lives here: one `draw` per paid request, one receipt, one `PaymentDrawn` log, USD 0.001, draw sender class min gas price at most 0.067 gwei | whole address served from the merge plan height; every method gated on the merge plan | layerxledger, which is the lxpay construct of D10 (resolution below) |
| 0x000000000000000000000000000000000000101B | layerxpay | precompiles/layerxpay + modules/layerxpay (new) | new | live at plan height: escrow (A.1), budget (A.2), stream (A.3) and service (A.4) method families of pay.md, including `budgetPull` and `budgetDelegateSpend` (exposed, executable once a kernel activity type reaches lx_budget_delegate_spend_execute and lx_budget_pull_execute (src/modules/budget/lx_budget_delegate.c:101,146)); service disputes resolved by the linked escrow's arbiter, which settles the escrow on the ruling (A.6); its stream keeper holds the only stream state and backs agent streams 0x0906 (A.7); write methods outside the exit set refuse with LXP_PAUSED_SCOPE under the governance pause scope (pay.md F pause row) | whole address served from the merge plan height | layerxpay (escrow, budget, stream, service keepers) |
| 0x000000000000000000000000000000000000101C | layerxauth | precompiles/layerxauth + modules/layerxauth (new) | new | live at plan height (ledger-auth.md precompile 2): `grantSession`, `grantSessionSuccessor`, `grantCapability`, `revokeGrant(bytes32,uint8,uint64)`; views `resultByIdempotency(bytes32,bytes32)`, `getGrant(bytes32)`, `isLive`, `sessionKey(bytes32)`, `capabilityScope(bytes32)`, `checkActivity`; events `SessionGranted`, `GrantBody`, `CapabilityGranted`, `GrantRevoked`, `IdentityStateUpdated`; authz `LayerXScopeAuthorization` (D3) | whole address served from the merge plan height | layerxauth |
| 0x000000000000000000000000000000000000101D | layerxprograms | precompiles/layerxprograms + modules/layerxprograms (new) | new | live at plan height (programs.md A, B, C): `deploy`, `upgrade`, `call`, `registryRead`, `transfer`, `registerAccount`, `setExitRoute`, `deprecate`, `tombstone`, `exitValue`, `exitValueBounded`, `sandboxExecute`, `sandboxFund`, `sandboxActivate`, `retireStorage` (the wind-down and sandbox families replace `windDown` and `sandbox`); `upgrade`, `call`, `simulateCall` and `sandboxExecute` take a trailing `uint128 feeLimit`; views `simulateCall`, `getProgram`, `getInterface`, `getProgramOwner`, `getProgramAccount`, `getProgramAccountById`, `getAccountProfile`, `getValueAccount`, `getStateHead`, `getWindDown`, `getExitRoute`, `getSandbox`, `getFeeSchedule`, `getCurrentFeeSchedule`, `getPendingFeeSchedule`, `getMeteringSchedule`, `getCurrentMeteringSchedule`, `getWebRequest`, `getRetirementControl`, `quoteCallFee`; events of programs.md C (`ProgramGuestEvent`, `ProgramCallOutcome` and peers) | whole address served from the merge plan height | layerxprograms |
| 0x0000000000000000000000000000000000000901 | orob resolver (agent function) | precompiles/layerxagent/orob (new) | new | live at plan height (D20 port; agent-functions.md A.1): pure `resolveOffset`, `resolveOffsetBatch`, `toOffset`; route constant human/wallet/gateway/src/precompiles.ts:18 | merge plan | agent functions (D20) |
| 0x0000000000000000000000000000000000000902 | batch clearing (agent function) | precompiles/layerxagent/clearing (new) | new | live at plan height (D20 port; agent-functions.md A.1): pure `computeClearing`, a helper outside the spot book; precompiles.ts:19 | merge plan | agent functions (D20) |
| 0x0000000000000000000000000000000000000903 | oracle aggregator (agent function) | precompiles/layerxagent/oracle (new; distinct from 0x1008 oracle) | new | live at plan height (D20 port; agent-functions.md A.1, B): pure `aggregate`; `submitPrice` writes and view `getValidatorPrice` reads the 0x1015 exchange oracle store, not 0x1008; precompiles.ts:20 | merge plan | agent functions (D20) |
| 0x0000000000000000000000000000000000000904 | pofq scorer (agent function) | precompiles/layerxagent/pofq (new) | new | live at plan height (D20 port; agent-functions.md A.1): pure `scoreFill`, `scoreBatch`, `updateRollingScore`; precompiles.ts:21 | merge plan | agent functions (D20) |
| 0x0000000000000000000000000000000000000905 | scheduler (agent function) | precompiles/layerxagent/scheduler (new) | new | live at plan height (D20 port; agent-functions.md A.1, B): `schedule` (payable), `cancel`, `reschedule`, views `getJob`, `pending`; job store run in receipt order; gateway ABI SCHEDULER_ABI precompiles.ts:50-97 matches | merge plan | agent functions (D20) |
| 0x0000000000000000000000000000000000000906 | payment streams (agent function) | precompiles/layerxagent/streams (new) | new | live at plan height (D20 port; agent-functions.md A.1, B): `open`, `settle`, `close`, `updateRate`, views `accrued`, `getStream`; keeps no store and executes through the 0x101B stream keeper, one stream state (pay.md A.7); gateway ABI STREAMS_ABI precompiles.ts:99-151 matches | merge plan | agent functions (D20) |
| 0x0000000000000000000000000000000000000907 | tee attestor (agent function) | precompiles/layerxagent/teeattestor (new) | new | live at plan height (D20 port; agent-functions.md A.1, B): views `verify`, `verifyAndExpect`, `rootOf`, `rootCount`; roots in gov-set params; signer rules use the embedded-wallet attestor set; gateway ABI TEE_ATTESTOR_ABI precompiles.ts:189-237 matches | merge plan | agent functions (D20) |
| 0x0000000000000000000000000000000000000908 | eip712 helper (agent function) | precompiles/layerxagent/eip712 (new) | new | live at plan height (D20 port; agent-functions.md A.1): pure EIP-712 hashing `hashTypedData`, `domainSeparator`, view `recoverTypedSigner`; gateway ABI EIP712_ABI precompiles.ts:153-187 matches | merge plan | agent functions (D20) |

### (a2) Method selectors of changed and new addresses
One row per signature; selector = first 4 bytes of keccak256 of the canonical signature, each one recomputed with keccak-256 for this amendment and equal to the owning construct contract where that part prints one; rows marked "computed" carry a selector the owning part does not print.

| Address | Canonical signature | Selector | Source (row) |
|---|---|---|---|
| 0x1001 | send(address,address,string,uint256,bytes32) | 0x2e4c85ac | ledger-auth.md 0-B; overload of send(address,address,string,uint256) 0x5c05961b (precompiles/bank/abi.json); computed |
| 0x1001 | log LayerXReceipt(address,address,string,uint256,bytes32) | topic0 0x4b11729a809ec729868d9ba54f3192aa4179fe63668e9f742aa9b25de21de65f | ledger-auth.md 0-B; emitted only by the overload; computed |
| 0x1015 | depositMargin(bytes32) | 0x4498105f | exchange-launchpad.md E1; unchanged (precompiles/layerxexchange/abi.json); computed |
| 0x1015 | depositMarginToken(address,uint256,bytes32) | 0x4576dad4 | E2; unchanged (abi.json); computed |
| 0x1015 | withdrawMargin(bytes32,uint256) | 0xdcea2ab6 | E3; replaces withdrawMargin(bytes32,bytes32,uint256) 0x661e2df4 (abi.json); computed |
| 0x1015 | placeOrder(bytes32,bytes32,uint8,uint256,uint256,uint8) | 0xf9835781 | E4; replaces placeOrder(bytes32,uint8,uint256,uint256,uint8) 0x36633730 (abi.json); computed |
| 0x1015 | cancelOrder(bytes32,bytes32) | 0xa47d9d33 | E5; replaces cancelOrder(bytes32) 0x7489ec23 (abi.json); computed |
| 0x1015 | requestSettlement(bytes32) | 0xe2ab32b3 | E6, retired; computed |
| 0x1015 | getIntent(bytes32) | 0xf13c46aa | E7, retired; computed |
| 0x1015 | intentNonce(address) | 0x634a2320 | E8, kept; computed |
| 0x1015 | getMarket(bytes32) | 0xc3c95c7b | E9; replaces getMarket(bytes32,uint64,bytes) 0x2b2336de (abi.json); computed |
| 0x1015 | getOrder(bytes32,bytes32) | 0x8b99f140 | E10; replaces getOrder(bytes32,bytes32,uint64,bytes) 0x28ae3df5; computed |
| 0x1015 | getPosition(bytes32,bytes32) | 0x68870299 | E11; replaces getPosition(bytes32,bytes32,uint64,bytes) 0x4fad915f; computed |
| 0x1015 | getMargin(address,bytes32) | 0x59c7fa18 | E12; replaces getMargin(bytes32,bytes32,uint64,bytes) 0xba721079; computed |
| 0x1015 | createPerpsMarket((...)) | tuple PerpsMarketParams in kernel order, see exchange-launchpad.md P1 | P1 |
| 0x1015 | setPerpsHalt(bytes32,bool) | 0x141053a4 | P2; computed |
| 0x1015 | pushOracle(bytes,bytes) | 0x0e4d9382 | P3; computed |
| 0x1015 | openPosition(bytes32,bytes32,uint8,uint256,uint256) | 0xb14bfbe6 | P6; computed |
| 0x1015 | increasePosition(bytes32,bytes32,uint256,uint256) | 0xf9f467f3 | P7; computed |
| 0x1015 | closePosition(bytes32,bytes32) | 0x9c49d053 | P8; computed |
| 0x1015 | fundingTick(bytes32) | 0x2b6edb1e | P9; computed |
| 0x1015 | liquidate(bytes32,bytes32,bytes32) | 0xd5b3e4d9 | P10; computed |
| 0x1015 | adl(bytes32,bytes32[]) | 0xc293c109 | P11; computed |
| 0x1015 | registerMargin(bytes32) | 0xf378512d | P12; computed |
| 0x1015 | createSpotMarket(bytes32,bytes32,bytes32,bytes32,uint256,uint256) | 0xdba301bc | S1; computed |
| 0x1015 | placeSpotOrder(bytes32,bytes32,bytes32,uint8,uint8,uint8,uint256,uint256) | 0x91a861ce | S2; computed |
| 0x1015 | cancelSpotOrder(bytes32,bytes32) | 0x7f94da73 | S3; computed |
| 0x1015 | setSpotHalt(bytes32,bool) | 0x9b89e0a4 | S4; computed |
| 0x1015 | getPerpsBook(bytes32) | 0x96f91fc5 | B; computed |
| 0x1015 | getFunding(bytes32) | 0xf7f943e0 | B; computed |
| 0x1015 | getOracle(bytes32) | 0xdafaf94a | B; computed |
| 0x1015 | getDeficit(bytes32) | 0x5fc35ffc | B; computed |
| 0x1015 | getSpotMarket(bytes32) | 0xf60ebf98 | B; computed |
| 0x1015 | getSpotOrder(bytes32,bytes32) | 0x1596fc79 | B; computed |
| 0x1015 | getSpotBook(bytes32) | 0xd4794507 | B; computed |
| 0x1015 | getPerpsPositions(bytes32) | 0xc0451774 | B; computed |
| 0x1017 | getConfig() | 0xc3f909d4 | B: selector unchanged, output tuple loses protocolFeesPending; computed |
| 0x1017 | getProtocolFees(address) | 0xf27dd8ab | B; computed |
| 0x1019 | fulfilProgram(bytes) | 0x2d4bf7ad | web.md A10 |
| 0x1019 | getProgramResult(bytes32,uint64) | 0x2b2e13fc | web.md B7 |
| 0x1019 | getKernelNetworkId() | 0xfc0d26f4 | web.md B8 |
| 0x101A | register(bytes32,bytes32,string,string,uint8,uint128,uint8,bytes) | 0xfd03012a | ledger-auth.md 1 A; computed |
| 0x101A | pause(bytes32) | 0xed56531a | 1 A; computed |
| 0x101A | unpause(bytes32) | 0x2f4dae9f | 1 A; computed |
| 0x101A | openAccount(bytes32) | 0xbdf52150 | 1 A; computed |
| 0x101A | send(bytes) | 0x2bb1ae7c | 1 A; computed |
| 0x101A | draw(bytes32,bytes32,uint128,bytes32,uint64,bytes32,bytes32) | 0x2e89490f | 1 A; computed |
| 0x101A | payExact(bytes32,bytes32,bytes32,uint128,uint64,bytes32) | 0x78ea11db | 1 A; computed |
| 0x101A | issueGrant(bytes) | 0xd0d53480 | 1 A; computed |
| 0x101A | revokeGrant(bytes32,uint64) | 0x58d183a8 | 1 A; computed |
| 0x101A | mint(bytes32,bytes32,uint128) | 0xf04db938 | 1 A; computed |
| 0x101A | burn(bytes32,bytes32,uint128) | 0xda3c63f3 | 1 A; computed |
| 0x101A | getAsset(bytes32) | 0x2cc3ce80 | 1 B; computed |
| 0x101A | balanceOf(bytes32,bytes32) | 0xb1ffc03e | 1 B; computed |
| 0x101A | totalUnits(bytes32) | 0x53a6332b | 1 B; computed |
| 0x101A | accountId(bytes32,bytes32) | 0x3f050cbc | 1 B; computed |
| 0x101A | accountOwner(bytes32) | 0x37955ab2 | 1 B; computed |
| 0x101A | getPayerGrant(bytes32) | 0x423fe4b2 | 1 B; computed |
| 0x101A | nextSequence(bytes32,bytes32) | 0xce2f921f | 1 B; computed |
| 0x101B | budgetPull(bytes32,bytes32,uint128,bytes) | 0xbc2548e7 | pay.md A.2; computed |
| 0x101B | budgetDelegateSpend(bytes32,bytes32,uint128) | 0xf9708fc1 | pay.md A.2; computed |
| 0x101B | every other method and view | as pay.md A.1-A.4, B | pay.md |
| 0x101C | grantSession(bytes,uint64,bytes32) | 0x1107b49e | ledger-auth.md 2 A; computed |
| 0x101C | grantSessionSuccessor(bytes,uint64,bytes32,bytes32,bytes32) | 0x0c49a046 | 2 A; computed |
| 0x101C | grantCapability(bytes) | 0x0cb291d1 | 2 A; computed |
| 0x101C | revokeGrant(bytes32,uint8,uint64) | 0x859615ab | 2 A; computed |
| 0x101C | resultByIdempotency(bytes32,bytes32) | 0x2eda9f2b | 2 B; computed |
| 0x101C | getGrant(bytes32) | 0xe483ac04 | 2 B; computed |
| 0x101C | isLive(bytes32,uint64,uint64) | 0xd4a03b3d | 2 B; computed |
| 0x101C | sessionKey(bytes32) | 0x55eb131c | 2 B; computed |
| 0x101C | capabilityScope(bytes32) | 0xd05b2513 | 2 B; computed |
| 0x101C | checkActivity(bytes32,uint32,bytes32,uint128) | 0xd825990c | 2 B; computed |
| 0x101D | deploy(bytes32,uint16,uint8,bytes32,bytes32,bytes,bytes) | 0xbde3ff66 | programs.md A |
| 0x101D | upgrade(bytes32,uint16,uint8,bytes32,bytes32,bytes,bytes,bytes,uint128) | 0xa860929f | A |
| 0x101D | call(bytes32,uint16,string,bytes,bytes,bytes,uint32,uint64[7],uint128) | 0x54bd2b48 | A |
| 0x101D | registryRead(bytes32,bytes) | 0xfb28c4b1 | A |
| 0x101D | transfer(bytes32,(bytes32,bytes32,bytes32,uint128)[]) | 0x59849fe7 | A |
| 0x101D | registerAccount(bytes32,uint8,bytes32,bytes) | 0xfc786007 | A |
| 0x101D | setExitRoute(bytes32,bytes32,bytes32,bytes32,bytes) | 0x23330dd5 | A |
| 0x101D | deprecate(bytes32,bytes32,uint64) | 0x86ba1a99 | A |
| 0x101D | tombstone(bytes32) | 0xa512f1a9 | A |
| 0x101D | exitValue(bytes32,bytes32) | 0x4dc50730 | A |
| 0x101D | exitValueBounded(bytes32,bytes32,uint128) | 0xade5d9e6 | A |
| 0x101D | sandboxExecute(bytes32,uint64,bytes32,bytes32,bytes32,bytes32,uint32,uint64[7],bytes,uint128) | 0xff04aa78 | A |
| 0x101D | sandboxFund(bytes32,bytes32,bytes32,bytes32,bytes32,uint128,uint64,uint32,uint64[7],bytes,bytes32,(bytes32,bytes32,bytes32,uint128)[]) | 0xf11e5752 | A |
| 0x101D | sandboxActivate(bytes32,bytes32,uint64) | 0x64e1674e | A |
| 0x101D | retireStorage(bytes) | 0x1a5fe5ff | A |
| 0x101D | simulateCall(bytes32,uint16,string,bytes,bytes,bytes,uint32,uint64[7],uint128) | 0x3044bd56 | B |
| 0x101D | getProgram(bytes32), getInterface(bytes32), getProgramOwner(bytes32), getProgramAccount(bytes32,bytes32,bytes), getProgramAccountById(bytes32), getAccountProfile(bytes32), getValueAccount(bytes32,bytes32), getStateHead(bytes32), getWindDown(bytes32), getExitRoute(bytes32,bytes32), getSandbox(bytes32,uint8), getFeeSchedule(uint32), getCurrentFeeSchedule(), getPendingFeeSchedule(), getMeteringSchedule(uint32), getCurrentMeteringSchedule(), getWebRequest(bytes32,uint64), getRetirementControl() | 0xb1ffb3d4, 0x8efc7240, 0x5d6be786, 0x9677b10c, 0x7a599bd0, 0x0f2d0496, 0x24460e23, 0x0833650b, 0x98e4e41d, 0x3ee6f18d, 0xb8b4f8bc, 0x8d8bfb15, 0xbbe2e42d, 0xe8b5cd95, 0xa842e85b, 0x543c7a2f, 0x61bc2ca4, 0xdcee2e41 (in order) | B |
| 0x101D | quoteCallFee(uint64[7]) | 0xd67f9ffa | B; computed |
| 0x0901 | resolveOffset(int256,int16) | 0x454ad3a1 | agent-functions.md A.1 |
| 0x0901 | resolveOffsetBatch(int256,int16[]) | 0x3df597e3 | A.1 |
| 0x0901 | toOffset(int256,int256) | 0x3f779aef | A.1 |
| 0x0902 | computeClearing(int256,(int16,uint128)[],(int16,uint128)[]) | 0x8397e9f8 | A.1 |
| 0x0903 | aggregate((int256,uint256,uint256)[]) | 0x81097a95 | A.1 |
| 0x0903 | submitPrice(bytes32,int256,uint256) | 0xdc9c2875 | A.1 |
| 0x0903 | getValidatorPrice(bytes32) | 0x99812615 | B |
| 0x0904 | scoreFill(int256,int256) | 0xe087a6ba | A.1 |
| 0x0904 | scoreBatch((int256,int256,uint128)[]) | 0x1b1fba48 | A.1 |
| 0x0904 | updateRollingScore(uint256,uint256,uint256,uint256,uint16) | 0xa8f32b0e | A.1 |
| 0x0905 | schedule(address,bytes,uint64,uint64) | 0x849ba0a3 | A.1 |
| 0x0905 | cancel(uint256) | 0x40e58ee5 | A.1 |
| 0x0905 | reschedule(uint256,uint64) | 0x08e6f770 | A.1 |
| 0x0905 | getJob(uint256) | 0xbf22c457 | B |
| 0x0905 | pending(address) | 0x5eebea20 | B |
| 0x0906 | open(address,address,uint256,uint64,uint64,uint256) | 0x93e5b08b | A.1 |
| 0x0906 | settle(uint256) | 0x8df82800 | A.1 |
| 0x0906 | close(uint256) | 0x0aebeb4e | A.1 |
| 0x0906 | updateRate(uint256,uint256) | 0x405abb41 | A.1 |
| 0x0906 | accrued(uint256) | 0xf52981f4 | B |
| 0x0906 | getStream(uint256) | 0x894e9a0d | B |
| 0x0907 | verify(uint8,bytes) | 0x5fa55ca7 | A.1 |
| 0x0907 | verifyAndExpect(uint8,bytes,bytes32) | 0x71ded4b3 | A.1 |
| 0x0907 | rootOf(uint8,uint256) | 0x01ec24b8 | B |
| 0x0907 | rootCount(uint8) | 0x26f02502 | B |
| 0x0908 | hashTypedData(bytes32,bytes32) | 0xd61c3c34 | A.1 |
| 0x0908 | domainSeparator(string,string,uint256,address) | 0xe6d341cd | A.1 |
| 0x0908 | recoverTypedSigner(bytes32,bytes32,bytes) | 0xac580b5c | A.1 |

### 0x101A resolution (D10 lxpay versus layerxledger)
| Item | Value |
|---|---|
| Ruled layout | 0x101A = layerxledger; "lxpay" in decision D10 is the name of the same construct. 0x101B = layerxpay (escrow, budget, stream, service), which is not lxpay. 0x101C = layerxauth. 0x101D = layerxprograms. |
| Why | the kernel executes grant issue, grant draw and the drawn total inside the ledger and asset module over one balance store: src/ledger/lxp_grant_store.c:72 (`lxp_verify_payer_grant`), :123 (`lxp_grant_draw_record`), src/ledger/lxp_receive.c:413-418 (receive resolves the grant and refuses LXP_ERR_NO_PAYER_GRANT), src/modules/asset/lx_asset_execution.h:46 (grant keys); a separate lxpay address would need a cross-precompile call into the ledger for every draw. |
| D10 behaviour kept | address 0x101A (D10 follow-up "reserve 0x101A for lxpay"); per-request `draw(bytes32 from,bytes32 assetId,uint128 amount,bytes32 grantId,uint64 receiverSequence,bytes32 idempotencyKey,bytes32 contextHash)`, the typed form of the kernel RECEIVE fields (include/layerx/lxp_ledger.h:216-227; ledger-auth.md 1 A); one draw yields one receipt and one precompile-emitted `PaymentDrawn(bytes32 indexed grantId,bytes32 indexed from,bytes32 indexed to,bytes32 assetId,uint128 amount,bytes32 idempotencyKey,bytes32 contextHash)` log (no kernel event constant) at USD 0.001; draw sender class min gas price at most 0.067 gwei (evm params, owned by the fee contract sequencer.md f); x402 suite unchanged. |
| payExact | `payExact(bytes32 from,bytes32 to,bytes32 assetId,uint128 amount,uint64 expiresAt,bytes32 paymentReference)`, a typed send whose paymentReference is the send idempotency key, with log `PaymentExact(bytes32 indexed from,bytes32 indexed to,bytes32 indexed paymentReference,bytes32 assetId,uint128 amount)` (ledger-auth.md 1 A, C) |
| ABI naming | precompile name "layerxledger"; an `ILxPay` interface file is published as the ABI subset `issueGrant`, `revokeGrant`, `draw`, `payExact` of layerxledger at 0x101A for x402 clients; no chain change (ledger-auth.md D-6). |
| Not adopted | Option of a separate modules/lxpay at 0x101A beside layerxledger elsewhere: would re-open the collision. |

### Route-map entries that are not chain addresses
| Gateway key | Constant (human/wallet/gateway/src/precompiles.ts) | Registered address on the chain | Fix |
|---|---|---|---|
| staking | 0x0000000000000000000000000000000000000800 (line 26) | 0x1005 (precompiles/staking/staking.go:49) | point the constant at 0x1005 |
| bech32 | 0x0000000000000000000000000000000000000400 (line 27) | none registered | map the key to addr 0x1004 `getPaxAddr`/`getEvmAddr` |
| p256 | 0x0000000000000000000000000000000000000100 (line 28) | 0x1011 (precompiles/p256/p256.go:24) | point the constant at 0x1011 |

### Code-owned ABIs (no construct contract)
These five constructs have no owning construct contract in 1.0.1; their ABI is owned by the code at 52b672ac4, and clients.md and services.md cite this table for them. A change to any of them needs a construct contract first.

| Construct | Address | ABI file (path:line) | Solidity interface (path:line) | Code construct (path:line) |
|---|---|---|---|---|
| addr | 0x0000000000000000000000000000000000001004 | precompiles/addr/abi.json, embedded at precompiles/addr/addr.go:62, loaded :88 | precompiles/addr/Addr.sol:4 (address constant), :10 `IAddr` | precompiles/addr/addr.go:57 (AddrAddress), :134 Execute |
| bank | 0x0000000000000000000000000000000000001001 | precompiles/bank/abi.json, embedded at precompiles/bank/bank.go:38 | precompiles/bank/Bank.sol:4, :10 `IBank` | precompiles/bank/bank.go:33 (BankAddress), :106 Execute; the LayerX `send` overload and LayerXReceipt log are owned by ledger-auth.md section 0-B |
| feetoken | 0x0000000000000000000000000000000000001018 | precompiles/feetoken/abi.json, embedded at precompiles/feetoken/feetoken.go:29 | precompiles/feetoken/FeeToken.sol:4, :7 `IFeeToken` | precompiles/feetoken/feetoken.go:22-23 |
| layerxbridge | 0x0000000000000000000000000000000000001016 | precompiles/layerxbridge/abi.json, embedded at precompiles/layerxbridge/layerxbridge.go:63 | precompiles/layerxbridge/LayerXBridge.sol:4, :30 `ILayerXBridge` | precompiles/layerxbridge/layerxbridge.go:54-55; modules/layerxbridge/types/keys.go:27 |
| BatchCallAndSponsor | no fixed address: EIP-7702 delegation target contract, not a precompile; the gas station requires the authorization delegate to equal the configured paymaster address (interop/crates/layerx-gas-station/src/tx.rs:314) | no tracked ABI json; the ABI is the source contracts/src/BatchCallAndSponsor.sol | contracts/src/BatchCallAndSponsor.sol:16 `contract BatchCallAndSponsor`, :20 `nonce()`, :135 `executeSponsored`, :187 `execute` | contracts/src/BatchCallAndSponsor.sol:16 |


## (b) Error code registry (D6)
Encoding rules first, then every code; kernel codes are the lxp_result enum (include/layerx/lxp_result.h:29-157), Go codes are the registered module errors and precompile error sites.

| Rule | Value | Source |
|---|---|---|
| Wire form | ABI Error(string), selector 0x08c379a0, text "LXP_<code>: <message>" optionally followed by "; <detail>" | precompiles/common/precompiles.go:59-65 RevertReason |
| Enabling | the precompile is built with WithRevertReasons (precompiles/common/precompiles.go:53); today used by layerxcustody, layerxanchor, layerxexchange, layerxbridge, launchpad, feetoken, xweb; every new precompile (0x101A-0x101D, 0x0901-0x0908) uses it | precompiles/common/precompiles.go:82-91 |
| Dynamic-gas precompiles | addr 0x1004 and bank 0x1001 are DynamicGasPrecompile, whose error path drops the reason (precompiles/common/precompiles.go:177-184); from the merge plan height an opt-in reason path in the dynamic-gas base returns Error(string) "LXP_<code>: <message>" for these two; below it the bytes stay as today (decision D6) | precompiles/common/precompiles.go:177-184; precompiles/addr/addr.go:123; precompiles/bank/bank.go:98 |
| Governance pause | modules/layerxgov is a Go proposal router (modules/layerxgov/handler.go:11-40 NewProposalHandler routes to module Msgs such as xweb MsgPause; types/proposal.go:37-38), not a precompile and without a pause flag; kernel pause scopes live in lxp_gov_emergency_state (include/layerx/lxp_governance.h:114-128, pauses[LXP_MAX_GOV_PAUSES] and module_enabled[]) checked by lxp_pause_scope_check (src/modules/governance/lxp_emergency.c:139-160) inside every kernel-backed precompile; Go-side pauses are per-module keys (xweb types.PausedKey modules/xweb/keeper/keeper.go:105-113; new layerxexchange Params.Paused) | as cited |
| xweb origin rule | Go-origin xweb errors keep their LXP_XWEB_<NAME> codes; kernel-origin errors of origin-2 requests carry kernel codes | modules/xweb/types/errors.go:6-29; web.md F12 |
| Height | the LXP_ text applies only at or above the merge plan height; below it every existing precompile returns byte-identical revert data (replay parity, must 1) | gate pattern modules/layerxcustody/keeper/keeper.go:42 |
| Kernel code name | enum name with the LXP_ERR_ (or LXP_) prefix replaced by LXP_; the numeric kernel value travels in the origin column, not in the string | include/layerx/lxp_result.h:29-157; src/protocol/lxp_result.c:3-12 lxp_result_name |
| Unknown kernel value | LXP_UNKNOWN, as lxp_result_name returns "LXP_ERR_UNKNOWN" for an unlisted value | src/protocol/lxp_result.c:10 |
| Go code name | LXP_<MODULE>_<registered error name in upper snake case>; message = the registered message text | per row |
| Fatal kernel values | never a revert: the cgo host halts the node (value <= -1000) | src/protocol/lxp_result.c:55-58 |
| Single table | one shared mapping table plus one focused test asserting the revert string per code (decision D6) | decision D6 |
| Retryable source | the Retryable column of this table is the single source of retriability; agent/crates/layerx-types/build.rs generates the Rust mirror from it and scripts/ci/result-codes-gen.py generates the TypeScript and Python SDK decoders, with a --check run in CI; a header code without a row fails the build | agent/crates/layerx-types/build.rs; scripts/ci/result-codes-gen.py |

| Code | Message | Emitting construct | Kernel or Go origin (file:line) | Retryable |
|---|---|---|---|---|
| LXP_TRUNCATED | truncated | layerxledger 0x101A; layerxpay 0x101B; layerxprograms 0x101D; shared cgo host (all kernel-backed precompiles) | LXP_ERR_TRUNCATED = -1, include/layerx/lxp_result.h:31; emitted src/modules/programs/registration.c:160 | no |
| LXP_TRAILING_BYTES | trailing bytes | layerxexchange 0x1015; layerxpay 0x101B; layerxprograms 0x101D; shared cgo host (all kernel-backed precompiles) | LXP_ERR_TRAILING_BYTES = -2, include/layerx/lxp_result.h:32; emitted src/modules/stream/lx_stream_record.c:159 | no |
| LXP_NON_CANONICAL | non canonical | addr 0x1004 (DID identity); layerxauth 0x101C; layerxexchange 0x1015; kernel governance checks inside every kernel-backed precompile; layerxledger 0x101A; layerxpay 0x101B; layerxprograms 0x101D; shared cgo host (all kernel-backed precompiles); xweb 0x1019 (origin 2) | LXP_ERR_NON_CANONICAL = -3, include/layerx/lxp_result.h:33; emitted src/modules/asset/lx_asset_transfer.c:11 | no |
| LXP_UNSORTED_SEQUENCE | unsorted sequence | layerxexchange 0x1015; kernel governance checks inside every kernel-backed precompile; layerxprograms 0x101D; shared cgo host (all kernel-backed precompiles); xweb 0x1019 (origin 2) | LXP_ERR_UNSORTED_SEQUENCE = -4, include/layerx/lxp_result.h:34; emitted src/protocol/lxp_genesis.c:435 | no |
| LXP_LENGTH_LIMIT | length limit | layerxexchange 0x1015; kernel governance checks inside every kernel-backed precompile; layerxledger 0x101A; layerxpay 0x101B; layerxprograms 0x101D; shared cgo host (all kernel-backed precompiles); xweb 0x1019 (origin 2) | LXP_ERR_LENGTH_LIMIT = -5, include/layerx/lxp_result.h:35; emitted src/modules/asset/lx_asset_reserve.c:99 | no |
| LXP_INVALID_TAG | invalid tag | layerxprograms 0x101D; shared cgo host (all kernel-backed precompiles) | LXP_ERR_INVALID_TAG = -6, include/layerx/lxp_result.h:36; emitted src/modules/programs/occupancy_evidence.c:617 | no |
| LXP_UNKNOWN_FIELD | unknown field | layerxauth 0x101C; layerxexchange 0x1015; kernel governance checks inside every kernel-backed precompile; layerxledger 0x101A; layerxpay 0x101B; layerxprograms 0x101D; shared cgo host (all kernel-backed precompiles); xweb 0x1019 (origin 2) | LXP_ERR_UNKNOWN_FIELD = -7, include/layerx/lxp_result.h:37; emitted src/modules/asset/lx_asset_custody.c:232 | no |
| LXP_DUPLICATE_ENTRY | duplicate entry | layerxpay 0x101B; layerxprograms 0x101D; shared cgo host (all kernel-backed precompiles); xweb 0x1019 (origin 2) | LXP_ERR_DUPLICATE_ENTRY = -8, include/layerx/lxp_result.h:38; emitted src/modules/programs/blob_lifecycle.c:300 | no |
| LXP_WRONG_NETWORK | wrong network | layerxprograms 0x101D; shared cgo host (all kernel-backed precompiles); xweb 0x1019 (origin 2) | LXP_ERR_WRONG_NETWORK = -100, include/layerx/lxp_result.h:39; emitted src/modules/web/lx_web_intake.c:533 | no |
| LXP_VERSION_UNSUPPORTED | version unsupported | layerxauth 0x101C; layerxexchange 0x1015; kernel governance checks inside every kernel-backed precompile; layerxpay 0x101B; layerxprograms 0x101D; shared cgo host (all kernel-backed precompiles) | LXP_ERR_VERSION_UNSUPPORTED = -101, include/layerx/lxp_result.h:40; emitted src/modules/programs/occupancy_evidence.c:222 | no |
| LXP_UNKNOWN_MODULE | unknown module | layerxauth 0x101C; shared cgo host (all kernel-backed precompiles) | LXP_ERR_UNKNOWN_MODULE = -102, include/layerx/lxp_result.h:41; emitted src/protocol/lxp_genesis.c:90 | no |
| LXP_MODULE_DISABLED | module disabled | retired executors at 0x1012, 0x1013, 0x1014 and retired 0x1015 methods (section c); layerxauth 0x101C; layerxpay 0x101B; layerxprograms 0x101D; shared cgo host (all kernel-backed precompiles); xweb 0x1019 (origin 2) | LXP_ERR_MODULE_DISABLED = -103, include/layerx/lxp_result.h:42; emitted src/modules/escrow/lx_escrow_timeout.c:312 | no |
| LXP_PAYLOAD_HASH_MISMATCH | payload hash mismatch | layerxprograms 0x101D; shared cgo host (all kernel-backed precompiles) | LXP_ERR_PAYLOAD_HASH_MISMATCH = -104, include/layerx/lxp_result.h:43; emitted src/modules/programs/deploy.c:226 | no |
| LXP_MALFORMED_ENVELOPE | malformed envelope | layerxauth 0x101C; shared cgo host (all kernel-backed precompiles) | LXP_ERR_MALFORMED_ENVELOPE = -105, include/layerx/lxp_result.h:44; emitted src/state/lxp_authority.c:558 | no |
| LXP_UNKNOWN_ACTIVITY | unknown activity | layerxexchange 0x1015; layerxledger 0x101A; layerxpay 0x101B; layerxprograms 0x101D; shared cgo host (all kernel-backed precompiles); xweb 0x1019 (origin 2) | LXP_ERR_UNKNOWN_ACTIVITY = -106, include/layerx/lxp_result.h:45; emitted src/modules/asset/lx_asset_registry.c:345 | no |
| LXP_MALFORMED_SEND | malformed send | layerxledger 0x101A | LXP_ERR_MALFORMED_SEND = -107, include/layerx/lxp_result.h:46; emitted src/ledger/lxp_send.c:189 | no |
| LXP_MALFORMED_RECEIVE | malformed receive | layerxledger 0x101A | LXP_ERR_MALFORMED_RECEIVE = -108, include/layerx/lxp_result.h:47; emitted src/ledger/lxp_receive.c:193 | no |
| LXP_UNKNOWN_DID | unknown did | addr 0x1004 (DID identity); layerxauth 0x101C; shared cgo host (all kernel-backed precompiles) | LXP_ERR_UNKNOWN_DID = -200, include/layerx/lxp_result.h:48; emitted src/state/lxp_authority.c:1151 | no |
| LXP_BAD_SIGNATURE | bad signature | addr 0x1004 (DID identity); layerxauth 0x101C; kernel governance checks inside every kernel-backed precompile; layerxledger 0x101A; shared cgo host (all kernel-backed precompiles); xweb 0x1019 (origin 2); layerxpay 0x101B (pay.md) | LXP_ERR_BAD_SIGNATURE = -201, include/layerx/lxp_result.h:49; emitted src/modules/governance/lxp_governance.c:565 | no |
| LXP_AUTH_EXPIRED | auth expired | layerxauth 0x101C | LXP_ERR_AUTH_EXPIRED = -202, include/layerx/lxp_result.h:50; emitted src/state/lxp_authority.c:1129 | no |
| LXP_AUTH_REVOKED | auth revoked | addr 0x1004 (DID identity); layerxauth 0x101C; kernel governance checks inside every kernel-backed precompile | LXP_ERR_AUTH_REVOKED = -203, include/layerx/lxp_result.h:51; emitted src/modules/governance/lxp_governance.c:397 | no |
| LXP_AUTH_SCOPE | auth scope | addr 0x1004 (DID identity); layerxauth 0x101C; kernel governance checks inside every kernel-backed precompile; layerxledger 0x101A; layerxprograms 0x101D; shared cgo host (all kernel-backed precompiles) | LXP_ERR_AUTH_SCOPE = -204, include/layerx/lxp_result.h:52; emitted src/modules/programs/fees.c:372 | no |
| LXP_AUTH_ALLOWANCE | auth allowance | layerxauth 0x101C; layerxledger 0x101A; shared cgo host (all kernel-backed precompiles); xweb 0x1019 (origin 2) | LXP_ERR_AUTH_ALLOWANCE = -205, include/layerx/lxp_result.h:53; emitted src/state/lxp_authority.c:929 | no |
| LXP_IDENTITY_FROZEN | identity frozen | addr 0x1004 (DID identity) | LXP_ERR_IDENTITY_FROZEN = -206, include/layerx/lxp_result.h:54; emitted src/state/lxp_identity.c:106 | yes |
| LXP_UNAUTHORIZED_DEBIT | unauthorized debit | layerxauth 0x101C; layerxexchange 0x1015; layerxledger 0x101A; layerxpay 0x101B; layerxprograms 0x101D; shared cgo host (all kernel-backed precompiles); xweb 0x1019 (origin 2) | LXP_ERR_UNAUTHORIZED_DEBIT = -207, include/layerx/lxp_result.h:55; emitted src/modules/budget/lx_budget_close.c:14 | no |
| LXP_UNKNOWN_ACCOUNT_NAMESPACE | unknown account namespace | addr 0x1004 (DID identity); layerxexchange 0x1015; kernel governance checks inside every kernel-backed precompile; layerxledger 0x101A; layerxpay 0x101B; layerxprograms 0x101D; shared cgo host (all kernel-backed precompiles); xweb 0x1019 (origin 2) | LXP_ERR_UNKNOWN_ACCOUNT_NAMESPACE = -208, include/layerx/lxp_result.h:56; emitted src/modules/asset/lx_asset_state.c:45 | no |
| LXP_MALFORMED_GRANT | malformed grant | layerxauth 0x101C; layerxledger 0x101A; layerxpay 0x101B (pay.md) | LXP_ERR_MALFORMED_GRANT = -209, include/layerx/lxp_result.h:57; emitted src/ledger/lxp_grant_store.c:37 | no |
| LXP_UNKNOWN_AUTHORITY_KIND | unknown authority kind | layerxauth 0x101C; kernel governance checks inside every kernel-backed precompile; layerxledger 0x101A; shared cgo host (all kernel-backed precompiles) | LXP_ERR_UNKNOWN_AUTHORITY_KIND = -210, include/layerx/lxp_result.h:58; emitted src/modules/governance/lxp_governance.c:396 | no |
| LXP_GRANT_EXHAUSTED | grant exhausted | layerxauth 0x101C; layerxledger 0x101A; shared cgo host (all kernel-backed precompiles); xweb 0x1019 (origin 2) | LXP_ERR_GRANT_EXHAUSTED = -211, include/layerx/lxp_result.h:59; emitted src/state/lxp_authority.c:818 | no |
| LXP_STALE_REVOCATION | stale revocation | layerxauth 0x101C; layerxledger 0x101A; layerxpay 0x101B | LXP_ERR_STALE_REVOCATION = -212, include/layerx/lxp_result.h:60; emitted src/modules/budget/lx_budget_module.c:515 | no |
| LXP_CONTEXT_MISMATCH | context mismatch | layerxauth 0x101C; kernel governance checks inside every kernel-backed precompile; layerxledger 0x101A; layerxpay 0x101B; layerxprograms 0x101D; shared cgo host (all kernel-backed precompiles); xweb 0x1019 (origin 2) | LXP_ERR_CONTEXT_MISMATCH = -213, include/layerx/lxp_result.h:61; emitted src/modules/escrow/lx_escrow_dispute.c:76 | no |
| LXP_NO_PAYER_GRANT | no payer grant | layerxledger 0x101A | LXP_ERR_NO_PAYER_GRANT = -214, include/layerx/lxp_result.h:62; emitted src/modules/asset/lx_asset_transfer.c:67 | no |
| LXP_GRANT_SCOPE_VIOLATION | grant scope violation | layerxledger 0x101A; layerxpay 0x101B; shared cgo host (all kernel-backed precompiles) | LXP_ERR_GRANT_SCOPE_VIOLATION = -215, include/layerx/lxp_result.h:63; emitted src/modules/asset/lx_asset_transfer.c:75 | no |
| LXP_PURPOSE_MISMATCH | purpose mismatch | layerxledger 0x101A | LXP_ERR_PURPOSE_MISMATCH = -216, include/layerx/lxp_result.h:64; emitted src/ledger/lxp_receive.c:379 | no |
| LXP_INVOICE_ALREADY_SETTLED | invoice already settled | layerxledger 0x101A | LXP_ERR_INVOICE_ALREADY_SETTLED = -217, include/layerx/lxp_result.h:65; emitted src/ledger/lxp_receive.c:375 | no |
| LXP_GRANT_EXPIRED | grant expired | layerxledger 0x101A | LXP_ERR_GRANT_EXPIRED = -218, include/layerx/lxp_result.h:66; emitted src/ledger/lxp_receive.c:422 | no |
| LXP_GRANT_REVOKED | grant revoked | layerxledger 0x101A | LXP_ERR_GRANT_REVOKED = -219, include/layerx/lxp_result.h:67; emitted src/ledger/lxp_receive.c:420 | no |
| LXP_AUTH_THRESHOLD_UNMET | auth threshold unmet | layerxauth 0x101C | LXP_ERR_AUTH_THRESHOLD_UNMET = -220, include/layerx/lxp_result.h:68; emitted src/state/lxp_authority.c:391 | no |
| LXP_AUTH_DUPLICATE_SIGNER | auth duplicate signer | layerxauth 0x101C; xweb 0x1019 (origin 2) | LXP_ERR_AUTH_DUPLICATE_SIGNER = -221, include/layerx/lxp_result.h:69; emitted src/modules/web/lx_web_intake.c:234 | no |
| LXP_AUTH_NOT_MATURE | auth not mature | layerxauth 0x101C | LXP_ERR_AUTH_NOT_MATURE = -222, include/layerx/lxp_result.h:70; emitted src/state/lxp_authority.c:1136 | yes |
| LXP_SEQUENCE_GAP | sequence gap | addr 0x1004 (DID identity); layerxledger 0x101A; layerxpay 0x101B; layerxprograms 0x101D; shared cgo host (all kernel-backed precompiles); xweb 0x1019 (origin 2) | LXP_ERR_SEQUENCE_GAP = -300, include/layerx/lxp_result.h:71; emitted src/modules/budget/lx_budget_module.c:292 | no |
| LXP_SEQUENCE_REUSED | sequence reused | addr 0x1004 (DID identity); layerxauth 0x101C; layerxexchange 0x1015; kernel governance checks inside every kernel-backed precompile; layerxledger 0x101A; layerxpay 0x101B; layerxprograms 0x101D; shared cgo host (all kernel-backed precompiles); xweb 0x1019 (origin 2) | LXP_ERR_SEQUENCE_REUSED = -301, include/layerx/lxp_result.h:72; emitted src/modules/asset/lx_asset_custody.c:102 | no |
| LXP_IDEMPOTENT_REPLAY | idempotent replay | layerxledger 0x101A; layerxpay 0x101B; layerxprograms 0x101D; shared cgo host (all kernel-backed precompiles); xweb 0x1019 (origin 2) | LXP_ERR_IDEMPOTENT_REPLAY = -302, include/layerx/lxp_result.h:73; emitted src/modules/escrow/lx_escrow_state.c:483 | no |
| LXP_EXPIRED | expired | kernel governance checks inside every kernel-backed precompile; layerxledger 0x101A; layerxpay 0x101B; xweb 0x1019 (origin 2) | LXP_ERR_EXPIRED = -303, include/layerx/lxp_result.h:74; emitted src/modules/budget/lx_budget_spend.c:63 | no |
| LXP_NOT_YET_VALID | not yet valid | addr 0x1004 (DID identity); layerxauth 0x101C; layerxexchange 0x1015; kernel governance checks inside every kernel-backed precompile; layerxpay 0x101B; layerxprograms 0x101D | LXP_ERR_NOT_YET_VALID = -304, include/layerx/lxp_result.h:75; emitted src/modules/programs/blob_lifecycle.c:465 | yes |
| LXP_SEQUENCE_MISMATCH | sequence mismatch | kernel governance checks inside every kernel-backed precompile; layerxledger 0x101A; layerxprograms 0x101D; shared cgo host (all kernel-backed precompiles) | LXP_ERR_SEQUENCE_MISMATCH = -305, include/layerx/lxp_result.h:76; emitted src/state/lxp_snapshot.c:496 | no |
| LXP_CONDITION_UNMET | condition unmet | layerxledger 0x101A; layerxprograms 0x101D; shared cgo host (all kernel-backed precompiles) | LXP_ERR_CONDITION_UNMET = -306, include/layerx/lxp_result.h:77; emitted src/ledger/lxp_send.c:271 | no |
| LXP_SEQUENCE_EXHAUSTED | sequence exhausted | layerxledger 0x101A; shared cgo host (all kernel-backed precompiles); xweb 0x1019 (origin 2) | LXP_ERR_SEQUENCE_EXHAUSTED = -307, include/layerx/lxp_result.h:78; emitted src/modules/asset/lx_asset_registry.c:637 | no |
| LXP_INSUFFICIENT_BALANCE | insufficient balance | layerxexchange 0x1015; layerxledger 0x101A; layerxprograms 0x101D; shared cgo host (all kernel-backed precompiles); xweb 0x1019 (origin 2) | LXP_ERR_INSUFFICIENT_BALANCE = -400, include/layerx/lxp_result.h:79; emitted src/modules/perps/lx_perps_liquidate.c:122 | yes |
| LXP_ZERO_AMOUNT | zero amount | layerxexchange 0x1015; layerxledger 0x101A; layerxpay 0x101B; layerxprograms 0x101D; shared cgo host (all kernel-backed precompiles); xweb 0x1019 (origin 2) | LXP_ERR_ZERO_AMOUNT = -401, include/layerx/lxp_result.h:80; emitted src/modules/perps/lx_perps_liquidate.c:157 | no |
| LXP_ASSET_MISMATCH | asset mismatch | layerxauth 0x101C; layerxexchange 0x1015; layerxledger 0x101A; layerxpay 0x101B; layerxprograms 0x101D; shared cgo host (all kernel-backed precompiles); xweb 0x1019 (origin 2) | LXP_ERR_ASSET_MISMATCH = -402, include/layerx/lxp_result.h:81; emitted src/modules/asset/lx_asset_transfer.c:23 | no |
| LXP_ASSET_PAUSED | asset paused | layerxexchange 0x1015; layerxledger 0x101A; shared cgo host (all kernel-backed precompiles); xweb 0x1019 (origin 2) | LXP_ERR_ASSET_PAUSED = -403, include/layerx/lxp_result.h:82; emitted src/modules/asset/lx_asset_transfer.c:19 | yes |
| LXP_ACCOUNT_FROZEN | account frozen | layerxledger 0x101A; layerxprograms 0x101D; xweb 0x1019 (origin 2) | LXP_ERR_ACCOUNT_FROZEN = -404, include/layerx/lxp_result.h:83; emitted src/modules/asset/lx_asset_transfer.c:25 | yes |
| LXP_CONSERVATION | conservation | layerxexchange 0x1015; layerxledger 0x101A; layerxpay 0x101B; xweb 0x1019 (origin 2) | LXP_ERR_CONSERVATION = -405, include/layerx/lxp_result.h:84; emitted src/ledger/lxp_apply.c:412 | no |
| LXP_TOO_MANY_LEGS | too many legs | layerxexchange 0x1015; layerxledger 0x101A; layerxprograms 0x101D; shared cgo host (all kernel-backed precompiles); xweb 0x1019 (origin 2) | LXP_ERR_TOO_MANY_LEGS = -406, include/layerx/lxp_result.h:85; emitted src/modules/perps/lx_perps_insurance.c:160 | no |
| LXP_ACCOUNT_NOT_EMPTY | account not empty | addr 0x1004 (DID identity); layerxexchange 0x1015; layerxledger 0x101A | LXP_ERR_ACCOUNT_NOT_EMPTY = -407, include/layerx/lxp_result.h:86; emitted src/modules/perps/lx_perps_dispatch.c:1359 | no |
| LXP_BALANCE_BYPASS | balance bypass | layerxledger 0x101A; shared cgo host (all kernel-backed precompiles); xweb 0x1019 (origin 2) | LXP_ERR_BALANCE_BYPASS = -408, include/layerx/lxp_result.h:87; emitted src/modules/asset/lx_asset_transfer.c:12 | no |
| LXP_ACCOUNT_ID_MISMATCH | account id mismatch | layerxledger 0x101A; layerxprograms 0x101D; shared cgo host (all kernel-backed precompiles) | LXP_ERR_ACCOUNT_ID_MISMATCH = -409, include/layerx/lxp_result.h:88; emitted src/ledger/lx_account_registry.c:564 | no |
| LXP_CLIENT_SUPPLIED_BALANCE | client supplied balance | layerxledger 0x101A; xweb 0x1019 (origin 2) | LXP_ERR_CLIENT_SUPPLIED_BALANCE = -410, include/layerx/lxp_result.h:89; emitted src/ledger/lxp_apply.c:265 | no |
| LXP_ASSET_ALREADY_REGISTERED | asset already registered | layerxledger 0x101A; shared cgo host (all kernel-backed precompiles) | LXP_ERR_ASSET_ALREADY_REGISTERED = -411, include/layerx/lxp_result.h:90; emitted src/modules/asset/lx_asset_registry.c:644 | no |
| LXP_INVALID_AMOUNT | invalid amount | layerxledger 0x101A; layerxpay 0x101B | LXP_ERR_INVALID_AMOUNT = -412, include/layerx/lxp_result.h:91; emitted src/modules/asset/lx_asset_decode.c:102 | no |
| LXP_DEPOSIT_PROOF_NOT_FINAL | deposit proof not final | layerxledger 0x101A; shared cgo host (all kernel-backed precompiles) | LXP_ERR_DEPOSIT_PROOF_NOT_FINAL = -413, include/layerx/lxp_result.h:92; emitted src/modules/asset/lx_asset_custody.c:98 | no |
| LXP_DEPOSIT_ALREADY_CREDITED | deposit already credited | layerxledger 0x101A; shared cgo host (all kernel-backed precompiles) | LXP_ERR_DEPOSIT_ALREADY_CREDITED = -414, include/layerx/lxp_result.h:93; emitted src/modules/asset/lx_asset_custody.c:231 | no |
| LXP_WITHDRAWAL_ALREADY_SETTLED | withdrawal already settled | not emitted after the merge: kernel WITHDRAW 0x00010009 is not registered and 0x101A has no withdraw (ledger-auth.md 1 A); only lx_asset_withdraw_request (src/modules/asset/lx_asset_custody.c:330) and lx_asset_withdraw_settle (:387) emit it | LXP_ERR_WITHDRAWAL_ALREADY_SETTLED = -415, include/layerx/lxp_result.h:94; emitted src/modules/asset/lx_asset_custody.c:351 | no |
| LXP_CHALLENGE_WINDOW_OPEN | challenge window open | not emitted after the merge: only retired kernel paths (bridge credit, network, replica, DA, guarantor) use it | LXP_ERR_CHALLENGE_WINDOW_OPEN = -416, include/layerx/lxp_result.h:95 | no |
| LXP_WITHDRAWAL_CANCELLED | withdrawal cancelled | not emitted after the merge: only retired kernel paths (bridge credit, network, replica, DA, guarantor) use it | LXP_ERR_WITHDRAWAL_CANCELLED = -417, include/layerx/lxp_result.h:96 | no |
| LXP_WITHDRAWAL_ASSET_MISMATCH | withdrawal asset mismatch | not emitted after the merge: kernel WITHDRAW 0x00010009 is not registered and 0x101A has no withdraw (ledger-auth.md 1 A); only lx_asset_withdraw_request (src/modules/asset/lx_asset_custody.c:330) and lx_asset_withdraw_settle (:387) emit it | LXP_ERR_WITHDRAWAL_ASSET_MISMATCH = -418, include/layerx/lxp_result.h:97; emitted src/modules/asset/lx_asset_custody.c:416 | no |
| LXP_OVERFLOW | overflow | addr 0x1004 (DID identity); layerxexchange 0x1015; kernel governance checks inside every kernel-backed precompile; layerxledger 0x101A; layerxpay 0x101B; layerxprograms 0x101D; shared cgo host (all kernel-backed precompiles); xweb 0x1019 (origin 2) | LXP_ERR_OVERFLOW = -500, include/layerx/lxp_result.h:98; emitted src/modules/asset/lx_asset_transfer.c:32 | no |
| LXP_UNDERFLOW | underflow | layerxledger 0x101A; shared cgo host (all kernel-backed precompiles); xweb 0x1019 (origin 2) | LXP_ERR_UNDERFLOW = -501, include/layerx/lxp_result.h:99; emitted src/modules/asset/lx_asset_transfer.c:29 | no |
| LXP_DIV_ZERO | div zero | shared cgo host (all kernel-backed precompiles) | LXP_ERR_DIV_ZERO = -502, include/layerx/lxp_result.h:100; emitted src/protocol/lxp_u256.c:72 | no |
| LXP_PRECISION | precision | not emitted after the merge: only retired kernel paths (bridge credit, network, replica, DA, guarantor) use it; the xweb wei remainder refusal is LXP_PRECOMPILE_WEI_REMAINDER | LXP_ERR_PRECISION = -503, include/layerx/lxp_result.h:101 | no |
| LXP_FEE_LIMIT | fee limit | layerxprograms 0x101D; shared cgo host (all kernel-backed precompiles) | LXP_ERR_FEE_LIMIT = -600, include/layerx/lxp_result.h:102; emitted src/protocol/lxp_fee_policy.c:53 | no |
| LXP_GAS_EXHAUSTED | gas exhausted | layerxprograms 0x101D; shared cgo host (all kernel-backed precompiles) | LXP_ERR_GAS_EXHAUSTED = -601, include/layerx/lxp_result.h:103; emitted src/protocol/lxp_meter.c:37 | yes |
| LXP_FEE_UNPAYABLE | fee unpayable | layerxprograms 0x101D; shared cgo host (all kernel-backed precompiles) | LXP_ERR_FEE_UNPAYABLE = -602, include/layerx/lxp_result.h:104; emitted src/modules/programs/call.c:1903 | yes |
| LXP_ESCROW_STATE | escrow state | layerxpay 0x101B | LXP_ERR_ESCROW_STATE = -700, include/layerx/lxp_result.h:105; emitted src/modules/escrow/lx_escrow_capture.c:14 | no |
| LXP_BUDGET_PERIOD_CAP | budget period cap | layerxpay 0x101B | LXP_ERR_BUDGET_PERIOD_CAP = -701, include/layerx/lxp_result.h:106; emitted src/modules/budget/lx_budget_module.c:418 | yes |
| LXP_STREAM_UNDERFUNDED | stream underfunded | not emitted after the merge: only retired kernel paths (bridge credit, network, replica, DA, guarantor) use it | LXP_ERR_STREAM_UNDERFUNDED = -702, include/layerx/lxp_result.h:107 | no |
| LXP_MARKET_HALTED | market halted | layerxexchange 0x1015; shared cgo host (all kernel-backed precompiles) | LXP_ERR_MARKET_HALTED = -703, include/layerx/lxp_result.h:108; emitted src/modules/perps/lx_oracle_halt.c:24 | yes |
| LXP_ORACLE_STALE | oracle stale | layerxexchange 0x1015 | LXP_ERR_ORACLE_STALE = -704, include/layerx/lxp_result.h:109; emitted src/modules/perps/lx_oracle_intake.c:158 | yes |
| LXP_MARGIN_INSUFFICIENT | margin insufficient | layerxexchange 0x1015 | LXP_ERR_MARGIN_INSUFFICIENT = -705, include/layerx/lxp_result.h:110; emitted src/modules/perps/lx_perps_position.c:270 | yes |
| LXP_AGREEMENT_STATE | agreement state | layerxexchange 0x1015; layerxpay 0x101B | LXP_ERR_AGREEMENT_STATE = -706, include/layerx/lxp_result.h:111; emitted src/modules/service/lx_service_agreement.c:79 | no |
| LXP_CAPTURE_EXCEEDS_HOLD | capture exceeds hold | layerxpay 0x101B | LXP_ERR_CAPTURE_EXCEEDS_HOLD = -707, include/layerx/lxp_result.h:112; emitted src/modules/escrow/lx_escrow_capture.c:88 | no |
| LXP_UNAUTHORIZED_CAPTURE | unauthorized capture | layerxpay 0x101B | LXP_ERR_UNAUTHORIZED_CAPTURE = -708, include/layerx/lxp_result.h:113; emitted src/modules/escrow/lx_escrow_capture.c:78 | no |
| LXP_HOLD_EXPIRED | hold expired | layerxpay 0x101B | LXP_ERR_HOLD_EXPIRED = -709, include/layerx/lxp_result.h:114; emitted src/modules/escrow/lx_escrow_capture.c:67 | no |
| LXP_UNAUTHORIZED_ESCROW_SPEND | unauthorized escrow spend | layerxpay 0x101B | LXP_ERR_UNAUTHORIZED_ESCROW_SPEND = -710, include/layerx/lxp_result.h:115; emitted src/modules/escrow/lx_escrow_invariants.c:20 | no |
| LXP_DISPUTE_WINDOW_CLOSED | dispute window closed | layerxpay 0x101B | LXP_ERR_DISPUTE_WINDOW_CLOSED = -711, include/layerx/lxp_result.h:116; emitted src/modules/service/lx_service_dispute.c:41 | no |
| LXP_HOLD_DISPUTED | hold disputed | layerxpay 0x101B | LXP_ERR_HOLD_DISPUTED = -712, include/layerx/lxp_result.h:117; emitted src/modules/escrow/lx_escrow_timeout.c:47 | no |
| LXP_BUDGET_ALLOWANCE_EXCEEDED | budget allowance exceeded | layerxpay 0x101B | LXP_ERR_BUDGET_ALLOWANCE_EXCEEDED = -713, include/layerx/lxp_result.h:118; emitted src/modules/budget/lx_budget_spend.c:28 | no |
| LXP_INSUFFICIENT_BUDGET_FUNDS | insufficient budget funds | layerxpay 0x101B | LXP_ERR_INSUFFICIENT_BUDGET_FUNDS = -714, include/layerx/lxp_result.h:119; emitted src/modules/budget/lx_budget_close.c:72 | yes |
| LXP_UNAUTHORIZED_DELEGATE | unauthorized delegate | layerxpay 0x101B | LXP_ERR_UNAUTHORIZED_DELEGATE = -715, include/layerx/lxp_result.h:120; emitted src/modules/budget/lx_budget_delegate.c:61 | no |
| LXP_BUDGET_REVOKED | budget revoked | layerxpay 0x101B | LXP_ERR_BUDGET_REVOKED = -716, include/layerx/lxp_result.h:121; emitted src/modules/budget/lx_budget_close.c:93 | no |
| LXP_NON_MONOTONIC_TIME | non monotonic time | kernel governance checks inside every kernel-backed precompile; layerxpay 0x101B | LXP_ERR_NON_MONOTONIC_TIME = -717, include/layerx/lxp_result.h:122; emitted src/modules/stream/lx_stream_open.c:92 | no |
| LXP_ACCRUAL_OVERFLOW | accrual overflow | layerxpay 0x101B | LXP_ERR_ACCRUAL_OVERFLOW = -718, include/layerx/lxp_result.h:123; emitted src/modules/stream/lx_stream_accrue.c:58 | no |
| LXP_UNAUTHORIZED_METER | unauthorized meter | layerxpay 0x101B | LXP_ERR_UNAUTHORIZED_METER = -719, include/layerx/lxp_result.h:124; emitted src/modules/stream/lx_stream_meter.c:39 | no |
| LXP_METER_REGRESSION | meter regression | layerxpay 0x101B | LXP_ERR_METER_REGRESSION = -720, include/layerx/lxp_result.h:125; emitted src/modules/service/lx_service_commit.c:143 | no |
| LXP_STREAM_CLOSED | stream closed | layerxpay 0x101B | LXP_ERR_STREAM_CLOSED = -721, include/layerx/lxp_result.h:126; emitted src/modules/stream/lx_stream_open.c:135 | no |
| LXP_MODULE_MAY_NOT_WRITE_BALANCE | module may not write balance | layerxexchange 0x1015; layerxpay 0x101B | LXP_ERR_MODULE_MAY_NOT_WRITE_BALANCE = -722, include/layerx/lxp_result.h:127; emitted src/modules/service/lx_service_acceptance.c:17 | no |
| LXP_OFFER_UNAVAILABLE | offer unavailable | layerxpay 0x101B | LXP_ERR_OFFER_UNAVAILABLE = -723, include/layerx/lxp_result.h:128; emitted src/modules/service/lx_service_offer.c:69 | no |
| LXP_TERMS_MISMATCH | terms mismatch | layerxpay 0x101B | LXP_ERR_TERMS_MISMATCH = -724, include/layerx/lxp_result.h:129; emitted src/modules/service/lx_service_agreement.c:34 | no |
| LXP_INVALID_ATTESTATION | invalid attestation | layerxpay 0x101B; xweb 0x1019 (origin 2) | LXP_ERR_INVALID_ATTESTATION = -725, include/layerx/lxp_result.h:130; emitted src/modules/service/lx_service_attest.c:127 | no |
| LXP_DELIVERABLE_MISMATCH | deliverable mismatch | layerxpay 0x101B | LXP_ERR_DELIVERABLE_MISMATCH = -726, include/layerx/lxp_result.h:131; emitted src/modules/service/lx_service_deliver.c:25 | no |
| LXP_DELIVERY_DEADLINE_PASSED | delivery deadline passed | layerxpay 0x101B | LXP_ERR_DELIVERY_DEADLINE_PASSED = -727, include/layerx/lxp_result.h:132; emitted src/modules/service/lx_service_deliver.c:87 | no |
| LXP_UNAUTHORIZED_DISPUTANT | unauthorized disputant | layerxpay 0x101B | LXP_ERR_UNAUTHORIZED_DISPUTANT = -728, include/layerx/lxp_result.h:133; emitted src/modules/service/lx_service_dispute.c:39 | no |
| LXP_UNAUTHORIZED_ORACLE | unauthorized oracle | layerxexchange 0x1015 | LXP_ERR_UNAUTHORIZED_ORACLE = -729, include/layerx/lxp_result.h:134; emitted src/modules/perps/lx_oracle_intake.c:83 | no |
| LXP_ORACLE_SEQUENCE | oracle sequence | layerxexchange 0x1015 | LXP_ERR_ORACLE_SEQUENCE = -730, include/layerx/lxp_result.h:135; emitted src/modules/perps/lx_perps_dispatch.c:530 | no |
| LXP_ORACLE_BOUNDS | oracle bounds | layerxexchange 0x1015 | LXP_ERR_ORACLE_BOUNDS = -731, include/layerx/lxp_result.h:136; emitted src/modules/perps/lx_oracle_checks.c:25 | no |
| LXP_ORACLE_DEVIATION | oracle deviation | layerxexchange 0x1015 | LXP_ERR_ORACLE_DEVIATION = -732, include/layerx/lxp_result.h:137; emitted src/modules/perps/lx_oracle_checks.c:48 | no |
| LXP_MARKET_ALREADY_EXISTS | market already exists | layerxexchange 0x1015 | LXP_ERR_MARKET_ALREADY_EXISTS = -733, include/layerx/lxp_result.h:138; emitted src/modules/perps/lx_perps_dispatch.c:606 | no |
| LXP_PARAMETER_BOUNDS | parameter bounds | layerxexchange 0x1015; kernel governance checks inside every kernel-backed precompile; layerxpay 0x101B; layerxprograms 0x101D; shared cgo host (all kernel-backed precompiles); xweb 0x1019 (origin 2) | LXP_ERR_PARAMETER_BOUNDS = -734, include/layerx/lxp_result.h:139; emitted src/modules/service/lx_service_payload.c:159 | no |
| LXP_PAUSED_SCOPE | paused scope | every kernel-backed precompile through lxp_pause_scope_check (src/modules/governance/lxp_emergency.c:139-160); layerxpay 0x101B writes outside the exit set (pay.md F); layerxexchange 0x1015 Go Params.Paused, exit paths cancelOrder, cancelSpotOrder, closePosition, withdrawMargin exempt (exchange-launchpad.md F) | LXP_ERR_PAUSED_SCOPE = -735, include/layerx/lxp_result.h:140; emitted src/modules/governance/lxp_emergency.c:98 | yes |
| LXP_PROGRAM_REFUSED | program refused | layerxprograms 0x101D; shared cgo host (all kernel-backed precompiles) | LXP_ERR_PROGRAM_REFUSED = -736, include/layerx/lxp_result.h:141; emitted src/modules/programs/call.c:648 | no |
| LXP_ARBITER_REQUIRED | arbiter required | layerxpay 0x101B serviceDisputeOpen, serviceDisputeResolve (linked escrow has no arbiter) | LXP_ERR_ARBITER_REQUIRED = -737, include/layerx/lxp_result.h:142; pay.md A.6 | no |
| LXP_UNAUTHORIZED_ARBITER | unauthorized arbiter | layerxpay 0x101B serviceDisputeResolve (principal is not the linked escrow arbiter) | LXP_ERR_UNAUTHORIZED_ARBITER = -738, include/layerx/lxp_result.h:143; pay.md A.6 | no |
| LXP_ESCROW_LINK_MISMATCH | escrow link mismatch | layerxpay 0x101B serviceDisputeOpen, serviceDisputeResolve (escrow `owner`, beneficiary or asset differ from the agreement) | LXP_ERR_ESCROW_LINK_MISMATCH = -739, include/layerx/lxp_result.h:144; pay.md A.6 | no |
| LXP_BATCH_GAP | batch gap | layerxprograms 0x101D; shared cgo host (all kernel-backed precompiles) | LXP_ERR_BATCH_GAP = -800, include/layerx/lxp_result.h:145; emitted src/modules/programs/occupancy_evidence.c:673 | no |
| LXP_ROOT_MISMATCH | root mismatch | layerxprograms 0x101D; shared cgo host (all kernel-backed precompiles) | LXP_ERR_ROOT_MISMATCH = -801, include/layerx/lxp_result.h:146; emitted src/modules/programs/fees.c:606 | no |
| LXP_TIMESTAMP_REGRESSION | timestamp regression | layerxexchange 0x1015; layerxpay 0x101B; shared cgo host (all kernel-backed precompiles) | LXP_ERR_TIMESTAMP_REGRESSION = -802, include/layerx/lxp_result.h:147; emitted src/modules/perps/lx_oracle_intake.c:162 | no |
| LXP_ATTESTATION_THRESHOLD | attestation threshold | layerxprograms 0x101D; xweb 0x1019 (origin 2) | LXP_ERR_ATTESTATION_THRESHOLD = -803, include/layerx/lxp_result.h:148; emitted src/modules/programs/blob_lifecycle.c:486 | no |
| LXP_DA_MISSING | da missing | layerxpay 0x101B | LXP_ERR_DA_MISSING = -804, include/layerx/lxp_result.h:149; emitted src/modules/service/lx_service_deliver.c:28 | no |
| LXP_EQUIVOCATION | equivocation | not emitted after the merge: only retired kernel paths (bridge credit, network, replica, DA, guarantor) use it | LXP_ERR_EQUIVOCATION = -805, include/layerx/lxp_result.h:150 | no |
| LXP_LOG_CORRUPT | log corrupt | layerxprograms 0x101D; shared cgo host (all kernel-backed precompiles) | LXP_ERR_LOG_CORRUPT = -900, include/layerx/lxp_result.h:151; emitted src/state/lxp_journal.c:1197 | no |
| LXP_LOG_TRUNCATED | log truncated | shared cgo host (all kernel-backed precompiles) | LXP_ERR_LOG_TRUNCATED = -901, include/layerx/lxp_result.h:152; emitted src/protocol/lxp_kernel.c:8671 | no |
| LXP_SNAPSHOT_MISMATCH | snapshot mismatch | shared cgo host (all kernel-backed precompiles) | LXP_ERR_SNAPSHOT_MISMATCH = -902, include/layerx/lxp_result.h:153; emitted src/state/lxp_snapshot.c:154 | no |
| LXP_PROJECTION_STALE | projection stale | layerxprograms 0x101D; shared cgo host (all kernel-backed precompiles) | LXP_ERR_PROJECTION_STALE = -903, include/layerx/lxp_result.h:154; emitted src/protocol/lxp_kernel.c:8085 | yes |
| LXP_IO | io | layerxauth 0x101C; layerxledger 0x101A; layerxprograms 0x101D; shared cgo host (all kernel-backed precompiles) | LXP_ERR_IO = -904, include/layerx/lxp_result.h:155; emitted src/ledger/lxp_journal.c:248 | yes |
| LXP_ARENA_EXHAUSTED | arena exhausted | addr 0x1004 (DID identity); layerxexchange 0x1015; kernel governance checks inside every kernel-backed precompile; layerxledger 0x101A; layerxpay 0x101B; layerxprograms 0x101D; shared cgo host (all kernel-backed precompiles); xweb 0x1019 (origin 2) | LXP_ERR_ARENA_EXHAUSTED = -905, include/layerx/lxp_result.h:156; emitted src/modules/perps/lx_perps_insurance.c:83 | no |
| LXP_SNAPSHOT_BLOBS_MISSING | snapshot blobs missing | shared cgo host (all kernel-backed precompiles) | LXP_ERR_SNAPSHOT_BLOBS_MISSING = -906, include/layerx/lxp_result.h:157; emitted src/state/lxp_snapshot.c:951 | no |
| LXP_FATAL_INVARIANT | fatal invariant | not a revert: fatal, the node halts (src/protocol/lxp_result.c:55 lxp_result_is_fatal) | LXP_FATAL_INVARIANT = -1001, include/layerx/lxp_result.h:158; emitted src/modules/budget/lx_budget_close.c:77 | no |
| LXP_FATAL_REPLAY_DIVERGENCE | fatal replay divergence | not a revert: fatal, the node halts (src/protocol/lxp_result.c:55 lxp_result_is_fatal) | LXP_FATAL_REPLAY_DIVERGENCE = -1002, include/layerx/lxp_result.h:159; emitted src/modules/programs/fees.c:625 | no |
| LXP_FATAL_SUPPLY_MISMATCH | fatal supply mismatch | not a revert: fatal, the node halts (src/protocol/lxp_result.c:55 lxp_result_is_fatal) | LXP_FATAL_SUPPLY_MISMATCH = -1003, include/layerx/lxp_result.h:160; emitted src/modules/asset/lx_asset_reserve.c:10 | no |
| LXP_UNKNOWN | unknown | shared cgo host (any value outside the enum) | lxp_result_name default branch, src/protocol/lxp_result.c:10 | no |
| LXP_XWEB_UNAUTHORIZED | unauthorized | xweb 0x1019 | ErrUnauthorized (code 2), modules/xweb/types/errors.go:6 | no |
| LXP_XWEB_INVALID_GENESIS | invalid genesis | xweb 0x1019 | ErrInvalidGenesis (code 3), modules/xweb/types/errors.go:7 | no |
| LXP_XWEB_INVALID_PARAMS | invalid params | xweb 0x1019 | ErrInvalidParams (code 4), modules/xweb/types/errors.go:8 | no |
| LXP_XWEB_INVALID_ATTESTORS | invalid attestor set | xweb 0x1019 | ErrInvalidAttestors (code 5), modules/xweb/types/errors.go:9 | no |
| LXP_XWEB_INVALID_THRESHOLD | threshold is not a majority of the attestor set | xweb 0x1019 | ErrInvalidThreshold (code 6), modules/xweb/types/errors.go:10 | no |
| LXP_XWEB_PAUSED | xweb paused | xweb 0x1019 | ErrPaused (code 7), modules/xweb/types/errors.go:11 | yes |
| LXP_XWEB_UNKNOWN_KIND | unknown request kind | xweb 0x1019 | ErrUnknownKind (code 8), modules/xweb/types/errors.go:12 | no |
| LXP_XWEB_PAYLOAD_SIZE | payload empty or over the payload cap | xweb 0x1019 | ErrPayloadSize (code 9), modules/xweb/types/errors.go:13 | no |
| LXP_XWEB_CALLBACK_GAS | callback gas zero or over the callback cap | xweb 0x1019 | ErrCallbackGas (code 10), modules/xweb/types/errors.go:14 | no |
| LXP_XWEB_WRONG_FEE | paid amount is not the current fee | xweb 0x1019 | ErrWrongFee (code 11), modules/xweb/types/errors.go:15 | no |
| LXP_XWEB_UNKNOWN_REQUEST | request not found | xweb 0x1019; `refund` of an origin-2 id, never keyed in the refund store (web.md A3, F7) | ErrUnknownRequest (code 12), modules/xweb/types/errors.go:16 | no |
| LXP_XWEB_ALREADY_FULFILLED | request already fulfilled | xweb 0x1019 | ErrAlreadyFulfilled (code 13), modules/xweb/types/errors.go:17 | no |
| LXP_XWEB_REFUNDED | request already refunded | xweb 0x1019 | ErrRefunded (code 14), modules/xweb/types/errors.go:18 | no |
| LXP_XWEB_RESPONSE_TOO_LARGE | response over the stored response bound | xweb 0x1019 | ErrResponseTooLarge (code 15), modules/xweb/types/errors.go:19 | no |
| LXP_XWEB_INVALID_LENGTH | full length shorter than the response | xweb 0x1019 | ErrInvalidLength (code 16), modules/xweb/types/errors.go:20 | no |
| LXP_XWEB_BAD_SIGNATURE | invalid attestor signature | xweb 0x1019 | ErrBadSignature (code 17), modules/xweb/types/errors.go:21 | no |
| LXP_XWEB_BELOW_THRESHOLD | attestor signatures below threshold | xweb 0x1019 | ErrBelowThreshold (code 18), modules/xweb/types/errors.go:22 | no |
| LXP_XWEB_NOT_EXPIRED | request not yet past its timeout | xweb 0x1019 | ErrNotExpired (code 19), modules/xweb/types/errors.go:23 | yes |
| LXP_XWEB_UNKNOWN_ATTESTOR | attestor not registered | xweb 0x1019 | ErrUnknownAttestor (code 20), modules/xweb/types/errors.go:24 | no |
| LXP_XWEB_CALLBACK_RECORDED | callback outcome already recorded | xweb 0x1019 | ErrCallbackRecorded (code 21), modules/xweb/types/errors.go:25 | no |
| LXP_XWEB_INVALID_REQUEST | invalid request | xweb 0x1019 | ErrInvalidRequest (code 22), modules/xweb/types/errors.go:26 | no |
| LXP_XWEB_INVALID_API | invalid api payload | xweb 0x1019 | ErrInvalidApi (code 23), modules/xweb/types/errors.go:27 | no |
| LXP_XWEB_INVALID_ENVELOPE | invalid credential envelope | xweb 0x1019 | ErrInvalidEnvelope (code 24), modules/xweb/types/errors.go:28 | no |
| LXP_XWEB_INVALID_LEVEL | invalid attestation level | xweb 0x1019 | ErrInvalidLevel (code 25), modules/xweb/types/errors.go:29 | no |
| LXP_XWEB_DELEGATECALL | cannot delegatecall xweb | xweb 0x1019 | precompile site precompiles/xweb/xweb.go:290 | no |
| LXP_XWEB_STATICCALL | cannot call xweb <method> from staticcall | xweb 0x1019 | precompile site precompiles/xweb/xweb.go:293 | no |
| LXP_XWEB_KEEPER_MISSING | xweb: the xweb keeper is not wired into this application | xweb 0x1019 | precompile site precompiles/xweb/xweb.go:296 | no |
| LXP_XWEB_FULFIL_EVM_ONLY | xweb: fulfil runs only through the EVM, which meters its callback | xweb 0x1019 | precompile site precompiles/xweb/xweb.go:243 | no |
| LXP_XWEB_CALLBACK_BUDGET | xweb: fulfil of request <id> needs <n> gas for its callback and its record, <m> left | xweb 0x1019 | precompile site precompiles/xweb/xweb.go:416 (text :417-418) | no |
| LXP_BANK_INSUFFICIENT_FUNDS | insufficient funds | xweb 0x1019 `request`, `fulfil`, `refund` bank legs; any precompile moving coins through bank SendCoins | ErrInsufficientFunds (root codespace "sdk", code 5), sdk/types/errors/errors.go:32, surfaced by sdk/x/bank/keeper/send.go:238; xweb legs modules/xweb/keeper/request.go:53, fulfil.go:148, refund.go:35 (web.md RQ, FU, RF) | yes |
| LXP_BRIDGE_UNAUTHORIZED | unauthorized | layerxbridge 0x1016 | ErrUnauthorized (code 2), modules/layerxbridge/types/errors.go:6 | no |
| LXP_BRIDGE_UNKNOWN_CHAIN | chain not registered | layerxbridge 0x1016 | ErrUnknownChain (code 5), modules/layerxbridge/types/errors.go:9 | no |
| LXP_BRIDGE_CHAIN_DISABLED | chain disabled | layerxbridge 0x1016 | ErrChainDisabled (code 6), modules/layerxbridge/types/errors.go:10 | yes |
| LXP_BRIDGE_UNKNOWN_ASSET | asset not registered | layerxbridge 0x1016 | ErrUnknownAsset (code 9), modules/layerxbridge/types/errors.go:13 | no |
| LXP_BRIDGE_PAUSED | bridge paused | layerxbridge 0x1016 | ErrPaused (code 10), modules/layerxbridge/types/errors.go:14 | yes |
| LXP_BRIDGE_NULLIFIED | remote event already bridged | layerxbridge 0x1016 | ErrNullified (code 11), modules/layerxbridge/types/errors.go:15 | no |
| LXP_BRIDGE_CAP_EXCEEDED | amount exceeds the asset cap | layerxbridge 0x1016 | ErrCapExceeded (code 12), modules/layerxbridge/types/errors.go:16 | yes |
| LXP_BRIDGE_BAD_SIGNATURE | invalid attestor signature | layerxbridge 0x1016 | ErrBadSignature (code 13), modules/layerxbridge/types/errors.go:17 | no |
| LXP_BRIDGE_BELOW_THRESHOLD | attestor signatures below threshold | layerxbridge 0x1016 | ErrBelowThreshold (code 14), modules/layerxbridge/types/errors.go:18 | no |
| LXP_BRIDGE_VAULT_MISMATCH | vault is not the registered vault | layerxbridge 0x1016 | ErrVaultMismatch (code 15), modules/layerxbridge/types/errors.go:19 | no |
| LXP_BRIDGE_INVALID_REQUEST | invalid bridge request | layerxbridge 0x1016 | ErrInvalidRequest (code 16), modules/layerxbridge/types/errors.go:20 | no |
| LXP_LAUNCHPAD_OVERFLOW | uint256 overflow | launchpad 0x1017 | ErrOverflow (code 3), modules/launchpad/types/errors.go:9 | no |
| LXP_LAUNCHPAD_DIVISION_BY_ZERO | division by zero | launchpad 0x1017 | ErrDivisionByZero (code 4), modules/launchpad/types/errors.go:10 | no |
| LXP_LAUNCHPAD_INSUFFICIENT_INPUT | insufficient input | launchpad 0x1017 | ErrInsufficientInput (code 5), modules/launchpad/types/errors.go:11 | no |
| LXP_LAUNCHPAD_INSUFFICIENT_LIQUIDITY | insufficient liquidity | launchpad 0x1017 | ErrInsufficientLiquidity (code 6), modules/launchpad/types/errors.go:12 | yes |
| LXP_LAUNCHPAD_VIRTUAL_FLOOR_BREACHED | virtual floor breached | launchpad 0x1017 | ErrVirtualFloorBreached (code 7), modules/launchpad/types/errors.go:13 | no |
| LXP_LAUNCHPAD_SLIPPAGE_EXCEEDED | slippage exceeded | launchpad 0x1017 | ErrSlippageExceeded (code 8), modules/launchpad/types/errors.go:14 | yes |
| LXP_LAUNCHPAD_DEADLINE_EXPIRED | deadline expired | launchpad 0x1017 | ErrDeadlineExpired (code 9), modules/launchpad/types/errors.go:15 | no |
| LXP_LAUNCHPAD_ZERO_ADDRESS | zero address | launchpad 0x1017 | ErrZeroAddress (code 10), modules/launchpad/types/errors.go:16 | no |
| LXP_LAUNCHPAD_ZERO_AMOUNT | zero amount | launchpad 0x1017 | ErrZeroAmount (code 11), modules/launchpad/types/errors.go:17 | no |
| LXP_LAUNCHPAD_UNKNOWN_MARKET | unknown market | launchpad 0x1017 | ErrUnknownMarket (code 12), modules/launchpad/types/errors.go:18 | no |
| LXP_LAUNCHPAD_PAUSED | market is paused | launchpad 0x1017 | ErrPaused (code 13), modules/launchpad/types/errors.go:19 | yes |
| LXP_LAUNCHPAD_NOT_PAUSED | market is not paused | launchpad 0x1017 | ErrNotPaused (code 14), modules/launchpad/types/errors.go:20 | yes |
| LXP_LAUNCHPAD_NOT_GUARDIAN | caller is not the market guardian | launchpad 0x1017 | ErrNotGuardian (code 15), modules/launchpad/types/errors.go:21 | no |
| LXP_LAUNCHPAD_NOT_FEE_RIGHTS_HOLDER | caller does not hold the market's fee rights | launchpad 0x1017 | ErrNotFeeRightsHolder (code 16), modules/launchpad/types/errors.go:22 | no |
| LXP_LAUNCHPAD_WRONG_STRATEGY | wrong fee strategy | launchpad 0x1017 | ErrWrongStrategy (code 17), modules/launchpad/types/errors.go:23 | no |
| LXP_LAUNCHPAD_INVALID_STRATEGY | invalid fee strategy | launchpad 0x1017 | ErrInvalidStrategy (code 18), modules/launchpad/types/errors.go:24 | no |
| LXP_LAUNCHPAD_NO_FEES_ACCUMULATED | no fees accumulated | launchpad 0x1017 | ErrNoFeesAccumulated (code 19), modules/launchpad/types/errors.go:25 | yes |
| LXP_LAUNCHPAD_AIRDROP_NOT_TRIGGERED | airdrop not triggered | launchpad 0x1017 | ErrAirdropNotTriggered (code 20), modules/launchpad/types/errors.go:26 | yes |
| LXP_LAUNCHPAD_ALREADY_CLAIMED | airdrop already claimed | launchpad 0x1017 | ErrAlreadyClaimed (code 21), modules/launchpad/types/errors.go:27 | no |
| LXP_LAUNCHPAD_INVALID_MARKET | invalid market | launchpad 0x1017 | ErrInvalidMarket (code 22), modules/launchpad/types/errors.go:28 | no |
| LXP_LAUNCHPAD_UNAUTHORIZED | not the launchpad params authority | launchpad 0x1017 | ErrUnauthorized (code 23), modules/launchpad/types/errors.go:29 | no |
| LXP_LAUNCHPAD_HOLDING_HISTORY | invalid holding history | launchpad 0x1017 | ErrHoldingHistory (code 25), modules/launchpad/types/errors.go:31 | no |
| LXP_LAUNCHPAD_UNSUPPORTED_HOLDING_EPOCH | holding history does not cover epoch | launchpad 0x1017 | ErrUnsupportedHoldingEpoch (code 26), modules/launchpad/types/errors.go:32 | no |
| LXP_LAUNCHPAD_INVALID_AIRDROP_EPOCH | airdrop epoch not opened | launchpad 0x1017 | ErrInvalidAirdropEpoch (code 27), modules/launchpad/types/errors.go:33 | yes |
| LXP_LAUNCHPAD_LEGACY_AIRDROP_EPOCH | airdrop epoch has no entitlement basis | launchpad 0x1017 | ErrLegacyAirdropEpoch (code 28), modules/launchpad/types/errors.go:34 | no |
| LXP_LAUNCHPAD_AIRDROP_BASIS | invalid airdrop entitlement basis | launchpad 0x1017 | ErrAirdropBasis (code 29), modules/launchpad/types/errors.go:35 | no |
| LXP_ANCHOR_CHECKPOINT_UNKNOWN | checkpoint unknown | layerxanchor 0x1014 read methods until the store-delete step (writes retired) | ErrCheckpointUnknown (code 8), modules/layerxanchor/types/errors.go:12 | no |
| LXP_ANCHOR_GUARANTOR_UNKNOWN | guarantor unknown | layerxanchor 0x1014 read methods until the store-delete step (writes retired) | ErrGuarantorUnknown (code 10), modules/layerxanchor/types/errors.go:14 | no |
| LXP_TOKENFACTORY_DENOM_EXISTS | attempting to create a denom that already exists (has bank metadata) | layerxledger 0x101A register/mint/burn through the tokenfactory keeper | ErrDenomExists (code 2), modules/tokenfactory/types/errors.go:13 | no |
| LXP_TOKENFACTORY_UNAUTHORIZED | unauthorized account | layerxledger 0x101A register/mint/burn through the tokenfactory keeper | ErrUnauthorized (code 3), modules/tokenfactory/types/errors.go:14 | no |
| LXP_TOKENFACTORY_INVALID_DENOM | invalid denom | layerxledger 0x101A register/mint/burn through the tokenfactory keeper | ErrInvalidDenom (code 4), modules/tokenfactory/types/errors.go:15 | no |
| LXP_TOKENFACTORY_SUBDENOM_TOO_LONG | subdenom too long, max length is MaxSubdenomLength bytes | layerxledger 0x101A register/mint/burn through the tokenfactory keeper | ErrSubdenomTooLong (code 8), modules/tokenfactory/types/errors.go:19 | no |
| LXP_TOKENFACTORY_DENOM_DOES_NOT_EXIST | denom does not exist | layerxledger 0x101A register/mint/burn through the tokenfactory keeper | ErrDenomDoesNotExist (code 10), modules/tokenfactory/types/errors.go:21 | no |
| LXP_EXCHANGE_INVALID_MARKET | invalid market | layerxexchange 0x1015 | ErrInvalidMarket (code 3), modules/layerxexchange/types/errors.go:9 | no |
| LXP_EXCHANGE_UNKNOWN_MARKET | market is not listed or is disabled | layerxexchange 0x1015 | ErrUnknownMarket (code 4), modules/layerxexchange/types/errors.go:10 | no |
| LXP_EXCHANGE_NOT_MARGIN_ASSET | asset is not the margin asset of an enabled market | layerxexchange 0x1015 | ErrNotMarginAsset (code 5), modules/layerxexchange/types/errors.go:11 | no |
| LXP_EXCHANGE_KEEPER_MISSING | layerxexchange keeper is not wired | layerxexchange 0x1015 | ErrKeeperMissing (code 12), modules/layerxexchange/types/errors.go:18 | no |
| LXP_ADDR_PANIC | execution reverted: <recovered value> | addr 0x1004 | precompiles/addr/addr.go:138 | no |
| LXP_ADDR_STATICCALL | cannot call <method> from staticcall | addr 0x1004 | precompiles/addr/addr.go:148,153,158,166 | no |
| LXP_ADDR_DELEGATECALL | cannot delegatecall <method> | addr 0x1004 | precompiles/addr/addr.go:161,169 | no |
| LXP_ADDR_EVM_NOT_ASSOCIATED | EVM address is not associated | addr 0x1004 | precompiles/addr/addr.go:196 | yes |
| LXP_ADDR_PAX_NOT_ASSOCIATED | pax address is not associated | addr 0x1004 | precompiles/addr/addr.go:219 | yes |
| LXP_ADDR_ALREADY_ASSOCIATED | address is already associated with evm address | addr 0x1004 | precompiles/addr/addr.go:308 | no |
| LXP_ADDR_EVM_NOT_BOUND | EVM address is not bound to a LayerX DID | addr 0x1004; principal resolution of 0x101A-0x101D (sender without a DID) | precompiles/addr/addr.go:401 | yes |
| LXP_ADDR_DID_NOT_BOUND | LayerX DID is not bound to an EVM address | addr 0x1004 | precompiles/addr/addr.go:417 | yes |
| LXP_ADDR_LAYERX_CHAIN_ID | layerx binding: chain id does not fit 32 bytes | addr 0x1004 (binding keeper) | ErrLayerXChainID, modules/evm/types/layerx_binding.go:28 | no |
| LXP_ADDR_LAYERX_SIGNATURE | layerx binding: signature refused | addr 0x1004 (binding keeper) | ErrLayerXSignature, modules/evm/types/layerx_binding.go:29 | no |
| LXP_ADDR_LAYERX_ADDRESS_BOUND | layerx binding: evm address is already bound to a did | addr 0x1004 (binding keeper) | ErrLayerXAddressBound, modules/evm/types/layerx_binding.go:30 | no |
| LXP_ADDR_LAYERX_DID_BOUND | layerx binding: did is already bound to an evm address | addr 0x1004 (binding keeper) | ErrLayerXDidBound, modules/evm/types/layerx_binding.go:31 | no |
| LXP_ADDR_LAYERX_NOT_BOUND | layerx binding: evm address is not bound to a did | addr 0x1004 (binding keeper) | ErrLayerXNotBound, modules/evm/types/layerx_binding.go:32 | yes |
| LXP_ADDR_LAYERX_NONCE_EXHAUSTED | layerx binding: nonce exhausted | addr 0x1004 (binding keeper) | ErrLayerXNonceExhausted, modules/evm/types/layerx_binding.go:33 | no |
| LXP_ADDR_LAYERX_NON_CANONICAL_KEY | layerx binding: did public key is not canonical | addr 0x1004 (binding keeper) | ErrLayerXNonCanonicalKey, modules/evm/types/layerx_binding.go:34 | no |
| LXP_ADDR_LAYERX_SIGNATURE_LENGTH | layerx binding: signature must be 64 bytes | addr 0x1004 (binding keeper) | ErrLayerXSignatureLength, modules/evm/types/layerx_binding.go:35 | no |
| LXP_FEETOKEN_KEEPER_MISSING | feetoken: fee-token EVM keeper is unavailable | feetoken 0x1018 | precompiles/feetoken/feetoken.go:45,85 | no |
| LXP_FEETOKEN_DELEGATECALL | cannot delegatecall feeToken | feetoken 0x1018 | precompiles/feetoken/feetoken.go:76 | no |
| LXP_FEETOKEN_STATICCALL | cannot call a feeToken state change from staticcall | feetoken 0x1018 | precompiles/feetoken/feetoken.go:82 | no |
| LXP_FEETOKEN_UNKNOWN_METHOD | feetoken: unknown method | feetoken 0x1018 | precompiles/feetoken/feetoken.go:108 | no |
| LXP_FEETOKEN_SWITCH_OFF | fee token preference: fee-token switch is off | feetoken 0x1018 | modules/evm/keeper/feetoken.go:27 | yes |
| LXP_FEETOKEN_DENOM_NOT_ALLOWED | fee token preference: denom is not allowed | feetoken 0x1018 | modules/evm/keeper/feetoken.go:30 (ErrFeeTokenDenomNotAllowed :43) | no |
| LXP_PRECOMPILE_NO_CONTEXT | cannot get context from EVM | every precompile (framework) | precompiles/common/precompiles.go:125 | no |
| LXP_PRECOMPILE_UNKNOWN_METHOD | no method with id: <selector> | every precompile (framework) | precompiles/common/precompiles.go:131 (abi MethodById); text from pax-geth v1.15.7-pax.17 `no method with id: %#x` (go.mod:280 replaces go-ethereum) | no |
| LXP_PRECOMPILE_INPUT_TOO_SHORT | input too short to extract method ID | every precompile (framework) | precompiles/common/precompiles.go:281 | no |
| LXP_PRECOMPILE_ARG_COUNT | expected N arguments but got M | every precompile (framework) | precompiles/common/precompiles.go:213 | no |
| LXP_PRECOMPILE_NON_PAYABLE | sending funds to a non-payable function | every precompile (framework) | precompiles/common/precompiles.go:221 | no |
| LXP_PRECOMPILE_WEI_REMAINDER | selected precompile function does not allow payment with non-zero wei remainder | every payable precompile (framework); xweb 0x1019 `request` and `fulfil` (web.md A1; precompiles/xweb/xweb_test.go:296-298) | precompiles/common/precompiles.go:230 | no |
| LXP_PRECOMPILE_INVALID_ADDR | invalid addr | every precompile taking an address argument (framework) | precompiles/common/precompiles.go:318 | no |
| LXP_AGENT_ABI_DECODE | argument decode failed | agent functions 0x0901-0x0908: every agent function | agent-functions.md A.0; new under precompiles/layerxagent | no |
| LXP_AGENT_READ_ONLY | write under staticcall | agent functions 0x0901-0x0908: submitPrice, schedule, cancel, reschedule, open, settle, close, updateRate | agent-functions.md A.0; new under precompiles/layerxagent | no |
| LXP_AGENT_DELEGATECALL | cannot delegatecall an agent function | agent functions 0x0901-0x0908: every write method | agent-functions.md A.0; new under precompiles/layerxagent | no |
| LXP_AGENT_OVERFLOW | result out of range | agent functions 0x0901-0x0908: orob 0x0901, clearing 0x0902, pofq 0x0904, streams 0x0906 | agent-functions.md A.0; new under precompiles/layerxagent | no |
| LXP_AGENT_BATCH_TOO_LARGE | batch over the cap (cap set at activation rehearsal) | agent functions 0x0901-0x0908: resolveOffsetBatch, computeClearing, scoreBatch, aggregate | agent-functions.md A.0; new under precompiles/layerxagent | no |
| LXP_AGENT_OROB_ORACLE_PRICE | oraclePrice cannot be zero | agent functions 0x0901-0x0908: orob 0x0901, computeClearing | agent-functions.md A.0; new under precompiles/layerxagent | no |
| LXP_AGENT_OROB_OFFSET_RANGE | offset out of range | agent functions 0x0901-0x0908: orob 0x0901, computeClearing | agent-functions.md A.0; new under precompiles/layerxagent | no |
| LXP_AGENT_CLEARING_LENGTH_MISMATCH | buy or sell arrays length mismatch | agent functions 0x0901-0x0908: computeClearing | agent-functions.md A.0; new under precompiles/layerxagent | no |
| LXP_AGENT_ORACLE_NO_FEEDS | no feeds provided | agent functions 0x0901-0x0908: aggregate | agent-functions.md A.0; new under precompiles/layerxagent | no |
| LXP_AGENT_ORACLE_NO_CONFIDENT_FEEDS | no feeds with non-zero confidence | agent functions 0x0901-0x0908: aggregate | agent-functions.md A.0; new under precompiles/layerxagent | no |
| LXP_AGENT_ORACLE_NOT_SIGNER | caller is not a validator oracle signer | agent functions 0x0901-0x0908: submitPrice | agent-functions.md A.0; new under precompiles/layerxagent | no |
| LXP_AGENT_ORACLE_PRICE | invalid price | agent functions 0x0901-0x0908: submitPrice, aggregate | agent-functions.md A.0; new under precompiles/layerxagent | no |
| LXP_AGENT_ORACLE_CONFIDENCE | invalid confidence | agent functions 0x0901-0x0908: aggregate, submitPrice | agent-functions.md A.0; new under precompiles/layerxagent | no |
| LXP_AGENT_POFQ_LENGTH_MISMATCH | array length mismatch | agent functions 0x0901-0x0908: scoreBatch | agent-functions.md A.0; new under precompiles/layerxagent | no |
| LXP_AGENT_POFQ_DECAY_RANGE | decayBps must be < 10000 | agent functions 0x0901-0x0908: updateRollingScore | agent-functions.md A.0; new under precompiles/layerxagent | no |
| LXP_AGENT_SCHEDULER_JOB_NOT_FOUND | job not found | agent functions 0x0901-0x0908: cancel, reschedule | agent-functions.md A.0; new under precompiles/layerxagent | no |
| LXP_AGENT_SCHEDULER_UNAUTHORIZED | unauthorized | agent functions 0x0901-0x0908: cancel, reschedule | agent-functions.md A.0; new under precompiles/layerxagent | no |
| LXP_AGENT_SCHEDULER_HORIZON_EXCEEDED | horizon exceeded | agent functions 0x0901-0x0908: schedule, reschedule | agent-functions.md A.0; new under precompiles/layerxagent | no |
| LXP_AGENT_SCHEDULER_PAST_BLOCK | target block is in the past | agent functions 0x0901-0x0908: schedule, reschedule | agent-functions.md A.0; new under precompiles/layerxagent | no |
| LXP_AGENT_SCHEDULER_MAX_JOBS | max jobs exceeded | agent functions 0x0901-0x0908: schedule | agent-functions.md A.0; new under precompiles/layerxagent | yes |
| LXP_AGENT_SCHEDULER_DEPOSIT_TOO_LOW | deposit too low | agent functions 0x0901-0x0908: schedule | agent-functions.md A.0; new under precompiles/layerxagent | no |
| LXP_AGENT_SCHEDULER_INVALID_GAS_LIMIT | invalid gas limit | agent functions 0x0901-0x0908: schedule | agent-functions.md A.0; new under precompiles/layerxagent | no |
| LXP_AGENT_SCHEDULER_INVALID_TARGET | invalid target | agent functions 0x0901-0x0908: schedule | agent-functions.md A.0; new under precompiles/layerxagent | no |
| LXP_AGENT_SCHEDULER_JOB_INACTIVE | job inactive | agent functions 0x0901-0x0908: cancel, reschedule | agent-functions.md A.0; new under precompiles/layerxagent | no |
| LXP_AGENT_SCHEDULER_ESCROW | escrow deposit failed | agent functions 0x0901-0x0908: schedule | agent-functions.md A.0; new under precompiles/layerxagent | no |
| LXP_AGENT_STREAMS_INVALID_PAYEE | invalid payee | agent functions 0x0901-0x0908: open | agent-functions.md A.0; new under precompiles/layerxagent | no |
| LXP_AGENT_STREAMS_SELF_PAYMENT | self payment | agent functions 0x0901-0x0908: open | agent-functions.md A.0; new under precompiles/layerxagent | no |
| LXP_AGENT_STREAMS_UNKNOWN_TOKEN | token has no assetId mapping | agent functions 0x0901-0x0908: open | agent-functions.md A.0; new under precompiles/layerxagent | no |
| LXP_AGENT_TEE_UNKNOWN_FAMILY | unknown family | agent functions 0x0901-0x0908: verify, verifyAndExpect | agent-functions.md A.0; new under precompiles/layerxagent | no |
| LXP_AGENT_TEE_NO_ROOTS | no roots loaded | agent functions 0x0901-0x0908: verify, verifyAndExpect | agent-functions.md A.0; new under precompiles/layerxagent | yes |
| LXP_AGENT_TEE_INVALID_QUOTE | invalid quote | agent functions 0x0901-0x0908: verify, verifyAndExpect | agent-functions.md A.0; new under precompiles/layerxagent | no |
| LXP_AGENT_TEE_UNTRUSTED_CHAIN | untrusted chain | agent functions 0x0901-0x0908: verify, verifyAndExpect | agent-functions.md A.0; new under precompiles/layerxagent | no |
| LXP_AGENT_TEE_STALE | attestation stale | agent functions 0x0901-0x0908: verify, verifyAndExpect | agent-functions.md A.0; new under precompiles/layerxagent | no |
| LXP_AGENT_TEE_DEBUG_REFUSED | debug enclave refused | agent functions 0x0901-0x0908: verify, verifyAndExpect | agent-functions.md A.0; new under precompiles/layerxagent | no |
| LXP_AGENT_TEE_REPORT_DATA_MISMATCH | report data mismatch | agent functions 0x0901-0x0908: verifyAndExpect | agent-functions.md A.0; new under precompiles/layerxagent | no |
| LXP_AGENT_TEE_UNAUTHORIZED_UPDATE | unauthorized root update | agent functions 0x0901-0x0908: tee root update (governance) | agent-functions.md A.0; new under precompiles/layerxagent | no |

## (c) Activation
One named plan for the merge; per-method gates read its done height through IsUpgradeActiveAtHeight (sdk/x/upgrade/keeper/keeper.go:296-299) as modules/layerxcustody/keeper/keeper.go:42 does.

| Item | Value | Source |
|---|---|---|
| Plan name | `MergeUpgrade = "v7.0"`, provisional in the v6.9/v6.10/v6.11 pattern; the activation rehearsal may rename it and every gate reads the constant | node/upgrades.go:82-107 |
| Constant home | node/upgrades.go beside `ActivationUpgrade` (:88), `V610Upgrade` (:97), `V611Upgrade` (:107); no entry in node/tags (pattern of v6.9-v6.11, node/upgrades.go:82-107) | node/upgrades.go:82-107 |
| Handler registration | `app.UpgradeKeeper.SetUpgradeHandler(MergeUpgrade, app.runMergeUpgrade)` in RegisterUpgradeHandlers beside :187-193; appended to knownUpgradePlans (:416-418) | node/upgrades.go:117-194, 416-418 |
| Store mounts | store upgrade adding layerxledger, layerxpay, layerxauth, layerxprograms, sequencer stores, applied through the store loader | node/app.go:1307-1325 (existing pattern) |
| Handler effects | run migrations; init the five new modules from genesis params; return every posted guarantor bond and open challenge deposit from layerxanchor (D4); no custody claim drain | decision D4 |
| Precompile serving of new addresses | entries for 0x101A-0x101D and 0x0901-0x0908 added to GetCustomPrecompiles (precompiles/setup.go:63-97) and served only from the plan height; the evm keeper single CustomPrecompileActivation (modules/evm/keeper/keeper.go:100,195-223; set at node/app.go:940 from node/upgrades.go:398) becomes a list of (plan name, address set), each entry read through IsUpgradeActiveAtHeight | modules/evm/keeper/keeper.go:195-223 |
| D18 activation height | the sequencer fork height H_act (set at activation rehearsal) and the leader list live in node config, identical on every validator; a node whose list or H_act differs refuses to start; the merge plan height is set equal to H_act (per sequencer.md c1, which owns it) | sequencer.md c1 (consensus/config/config.go new fields beside :1160-1187); consensus/internal/consensus/types/round_state.go:422 |
| Retire mechanism | retired executors stay registered; each method checks IsUpgradeActiveAtHeight(ctx, MergeUpgrade, ctx.BlockHeight()) and returns Error("LXP_MODULE_DISABLED: <precompile> retired") when true | modules/layerxcustody/keeper/keeper.go:38-46 |
| Anchor store-delete step | after the plan handler has returned every posted guarantor bond and open challenge deposit (D4), a later step deletes the layerxanchor store; from that height the nine anchor reads return LXP_MODULE_DISABLED; the 0x1014 address stays registered; step name and height set at activation rehearsal | modules/layerxcustody/keeper/keeper.go:38-46 gate pattern |

| Address | Methods | Below the plan height | At or above the plan height |
|---|---|---|---|
| 0x1001 bank | `send(address,address,string,uint256,bytes32)` 0x2e4c85ac and its LayerXReceipt log (ledger-auth.md 0-B) | not registered, behaviour as today | live |
| 0x1001 bank | every existing method | as today | as today with LXP_ revert text through the opt-in reason path |
| 0x1004 addr | every existing method | as today | as today with LXP_ revert text through the opt-in reason path |
| 0x1004 addr | `rotateLayerXKey`, `setRecoveryRoot`, `recoverLayerX` | not registered | not registered (D16 out of merge scope) |
| 0x1012 layerxVerify | all five methods | as today | LXP_MODULE_DISABLED |
| 0x1013 layerxCustody | all twenty methods of section (a) | as today | LXP_MODULE_DISABLED |
| 0x1014 layerxAnchor | twelve write methods of section (a) | as today | LXP_MODULE_DISABLED |
| 0x1014 layerxAnchor | nine read methods of section (a) | as today | served until the store-delete step, then LXP_MODULE_DISABLED |
| 0x1015 layerxExchange | `depositMargin`, `depositMarginToken`, `withdrawMargin(bytes32,uint256)`, `placeOrder`, `cancelOrder` (signatures in a2) | intent recording (modules/layerxexchange/keeper/intents.go); old selectors as today | engine execution through modules/layerxexchange/native; `withdrawMargin` is a bank release behind a free-margin check; old selectors of the three changed signatures fall to LXP_PRECOMPILE_UNKNOWN_METHOD |
| 0x1015 layerxExchange | `requestSettlement`, `getIntent` | as today | LXP_MODULE_DISABLED |
| 0x1015 layerxExchange | `intentNonce` | as today | kept: nonce input of the D8 chain-derived order and deposit id |
| 0x1015 layerxExchange | `getMarket`, `getOrder`, `getPosition`, `getMargin` (signatures in a2) | batch/witness proof reads | direct state reads (exchange-launchpad.md E9-E12) |
| 0x1015 layerxExchange | `createPerpsMarket`, `setPerpsHalt`, `pushOracle`, `openPosition`, `increasePosition`, `closePosition`, `fundingTick`, `liquidate`, `adl`, `registerMargin`, `createSpotMarket`, `placeSpotOrder`, `cancelSpotOrder`, `setSpotHalt`, `getPerpsBook`, `getFunding`, `getOracle`, `getDeficit`, `getSpotMarket`, `getSpotOrder`, `getSpotBook`, `getPerpsPositions` | not registered, behaviour as today | live |
| 0x1016 layerxBridge, 0x1018 feeToken | every method | as today | as today with LXP_ revert text |
| 0x1017 launchpad | every existing method; `getConfig()` | as today | as today with LXP_ revert text; `getConfig` Config tuple without protocolFeesPending |
| 0x1017 launchpad | `getProtocolFees(address)` | not registered, behaviour as today | live |
| 0x1019 xweb | `fulfilProgram(bytes)`, `getProgramResult`, `getKernelNetworkId` | not registered, behaviour as today | live |
| 0x1019 xweb | `refund` | as today | origin 1 as today; an origin-2 id returns LXP_XWEB_UNKNOWN_REQUEST |
| 0x1019 xweb | every other method | as today | as today with LXP_ revert text |
| 0x101A-0x101D | every method | address not served | live |
| 0x0901-0x0908 | every method of agent-functions.md A.1, B (selectors in a2) | address not served (calls succeed empty today) | live |
| 0x1002-0x100C, 0x1011 | every method | as today | as today (outside D6: not LayerX lineage) |
