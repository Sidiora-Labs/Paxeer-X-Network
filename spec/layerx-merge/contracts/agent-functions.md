# layerxagent precompiles (0x0901-0x0908)

| Field | Value |
|---|---|
| Precompiles | orob resolver 0x0000000000000000000000000000000000000901, batch clearing 0x…0902, oracle aggregator 0x…0903, pofq scorer 0x…0904, scheduler 0x…0905, payment streams 0x…0906, tee attestor 0x…0907, eip712 helper 0x…0908 (the address table rows 0x0901-0x0908; gateway map human/wallet/gateway/src/precompiles.ts:17-29) |
| Version | 1.0.1 |
| Change control | Changes only by amendment to this contract, recorded with a new version number. |
| Source revisions | repository at 52b672ac4 (read-only); the earlier implementation, specified from, not copied |
| Binding decisions | decision D20 A with amendment 1 (all eight kept and ported as real precompiles; 0x0901-0x0904 re-specified here and implemented anew); 0x0901-0x0904 semantics follow the earlier implementation; decision D6 (Error(string) with LXP_ prefix); decision D3 (authz LayerX-scope grants); decision D2 (derived bank addresses); decision D7 (block-open timestamp); the embedded-wallet attestor set governs the tee attestor and every signer rule; 0x0906 executes through the 0x101B stream keeper (pay.md A.7); fixed per-method gas table (D2 gas table below); gateway ABIs frozen; eip712 pure hashing; tee roots set by governance; scheduler job store in receipt order; 0x0903 reads and writes the 0x1015 exchange oracle store |
| ABI source | method and event signatures from the earlier implementation's abi.json files and Solidity precompile interfaces; gateway ABIs human/wallet/gateway/src/precompiles.ts:50-97 (scheduler), :99-151 (streams), :153-187 (eip712), :189-237 (tee) match them; method id = first 4 bytes of keccak256(signature), selector lookup through MethodById precompiles/common/precompiles.go:68-80 |
| Principal (EVM-AUTH) | msg.sender's 0x1004 DID binding (modules/evm/keeper/layerx_binding.go:18), or an decision D3 authz grantee; delegatecall refused as precompiles/layerxexchange/layerxexchange.go:206 |
| Height gate | every method refuses below the merge plan height (plan name set at activation rehearsal), pattern modules/layerxcustody/keeper/keeper.go:42 |
| Revert form | Error(string) "LXP_<code>: <message>", WithRevertReasons precompiles/common/precompiles.go:53; Go-origin errors carry LXP_AGENT_<FN>_<NAME>; kernel codes only for kernel-origin errors, keeping the kernel name with LXP_ERR_ replaced by LXP_ (include/layerx/lxp_result.h:29-157) |

## A. Methods
Error codes first (A.0), then one row per method; every row's execution function is under precompiles/layerxagent/.

### A.0 Error codes
| Code | Origin (earlier implementation error, or repository file:line) | Used by |
|---|---|---|
| LXP_PRECOMPILE_INPUT_TOO_SHORT | "input too short" in the earlier orob, clearing, pofq and eip712 precompiles; repo precompiles/common/precompiles.go:281 | all |
| LXP_PRECOMPILE_UNKNOWN_METHOD | "unknown method" and MethodById errors in the earlier orob, clearing, pofq and eip712 precompiles; repo precompiles/common/precompiles.go:131 | all |
| LXP_PRECOMPILE_ARG_COUNT | "expected 3 args" in the earlier oracle precompile; "expected N args" in the earlier tee attestor handlers; repo precompiles/common/precompiles.go:213 | all |
| LXP_PRECOMPILE_NON_PAYABLE | repo precompiles/common/precompiles.go:221 | all but schedule |
| LXP_PRECOMPILE_NO_CONTEXT | repo precompiles/common/precompiles.go:125 | all |
| LXP_AGENT_ABI_DECODE | Unpack errors and type assertions of the earlier orob, clearing, pofq, oracle and eip712 precompiles; schedule-argument and streamId decoding of the earlier scheduler and streams precompiles; eip712 chainId nil or negative; tee argument decoding | all |
| LXP_AGENT_READ_ONLY | write under staticcall (the earlier scheduler, streams and oracle precompiles ran setup with IsTransaction) | submitPrice, schedule, cancel, reschedule, open, settle, close, updateRate |
| LXP_AGENT_DELEGATECALL | delegatecall refused (pattern precompiles/layerxexchange/layerxexchange.go:206) | every write method |
| LXP_AGENT_OVERFLOW | every result is range-checked against its output type before packing (orob toOffset int16, pofq scores and weights) | orob, clearing, pofq, streams |
| LXP_AGENT_BATCH_TOO_LARGE | batch above the cap (cap set at activation rehearsal) | resolveOffsetBatch, computeClearing, scoreBatch, aggregate |
| LXP_AGENT_OROB_ORACLE_PRICE | "oraclePrice cannot be zero" in the earlier toOffset; extended to price ≤ 0 on resolveOffset, resolveOffsetBatch, toOffset and computeClearing | 0x0901, computeClearing |
| LXP_AGENT_OROB_OFFSET_RANGE | offsetBps ≤ -10000 refused (it would give a non-positive price); toOffset results outside int16 refused instead of wrapping | 0x0901, computeClearing |
| LXP_AGENT_CLEARING_LENGTH_MISMATCH | "buy arrays length mismatch", "sell arrays length mismatch" in the earlier clearing precompile; unreachable under the struct ABI (D1) | computeClearing |
| LXP_AGENT_ORACLE_NO_FEEDS | "no feeds provided" in the earlier oracle aggregator | aggregate |
| LXP_AGENT_ORACLE_NO_CONFIDENT_FEEDS | "no feeds with non-zero confidence" in the earlier oracle aggregator | aggregate |
| LXP_AGENT_ORACLE_NOT_SIGNER | caller not a permitted oracle signer; ErrNotValidator, ErrInvalidSigner of the earlier oracle module | submitPrice |
| LXP_AGENT_ORACLE_PRICE | ErrInvalidPrice of the earlier oracle module, applied on the precompile path as well as the message path | submitPrice, aggregate (price ≤ 0) |
| LXP_AGENT_ORACLE_CONFIDENCE | ErrInvalidConfidence of the earlier oracle module; also confidence above the cap (set at activation rehearsal), so one feed cannot dominate the median | aggregate, submitPrice |
| LXP_UNKNOWN_FIELD (kernel) | ErrMarketNotSupported, ErrInvalidMarketId of the earlier oracle module → committed read miss src/modules/perps/lx_oracle_read.c:51 | getValidatorPrice |
| LXP_MARKET_HALTED, LXP_NON_CANONICAL (kernel) | committed read src/modules/perps/lx_oracle_read.c:111, :116 | getValidatorPrice |
| retired | ErrInsufficientQuorum and ErrStaleSubmissions of the earlier oracle module: the 0x1015 committed record has no quorum count (include/layerx/lx_oracle.h:86-92) and staleness is enforced at intake (LXP_ORACLE_STALE, exchange-launchpad.md P3) | none |
| 0x1015 intake codes (kernel) | LXP_UNAUTHORIZED_ORACLE, LXP_ORACLE_SEQUENCE, LXP_ORACLE_STALE, LXP_TIMESTAMP_REGRESSION, LXP_ORACLE_BOUNDS, LXP_ORACLE_DEVIATION, LXP_MARKET_HALTED, LXP_NON_CANONICAL (exchange-launchpad.md P3, execute_oracle_push src/modules/perps/lx_perps_dispatch.c:914) | submitPrice |
| LXP_AGENT_POFQ_LENGTH_MISMATCH | "array length mismatch" in the earlier pofq precompile; unreachable under the struct ABI (D1) | scoreBatch |
| LXP_AGENT_POFQ_DECAY_RANGE | "decayBps must be < 10000" in the earlier pofq precompile | updateRollingScore |
| LXP_AGENT_SCHEDULER_JOB_NOT_FOUND | ErrJobNotFound 2 of the earlier scheduler module; "jobId out of uint64 range" | cancel, reschedule |
| LXP_AGENT_SCHEDULER_UNAUTHORIZED | ErrUnauthorized 3 | cancel, reschedule |
| LXP_AGENT_SCHEDULER_HORIZON_EXCEEDED | ErrHorizonExceeded 4 | schedule, reschedule |
| LXP_AGENT_SCHEDULER_PAST_BLOCK | ErrPastBlock 5 | schedule, reschedule |
| LXP_AGENT_SCHEDULER_MAX_JOBS | ErrMaxJobsExceeded 6 | schedule |
| LXP_AGENT_SCHEDULER_DEPOSIT_TOO_LOW | ErrDepositTooLow 7 (and the negative-deposit check, unreachable for msg.value) | schedule |
| LXP_AGENT_SCHEDULER_INVALID_GAS_LIMIT | ErrInvalidGasLimit 8; also gasLimit above the per-job cap (set at activation rehearsal) | schedule |
| LXP_AGENT_SCHEDULER_INVALID_TARGET | ErrInvalidTarget 9 | schedule |
| LXP_AGENT_SCHEDULER_JOB_INACTIVE | ErrJobInactive 10 | cancel, reschedule |
| LXP_AGENT_SCHEDULER_ESCROW | "escrow deposit failed" in the earlier scheduler precompile | schedule |
| LXP_AGENT_STREAMS_INVALID_PAYEE | ErrInvalidPayee 4 of the earlier streams module; also a payee without a 0x1004 binding | open |
| LXP_AGENT_STREAMS_SELF_PAYMENT | ErrSelfPayment 11 | open |
| LXP_AGENT_STREAMS_UNKNOWN_TOKEN | token address with no assetId mapping | open |
| kernel stream codes | ErrStreamNotFound 2 → LXP_UNKNOWN_FIELD (src/modules/stream/lx_stream_open.c:278); ErrUnauthorized 3 → LXP_UNAUTHORIZED_DEBIT; ErrInvalidRate 5, ErrInvalidCap 6, ErrInvalidTime 7 → LXP_NON_CANONICAL; ErrStreamInactive 8 → LXP_STREAM_CLOSED; ErrInsufficientEscrow 9 → the fund-leg ledger code of streamOpen; plus every code pay.md lists for streamOpen, streamSettle, streamClose | 0x0906 |
| retired | ErrMaxStreamsExceeded 10 (MaxStreamsPerPayer 4096): the kernel stream module has no per-payer cap (pay.md A.3) | none |
| LXP_AGENT_TEE_UNKNOWN_FAMILY | ErrUnknownFamily 2 of the earlier attestor module | verify, verifyAndExpect |
| LXP_AGENT_TEE_NO_ROOTS | ErrNoRootsLoaded 3 | verify, verifyAndExpect |
| LXP_AGENT_TEE_INVALID_QUOTE | ErrInvalidQuote 4 | verify, verifyAndExpect |
| LXP_AGENT_TEE_UNTRUSTED_CHAIN | ErrUntrustedChain 5 | verify, verifyAndExpect |
| LXP_AGENT_TEE_STALE | ErrAttestationStale 6; also a timestamp later than block-open time (negative age refused) | verify, verifyAndExpect |
| LXP_AGENT_TEE_DEBUG_REFUSED | ErrDebugRejected 7 | verify, verifyAndExpect |
| LXP_AGENT_TEE_REPORT_DATA_MISMATCH | ErrReportDataMismatch 8 | verifyAndExpect |
| LXP_AGENT_TEE_UNAUTHORIZED_UPDATE | ErrUnauthorizedUpdate 9 (governance-only root update) | F root update |

### A.1 Methods
| Method (selector) | Inputs | Outputs | Authority | Effects | Errors | Kernel execution function | Gas |
|---|---|---|---|---|---|---|---|
| resolveOffset(int256 oraclePrice, int16 offsetBps) (0x454ad3a1), pure | oraclePrice > 0 (price units of the caller's market); -10000 < offsetBps ≤ 32767 | int256 absolutePrice = oraclePrice·(10000+offsetBps)/10000, truncated toward zero (the earlier implementation divides with big.Int Div, Euclidean; equal for positive operands) | none | none; pure helper, never touches the 0x1015 spot book (D3) | INPUT_TOO_SHORT, UNKNOWN_METHOD, ABI_DECODE, OROB_ORACLE_PRICE, OROB_OFFSET_RANGE, OVERFLOW | precompiles/layerxagent/orob/orob.go ResolveOffset | D2 |
| resolveOffsetBatch(int256 oraclePrice, int16[] offsetsBps) (0x3df597e3), pure | as resolveOffset; 1..cap offsets (cap set at activation rehearsal) | int256[] absolutePrices | none | none | as resolveOffset, BATCH_TOO_LARGE | precompiles/layerxagent/orob/orob.go ResolveOffsetBatch | D2 |
| toOffset(int256 oraclePrice, int256 absolutePrice) (0x3f779aef), pure | oraclePrice > 0; absolutePrice > 0 | int16 offsetBps = (absolutePrice−oraclePrice)·10000/oraclePrice, truncated toward zero; result outside int16 refused | none | none | as resolveOffset | precompiles/layerxagent/orob/orob.go ToOffset | D2 |
| computeClearing(int256 oraclePrice, (int16 offsetBps, uint128 size)[] buyOrders, (int16 offsetBps, uint128 size)[] sellOrders) (0x8397e9f8), pure; struct form of the earlier IBatchClearing interface (D1) | oraclePrice > 0; offsets as resolveOffset; buys+sells 0..cap (set at activation rehearsal) | (int16 clearingOffsetBps, int256 clearingPrice, uint256 matchedVolume) | none | none; algorithm of the earlier implementation: candidate levels = unique offsets ascending; demand(l) = Σ size of buys with offset ≥ l; supply(l) = Σ size of sells with offset ≤ l; volume(l) = min; pick the lowest level among the maxima (strict >); price = resolveOffset(oraclePrice, level); empty side or best buy < cheapest sell returns (0, oraclePrice, 0) | INPUT_TOO_SHORT, UNKNOWN_METHOD, ABI_DECODE, OROB_ORACLE_PRICE, OROB_OFFSET_RANGE, BATCH_TOO_LARGE, OVERFLOW | precompiles/layerxagent/clearing/clearing.go ComputeClearing | D2 |
| aggregate((int256 price, uint256 confidence, uint256 timestamp)[] feeds) (0x81097a95), pure | 1..cap feeds; price > 0; confidence ≤ cap (caps set at activation rehearsal); timestamp ignored, as in the earlier implementation | (int256 price, uint256 confidence) | none | none; drop zero-confidence feeds; one feed returned as is; else sort by price, half = floor(Σconf/2), median = first price whose cumulative confidence ≥ half; confidence out = minimum of the kept feeds | ABI_DECODE, BATCH_TOO_LARGE, ORACLE_NO_FEEDS, ORACLE_NO_CONFIDENT_FEEDS, ORACLE_PRICE, ORACLE_CONFIDENCE | precompiles/layerxagent/oracle/oracle.go Aggregate | D2 |
| submitPrice(bytes32 marketId, int256 price, uint256 confidence) (0xdc9c2875), nonpayable | marketId = 0x1015 market id; 0 < price ≤ u128 max; confidence validated, not stored: the 0x1015 observation has no confidence field | bool success = true | the caller's 0x1004 didPublicKey must be in the market's permittedOracleKeys (lx_oracle_key_set_check src/modules/perps/lx_oracle_intake.c:64), which stands in for the ed25519 observation signature of 0x1015 pushOracle (the earlier implementation allowed any registered validator) | runs the 0x1015 oracle intake for one observation of the caller's key (sequence, observedAt from block-open decision D7); writes the 0x1015 committed oracle record; no EVM log (the earlier implementation emitted only a Cosmos event paxoracle_price_submitted) | READ_ONLY, DELEGATECALL, ARG_COUNT, ABI_DECODE, ORACLE_NOT_SIGNER, ORACLE_PRICE, 0x1015 intake codes | precompiles/layerxagent/oracle/oracle.go SubmitPrice, calling the kernel intake execute_oracle_push src/modules/perps/lx_perps_dispatch.c:914 | D2 |
| scoreFill(int256 fillPrice, int256 oraclePrice) (0xe087a6ba), pure | both > 0 | uint256 score = max(0, 1e18 − abs(fill−oracle)·1e18/oracle) | none | none; oraclePrice comes from 0x1015 getOracle (D3) | ABI_DECODE, OROB_ORACLE_PRICE, OVERFLOW | precompiles/layerxagent/pofq/pofq.go ScoreFill | D2 |
| scoreBatch((int256 fillPrice, int256 oraclePrice, uint128 size)[] fills) (0x1b1fba48), pure; struct form of the earlier IPoFQScorer interface (D1) | 0..cap fills (cap set at activation rehearsal) | (uint256 avgScore, uint256 totalVolume); empty input or zero volume gives (0, 0) | none | none; avgScore = Σ score·size / Σ size | ABI_DECODE, BATCH_TOO_LARGE, OROB_ORACLE_PRICE, OVERFLOW | precompiles/layerxagent/pofq/pofq.go ScoreBatch | D2 |
| updateRollingScore(uint256 currentScore, uint256 currentWeight, uint256 newScore, uint256 newWeight, uint16 decayBps) (0xa8f32b0e), pure | decayBps < 10000 | (uint256 updatedScore, uint256 updatedWeight) with w = currentWeight·(10000−decayBps) + newWeight·10000, updatedScore = (currentScore·currentWeight·(10000−decayBps) + newScore·newWeight·10000)/w, updatedWeight = w/10000, so the weight fed back per fill stays in input units | none | none | ABI_DECODE, POFQ_DECAY_RANGE, OVERFLOW | precompiles/layerxagent/pofq/pofq.go UpdateRollingScore | D2 |
| schedule(address target, bytes callData, uint64 executeAtBlock, uint64 gasLimit) (0x849ba0a3), payable | target nonzero; callData ≤ cap (set at activation rehearsal); height < executeAtBlock ≤ height + MaxScheduleHorizon (2,592,000); 0 < gasLimit ≤ per-job cap (set at activation rehearsal); msg.value ≥ gasLimit·baseFee·MinDepositFactor (2) | uint256 jobId (next id, store prefix 0x05) | msg.sender (delegatecall refused); creator = msg.sender; at most MaxJobsPerCreator 1024 active | msg.value moved from the precompile balance to the module escrow; job by id, due index (block BE, id BE), creator index; EVM log JobScheduled(jobId, creator, target, executeAtBlock, gasLimit, deposit) | READ_ONLY, DELEGATECALL, ABI_DECODE, SCHEDULER_INVALID_TARGET, SCHEDULER_INVALID_GAS_LIMIT, SCHEDULER_PAST_BLOCK, SCHEDULER_HORIZON_EXCEEDED, SCHEDULER_MAX_JOBS, SCHEDULER_DEPOSIT_TOO_LOW, SCHEDULER_ESCROW (check order of the earlier implementation) | precompiles/layerxagent/scheduler/scheduler.go Schedule | D2; job execution charged per F |
| cancel(uint256 jobId) (0x40e58ee5), nonpayable | jobId ≤ u64 | none | creator only | full deposit refunded to the creator; job and indexes deleted; EVM log JobCancelled(jobId, creator, refund) | READ_ONLY, DELEGATECALL, ABI_DECODE, SCHEDULER_JOB_NOT_FOUND, SCHEDULER_JOB_INACTIVE, SCHEDULER_UNAUTHORIZED | precompiles/layerxagent/scheduler/scheduler.go Cancel | D2 |
| reschedule(uint256 jobId, uint64 newBlock) (0x08e6f770), nonpayable | newBlock as executeAtBlock | none | creator only | due index moved; EVM log JobRescheduled(jobId, newBlock) | READ_ONLY, DELEGATECALL, ABI_DECODE, SCHEDULER_JOB_NOT_FOUND, SCHEDULER_JOB_INACTIVE, SCHEDULER_UNAUTHORIZED, SCHEDULER_PAST_BLOCK, SCHEDULER_HORIZON_EXCEEDED | precompiles/layerxagent/scheduler/scheduler.go Reschedule | D2 |
| open(address payee, address token, uint256 ratePerSecond, uint64 startTime, uint64 stopTime, uint256 cap) (0x93e5b08b), nonpayable | executes through 0x101B streamOpen (pay.md A.3, A.7); payee 0x1004-bound; token address(0) = native, else mapped to an assetId through GetAssetByDenom / GetAssetByPointer (modules/layerxcustody/keeper/keeper.go:172, :183); ratePerSecond ≤ u128 → rate, rateUnit 1000; startTime·1000 → startMs (0 = block-open ms, decision D7); stopTime·1000 → endMs (0 = open-ended); cap ≤ u128 → totalCap = initialFunding; meterAuthorities empty; the earlier MinDuration of 60 s is not carried, kernel rules apply | uint256 streamId: the uint256 view of the kernel bytes32 streamId (derivation formula, candidate keccak(caller, nonce), set at activation rehearsal) | the account-holder authority kind (LXP_AUTHORITY_OWNER, include/layerx/lxp_authority.h:12) from msg.sender's 0x1004 binding (pay.md); payer := principal (src/modules/stream/lx_stream_execution.h:162); payee ≠ caller | streamOpen mode 1 TIME: payer AGENT_MAIN → streamAccount initialFunding through the kernel fund leg (src/modules/stream/lx_stream_open.c:204); kv "stream:"+streamId; EVM log StreamOpened(streamId, payer, payee, token, ratePerSecond, cap) from 0x0906 | READ_ONLY, DELEGATECALL, ABI_DECODE, STREAMS_INVALID_PAYEE, STREAMS_SELF_PAYMENT, STREAMS_UNKNOWN_TOKEN, OVERFLOW, kernel stream codes | precompiles/layerxagent/streams/streams.go Open → lx_stream_open_execute src/modules/stream/lx_stream_open.c:249 | D2 |
| settle(uint256 streamId) (0x8df82800), nonpayable | streamId as open | uint256 paid | principal == payer or recipient (src/modules/stream/lx_stream_execution.h:257-259); the earlier implementation allowed any caller | streamSettle with a derived idempotencyKey (from (streamId, height, tx index); formula set at activation rehearsal): pays min(unsettled, balance) to the recipient (src/modules/stream/lx_stream_settle.c:95); EVM log StreamSettled(streamId, payee, paid, committed = paid) | READ_ONLY, DELEGATECALL, ABI_DECODE, kernel stream codes | precompiles/layerxagent/streams/streams.go Settle → lx_stream_settle_execute src/modules/stream/lx_stream_settle.c:95 | D2 |
| close(uint256 streamId) (0x0aebeb4e), nonpayable | streamId as open | uint256 finalPaid | payer only (src/modules/stream/lx_stream_lifecycle.c:19-20); the earlier implementation allowed payer or payee | streamClose with a derived idempotencyKey as settle: pays the recipient, refunds the payer, closed record kept (src/modules/stream/lx_stream_lifecycle.c:87); EVM log StreamClosed(streamId, finalPaid, refund) | READ_ONLY, DELEGATECALL, ABI_DECODE, kernel stream codes | precompiles/layerxagent/streams/streams.go Close → lx_stream_close_execute src/modules/stream/lx_stream_lifecycle.c:87 | D2 |
| updateRate(uint256 streamId, uint256 newRate) (0x405abb41), nonpayable | newRate nonzero, ≤ u128 | none | payer only | settle at the old rate to now (accrual lx_stream_accrue include/layerx/lx_stream.h:217), then set the new rate; EVM log StreamRateUpdated(streamId, oldRate, newRate) | READ_ONLY, DELEGATECALL, ABI_DECODE, OVERFLOW, kernel stream codes | precompiles/layerxagent/streams/streams.go UpdateRate → a kernel stream rate-change activity (accrue at the old rate to now, then set the new rate), a required change in the kernel stream module (pay.md) | D2 |
| verify(uint8 family, bytes quote) (0x5fa55ca7), view | family 0 TDX, 1 SEV-SNP, 2 NVIDIA H100, 3 SGX; quote = dev envelope "PXVE\x01", ≤ cap (set at activation rehearsal) | (uint8 family, bytes32 mrtd, bytes32 reportData, uint64 timestamp, bool debug) | none to call; the envelope signer must be in the governance-set root list for the family, and the root list is the embedded-wallet attestor set (human/wallet/gateway/src/attestor/client.ts:533) | none; exact-match root lookup, signature over data[0:95], debug refused unless DebugAllowed (false), 0 ≤ now − timestamp ≤ MaxAttestationAge 600 s, now = block-open seconds (decision D7) | ABI_DECODE, TEE_UNKNOWN_FAMILY, TEE_NO_ROOTS, TEE_INVALID_QUOTE, TEE_UNTRUSTED_CHAIN, TEE_STALE, TEE_DEBUG_REFUSED | precompiles/layerxagent/teeattestor/teeattestor.go Verify | D2 |
| verifyAndExpect(uint8 family, bytes quote, bytes32 expectedReportData) (0x71ded4b3), view | as verify | as verify | as verify | as verify, then reportData == expectedReportData; replay protection rests with the caller through reportData (the envelope nonce is not checked) | verify codes, TEE_REPORT_DATA_MISMATCH | precompiles/layerxagent/teeattestor/teeattestor.go VerifyAndExpect | D2 |
| hashTypedData(bytes32 domainSeparator, bytes32 structHash) (0xd61c3c34), pure | none | bytes32 digest = keccak256(0x19 0x01 ‖ domainSeparator ‖ structHash) | none | none | INPUT_TOO_SHORT, UNKNOWN_METHOD, ABI_DECODE | precompiles/layerxagent/eip712/eip712.go HashTypedData | D2 |
| domainSeparator(string name, string version, uint256 chainId, address verifyingContract) (0xe6d341cd), pure | name and version ≤ cap bytes (cap set at activation rehearsal) | bytes32 = keccak256(typehash ‖ keccak(name) ‖ keccak(version) ‖ chainId ‖ verifyingContract), typehash EIP712Domain(string name,string version,uint256 chainId,address verifyingContract) | none | none | ABI_DECODE, BATCH_TOO_LARGE | precompiles/layerxagent/eip712/eip712.go DomainSeparator | D2 |
| recoverTypedSigner(bytes32 domainSeparator_, bytes32 structHash, bytes signature) (0xac580b5c), view | signature 65 bytes r‖s‖v, v 27/28 normalized | address signer; zero address on length ≠ 65, bad v, high s (low-s check) or failed recovery | none | none | ABI_DECODE | precompiles/layerxagent/eip712/eip712.go RecoverTypedSigner | D2 |

The signer rule for the tee attestor (one attestor signature or the three-of-five threshold of human/wallet/gateway/src/attestor/client.ts:533, and the attestor key type) and the on-chain registration point of the embedded-wallet attestor set (configured for the gateway at human/wallet/gateway/src/env.ts:201-218) are set at activation rehearsal. Vendor-native quote formats (TDX, SEV-SNP, SGX, NVIDIA) are out of scope for this feature: verify accepts the dev envelope signed by the attestor set, and parsing vendor quotes has no kernel operation.

## B. Views
State reads; errors as A.0.

| View (selector) | Outputs | Read helper reproduced | Errors |
|---|---|---|---|
| getValidatorPrice(bytes32 marketId) (0x99812615) | (int256 price = committed u128 price, uint256 quorum = 1, uint256 timestamp = observed_at ms / 1000, unix seconds) | lx_oracle_committed_read src/modules/perps/lx_oracle_read.c:93 on the 0x1015 store (record include/layerx/lx_oracle.h:86-92); same store as exchange-launchpad.md getOracle (src/modules/perps/lx_perps_state.c:105) | LXP_UNKNOWN_FIELD, LXP_MARKET_HALTED, LXP_NON_CANONICAL |
| getJob(uint256 jobId) (0xbf22c457) | (uint256 id, address creator, address target, bytes callData, uint64 executeAtBlock, uint64 gasLimit, uint256 deposit, bool active); zero Job when absent | precompiles/layerxagent/scheduler/scheduler.go GetJob over the job store (F) | ABI_DECODE |
| pending(address creator) (0x5eebea20) | uint256[] active job ids in id order, at most MaxJobsPerCreator 1024 (bounded by that cap) | precompiles/layerxagent/scheduler/scheduler.go Pending over the creator index | ABI_DECODE |
| accrued(uint256 streamId) (0xf52981f4) | uint256 pending accrual at block-open ms (decision D7); 0 when absent | lx_stream_accrue include/layerx/lx_stream.h:217 on a copy of lx_stream_load src/modules/stream/lx_stream_state.c:37 (pay.md A.7) | ABI_DECODE |
| getStream(uint256 streamId) (0x894e9a0d) | (uint256 id, address payer, address payee, address token, uint256 ratePerSecond, uint256 cap, uint64 startTime, uint64 stopTime, uint256 settled, bool active); zero when absent; active = !closed && !paused; times = ms / 1000 | pay.md getStream, lx_stream_load src/modules/stream/lx_stream_state.c:37, with reverse principal → address (GetLayerXDid modules/evm/keeper/layerx_binding.go:18) and assetId → token mapping (modules/layerxcustody/keeper/keeper.go:172) | ABI_DECODE |
| rootOf(uint8 family, uint256 index) (0x01ec24b8) | bytes root; empty when family or index is out of range (the index is not truncated) | precompiles/layerxagent/teeattestor/teeattestor.go RootOf over the governance-set root list | ABI_DECODE |
| rootCount(uint8 family) (0x26f02502) | uint256 | precompiles/layerxagent/teeattestor/teeattestor.go RootCount | ABI_DECODE |

## C. Events
EVM logs emitted by the precompile address; no kernel event constant exists for 0x0901-0x0905, 0x0907, 0x0908.

| Event (topic0) | Fields | Emitted by |
|---|---|---|
| JobScheduled(uint256 indexed jobId, address indexed creator, address target, uint64 executeAtBlock, uint64 gasLimit, uint256 deposit) (0xf946d531…) | as signature | schedule |
| JobCancelled(uint256 indexed jobId, address indexed creator, uint256 refund) (0xa80c76c4…) | as signature | cancel |
| JobRescheduled(uint256 indexed jobId, uint64 newBlock) (0xb060fcde…) | as signature | reschedule |
| JobExecuted(uint256 indexed jobId, address indexed creator, uint64 gasUsed, uint256 refund) (0x2052025f…) | as signature | F end-block hook; Cosmos typed event only (no transaction receipt at end block) |
| JobFailed(uint256 indexed jobId, address indexed creator, string reason) (0x0bf83276…) | reason = deterministic revert string truncated to a cap (set at activation rehearsal) | F end-block hook; Cosmos typed event only |
| StreamOpened(uint256 indexed streamId, address indexed payer, address indexed payee, address token, uint256 ratePerSecond, uint256 cap) (0xc87f2fb1…) | as signature | open, after kernel StreamOpened (pay.md C) |
| StreamSettled(uint256 indexed streamId, address indexed payee, uint256 paid, uint256 committed) (0x0ad296c7…) | committed = paid | settle, after kernel StreamSettled |
| StreamClosed(uint256 indexed streamId, uint256 finalPaid, uint256 refund) (0xbd03ee89…) | as signature | close, after kernel StreamClosed |
| StreamRateUpdated(uint256 indexed streamId, uint256 oldRate, uint256 newRate) (0xd3372d2a…) | as signature | updateRate |
| AttestationVerified(uint8 indexed family, bytes32 indexed mrtd, bytes32 reportData, uint64 timestamp, bool debug) (0x936ac9b7…) | as signature | not emitted: verify and verifyAndExpect are view (the earlier implementation does not emit it either); kept in the frozen ABI |

## D. Framework fit
ABI choices, overlaps with the exchange part, and the gas table.

| Item | Rule | Reason |
|---|---|---|
| D1 struct ABI | computeClearing and scoreBatch use the struct form of the Solidity interfaces (selectors 0x8397e9f8, 0x1b1fba48), not the parallel-array abi.json form (0xb6a4c9ae, 0xc6f7591b) | the only caller in the earlier implementation, its matching engine, encodes 0x8397e9f8, so the served selector matches the selector the caller encodes |
| D3 overlap orob, clearing vs 0x1015 spot | 0x0901 and 0x0902 are pure price helpers; they never read or write the 0x1015 spot book; spot orders settle only through 0x1015 placeSpotOrder (exchange-launchpad.md S2, src/modules/spot/lx_spot_market.c) at absolute prices, so an offset order resolves through resolveOffset before placement | one book, one settlement path |
| D3 overlap oracle vs 0x1015 | 0x0903 holds no price store: getValidatorPrice reads and submitPrice writes the 0x1015 exchange oracle store, the same store the program oracle_read reads (tests/test_programs_oracle_read.c:467-475) | one oracle state |
| D3 overlap pofq vs perps oracle intake | the oraclePrice input of scoreFill and scoreBatch should be the 0x1015 getOracle committed price written by the perps oracle intake (src/modules/perps/lx_oracle_intake.c:64); 0x0904 does not read it itself | pure helper; caller supplies the reference price |
| D4 streams via 0x101B | 0x0906 keeps its ABI (frozen) and holds no stream store; every write calls the 0x101B stream keeper (pay.md A.3, A.7) | one stream state |
| D5 times | scheduler uses block heights, as the earlier implementation; streams convert seconds ↔ ms at the boundary; tee and accrued use block-open time (decision D7) | kernel stream times are ms (pay.md conventions) |

### D2. Gas table
Fixed per-method EVM gas in the DefaultGasCost pattern (precompiles/common/precompiles.go:286-292): read = 1000 + 3 x len(input), write = 2000 + 30 x len(input), heavy = read or write + fixed surcharge; unknown selector 3000 (UnknownMethodCallGas precompiles/common/precompiles.go:22); every number below is provisional, final numbers set at activation rehearsal; the per-element constants of the earlier implementation (orob 5 / 3 per element; pofq 5 / 3 / 8; clearing 50 + 3 per order) sit far below cost and are not used.

| Method | Gas class | Provisional gas |
|---|---|---|
| resolveOffset, resolveOffsetBatch, toOffset | read | 1000, + 3/byte |
| computeClearing | heavy | 6000, + 3/byte (the batch cap bounds the levels x orders loop) |
| aggregate | read | 1000, + 3/byte (the earlier implementation charged a fixed 20 + 5x5 regardless of feeds) |
| submitPrice | heavy | 30000, + 30/byte |
| scoreFill, scoreBatch, updateRollingScore | read | 1000, + 3/byte |
| schedule | heavy | 25000, + 30/byte (job store and escrow) |
| cancel, reschedule | write | 20000 each, + 30/byte |
| open | write | 50000, + 30/byte (pay.md D.2 streamOpen) |
| settle | write | 60000, + 30/byte (pay.md D.2 streamSettle) |
| close | write | 80000, + 30/byte (pay.md D.2 streamClose) |
| updateRate | write | 60000, + 30/byte |
| verify, verifyAndExpect | heavy | 46000, + 3/byte (the earlier implementation charged 30000 + 15000) |
| hashTypedData, domainSeparator | read | 1000, + 3/byte (the per-byte term replaces the flat gas of the earlier implementation) |
| recoverTypedSigner | heavy | 4000, + 3/byte (ecrecover 3000) |
| every B view | read | 1000, + 3/byte |

## E. Parity vectors
The earlier implementation has no Go test for any of the eight precompiles or for its scheduler, streams, attestor and oracle modules; its Solidity tests mock the precompiles and its scripts run against a live chain, so they are not carried. Go tests and golden vectors (including domainSeparator and hashTypedData digest equality) are written with precompiles/layerxagent for every method in A.1 and view in B. Repository tests that already cover the kernel paths:

| Method | Test or golden path | What it asserts |
|---|---|---|
| open, settle, close (0x101B path) | tests/modules/test_stream_open.c, tests/modules/test_stream_settle.c, tests/modules/test_stream_lifecycle.c, tests/modules/test_stream_accrue.c | kernel open validation and funding, settle, lifecycle and close refund, accrual |
| getValidatorPrice | tests/test_programs_oracle_read.c:467-475 | committed read found, unknown, halted |
| submitPrice | tests/modules/test_oracle_intake.c | 0x1015 intake acceptance and refusals |
| streams settle route | human/wallet/gateway/test/agent.test.ts:267 | gateway route auth only; not a precompile vector |

## F. Internal (not user methods)
Stores, end-block hook, root updates.

| Item | Rule | Source |
|---|---|---|
| scheduler store | modules/layerxagent keeper with prefixes due (block BE, id BE), creator index, job by id, params, next id; jobs run in receipt order = ascending jobId within a height | store layout of the earlier scheduler module |
| scheduler end-block hook | at each height run the due jobs in id order, at most a per-block cap (set at activation rehearsal), the rest roll to the next height; each job calls target with callData as the creator in a cached context under the job's own gasLimit at the base fee; fee = gasUsed·baseFee from the deposit to the fee collector on success and on failure; refund the rest to the creator; a refund failure halts nothing and is recorded in JobFailed | end-block hook of the earlier scheduler module, with gas accounting and fee return as stated |
| scheduler params | GasScheduleBase/PerByte/Cancel/Reschedule/View (replaced by D2), MaxJobsPerCreator 1024, MaxScheduleHorizon 2,592,000 blocks, MinDepositFactor 2, per-block cap and per-job gasLimit cap set at activation rehearsal | params of the earlier scheduler module |
| tee roots | governance-only update of the per-family root list, authority check as the earlier attestor module; the list holds the embedded-wallet attestor set public keys; per-family gas params of the earlier implementation (unused) dropped for D2 | root store of the earlier attestor module |
| oracle params | StalenessThreshold 15 blocks and MinQuorum 1 of the earlier oracle module retired; 0x1015 market oracle config governs (exchange-launchpad.md) | params of the earlier oracle module |
| pause | write methods refuse while the governance pause covers the layerxagent scope (scope id set at activation rehearsal) | pay.md F pause row pattern |

## Changes relative to the earlier implementation
Target behaviour of this contract where it differs from the earlier implementation.

| Area | This contract |
|---|---|
| Selector agreement between ABI and caller | computeClearing and scoreBatch are served at the struct-form selectors 0x8397e9f8 and 0x1b1fba48 that the caller encodes (D1); the parallel-array length codes are unreachable |
| Bounded quality weight | aggregate refuses confidence above a cap so one feed cannot dominate the median; updateRollingScore returns updatedWeight = w/10000 so the weight stays bounded across fills; every result is range-checked (LXP_AGENT_OVERFLOW) |
| Scheduler gas accounting and fee return | each job runs under its own gasLimit at the base fee; gasUsed·baseFee goes to the fee collector on success and on failure, and the rest of the deposit returns to the creator (F) |
| Stream rate accrual | updateRate settles at the old rate to now before the new rate applies (accrual include/layerx/lx_stream.h:217) |
| Stream funding | open funds the stream through the kernel fund leg of streamOpen (src/modules/stream/lx_stream_open.c:204), so the funded amount is the amount moved; insufficient funding returns the fund-leg ledger code |
| Stream authority | settle by payer or recipient (src/modules/stream/lx_stream_execution.h:257-259); close by payer only (src/modules/stream/lx_stream_lifecycle.c:19-20) |
| Attestor timestamp and index bounds | a timestamp later than block-open time is refused (TEE_STALE); rootOf with family or index out of range returns empty, with no truncation of the index |
| Low-s signature check | recoverTypedSigner returns the zero address for a high-s signature |
| Delegatecall guard | delegatecall refused on every write method (pattern precompiles/layerxexchange/layerxexchange.go:206) |
| Overflow range checks | results are checked against their output type before packing; toOffset refuses results outside int16 |
| Batch caps | resolveOffsetBatch, computeClearing, scoreBatch and aggregate refuse inputs above a cap (LXP_AGENT_BATCH_TOO_LARGE; caps set at activation rehearsal) |
| Oracle price > 0 | every orob and clearing method and scoreFill refuse oraclePrice ≤ 0; submitPrice validates price on the precompile path |
| Offset range | offsetBps ≤ -10000 refused (LXP_AGENT_OROB_OFFSET_RANGE) |
| Oracle signer | submitPrice requires the caller's 0x1004 key in the market's permittedOracleKeys (src/modules/perps/lx_oracle_intake.c:64) |
| Deterministic revert reason in JobFailed | the reason is the deterministic revert string, truncated to a cap |
| Per-block job cap | the end-block hook runs at most a capped number of due jobs per height; the rest roll forward |
| Scheduler EVM logs | schedule, cancel and reschedule emit EVM logs (C) |
| pending bound | pending returns at most MaxJobsPerCreator 1024 ids |
| Gas | per-method gas follows the D2 table with per-byte terms (precompiles/common/precompiles.go:286-292) |
