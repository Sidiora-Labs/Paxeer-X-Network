# Ledger and auth: layerxledger and layerxauth precompile contracts

| Field | Value |
|---|---|
| Name | layerxledger (proposed 0x000000000000000000000000000000000000101A) and layerxauth (proposed 0x000000000000000000000000000000000000101C) |
| Version | 1.0.3 |
| Change control | Changes only by amendment to this contract, recorded with a new version number. |
| Binding decisions | D2 A (derived bank address per DID and sub-account suffix; parity target is kernel sub-account parent/kind), D3 A (sdk/x/authz with a LayerX-scope Authorization, all eight authority kinds kept), D6 A (Error(string) with prefix LXP_<code>), D10 A (per-request draw lives at 0x101A); typed payExact is a send with a payment reference; EVM gas is a fixed per-method gas table |
| Bring-up | a bring-up at the activation height; no account-state migration is required |
| Source revision | repository main at 52b672ac4, read-only |
| Activation | 0x101A and 0x101C are served from the merge plan height; the evm keeper activation read at modules/evm/keeper/keeper.go:195-223 becomes a list of (plan, address set) |

## 0 Shared rules (both precompiles)

One line: identity resolution, account-to-address mapping, encoding and error form used by every method below.

| Rule | Statement | Source |
|---|---|---|
| Caller to DID | caller EVM address -> didPublicKey via addr 0x1004 getLayerXDid(caller); no binding -> revert LXP_UNAUTHORIZED_DEBIT (ledger) / LXP_AUTH_SCOPE (auth) | precompiles/addr/addr.go:391; kernel authority check src/modules/asset/lx_asset_execution.h:176-182, src/modules/governance/lxp_governance.c:183-250 |
| DID string | did = "did:layerx:" + lowercase hex(didPublicKey) | modules/evm/types/layerx_binding.go:21, :58 |
| Kernel DID id (actor) | actor = hash_domain(LXP_DOMAIN_DID_ID, u16be(len(did)) \|\| did) | src/state/lxp_identity.c:46-56 |
| Kernel account id | id = H("LX:ACCOUNT:v1" \|\| u32be(len(name)) \|\| name) | src/ledger/lx_account_id.c:196-226; Go mirror modules/evm/types/layerx_binding.go:69 |
| Main account | name "agent:<did>:main", kind AGENT_MAIN=1; D2 suffix "main" | include/layerx/lxp_ledger.h:20-35; src/modules/asset/lx_asset_registry.c:66-99 |
| Asset sub-account | name "agent:<did>:asset:<hex64 assetId>", kind AGENT_ASSET=14, parent = holder DID via lx_account_owner_did; D2 suffix "asset:<hex64 assetId>" | src/protocol/lxp_module_ctx.c:617-685; src/ledger/lx_account_read.c:6-37 |
| Issuance account | name "module:asset:value:<hex id>", id = H("LX:ACCOUNT:v1"\|\|u32be(79)\|\|"asset:<hex64>:issuance"), kind MODULE_VALUE=13; module-held, no DID; bank: one module account per asset at address.Module("layerx", issuance account id) holds issuance, supply is bank supply, mint/burn through the module's minter permission | src/ledger/lx_account_id.c:228-256; src/protocol/lxp_module_ctx.c:565-615; sdk/types/address/hash.go:63-66 |
| Account id -> address | every bytes32 accountId argument is the kernel id above; the precompile recomputes it from (did, suffix); an accountId that does not equal the recomputed id for the caller's DID reverts as the kernel does (source_matches_actor); bank address = address.Module("layerx", kernel 32-byte account id) | src/modules/asset/lx_asset_registry.c:66-99; sdk/types/address/hash.go:63-66 |
| Source match | source account must be agent:<actor did>:main or agent:<actor did>:asset:<hex of that account's asset_id> | src/modules/asset/lx_asset_registry.c:66-99 |
| EVM authority | the precompile builds the kernel account-holder authority (LXP_AUTHORITY_OWNER, include/layerx/lxp_authority.h:12) from msg.sender's 0x1004 binding: verified_key = bound DID key, actor = kernel DID id, principal = kernel account id; the kernel activity ed25519 signature and any envelope or receiver authorization ed25519 signature are not verified on the EVM path (the EVM transaction signature substitutes); every other kernel check is kept | src/modules/asset/lx_asset_execution.h:175-184; src/ledger/lxp_send.c:281-285; src/ledger/lxp_receive.c:356-362 |
| Authz on ledger | a D3 authz grantee may not call any layerxledger method: the kernel requires the account-holder authority kind, so the LayerX-scope Authorization refuses every layerxledger method and applies only to modules other than layerxledger; revert LXP_UNAUTHORIZED_DEBIT as the kernel | src/modules/asset/lx_asset_execution.h:176; src/modules/asset/lx_asset_registry.c:147 |
| Authz grantee address | authz grantee = the 0x1004-bound address of the grantee ed25519 key; an unbound grantee key is refused (LXP_AUTH_SCOPE) | precompiles/addr/addr.go:391 |
| EVM gas | EVM gas = the fixed per-method table D-G in section D of each precompile; kernel gas units and fee ordinals in the "Gas or fee rule" columns are kernel-internal parity facts, never charged to the EVM caller | precompiles/common/precompiles.go:286-292 |
| Integer encoding | kernel u128 amounts -> uint128; u64 -> uint64; big-endian on the kernel wire | src/codec/lxp_codec.c:62-159 |
| Kernel bytes field | u32be length prefix | src/codec/lxp_codec.c:184-201 |
| Error form | revert Error(string) "LXP_<CODE>" where CODE is the LXP_ERR_<CODE> name; fatal LXP_FATAL_SUPPLY_MISMATCH -> "LXP_FATAL_SUPPLY_MISMATCH" | include/layerx/lxp_result.h; D6 |
| Revert plumbing | executor error is returned through Precompile.Run with WithRevertReasons, which packs RevertReason(err) | precompiles/common/precompiles.go:53-66, :82-111 |
| ABI | abi.json embedded via go:embed and MustGetABI; selector = first 4 bytes, MethodById | precompiles/common/precompiles.go:122-143, :278-305; precompiles/layerxexchange/layerxexchange.go:81 |

## 0-B bank 0x1001 `send` LayerX context-hash overload

One line: frozen signature of the bank overload used by unified-account transfers; the 4-argument `send` is unchanged.

| Item | Signature (selector or topic0) | Rule | Source |
|---|---|---|---|
| Method | send(address fromAddress, address toAddress, string denom, uint256 amount, bytes32 contextHash) returns (bool success) (0x2e4c85ac) | overload of send(address,address,string,uint256) (0x5c05961b, precompiles/bank/Bank.sol:12-17, precompiles/bank/abi.json); same checks and bank move as that method (non-payable, not from staticcall, caller is the denom's ERC20 native pointer, zero amount short-circuits, MsgSend through the bank msg server, precompiles/bank/bank.go:134-185); contextHash is carried into the log only; served from the merge plan height; gas as the dynamic-gas bank precompile (precompiles/bank/bank.go:67, :102) | precompiles/bank/bank.go:22, :79, :114, :134-185 |
| Log | LayerXReceipt(address indexed from, address indexed to, string denom, uint256 amount, bytes32 contextHash) (0x4b11729a809ec729868d9ba54f3192aa4179fe63668e9f742aa9b25de21de65f) | emitted by 0x1001 once per successful overload call with a nonzero amount; the 4-argument send emits no log (no log call in precompiles/bank/bank.go:134-185) | precompiles/bank/bank.go:134-185 |

# Precompile 1: layerxledger (0x101A)

One line: kernel asset module, LXP_MODULE_ASSET=1, ABI 1, activity types 0x00010001-0x0001000b (include/layerx/lx_asset.h:21-31; iface src/modules/asset/lx_asset_registry.c:574-583); WITHDRAW 0x00010009 is not registered on layerxledger, and there is no withdraw or receive method on 0x101A.

## A Methods

One line: every row follows dispatch decode -> validate -> execute (src/protocol/lxp_kernel.c:2299-2340) after ledger admission (src/protocol/lxp_kernel.c:427-510); every method requires the account-holder authority kind (src/modules/asset/lx_asset_execution.h:176, src/modules/asset/lx_asset_registry.c:147, :200), built per shared rule EVM authority. Below, "caller bound to the DID" means the caller's DID resolved via 0x1004, and "kind LXP_AUTH_OWNER" is the account-holder authorization kind of the kernel wire (include/layerx/lxp_ledger.h:108).

| Solidity signature | Inputs (type, unit, max length) | Outputs | Authority rule | Effects | Errors | Kernel execution function | Gas or fee rule |
|---|---|---|---|---|---|---|---|
| register(bytes32 assetId, bytes32 salt, string symbol, string name, uint8 decimals, uint128 supplyCap, uint8 issuerKind, bytes custodyReference) returns (bytes32) | assetId bytes32; salt bytes32; symbol ASCII 1-16 bytes; name UTF-8 1-32 bytes; decimals 0-38; supplyCap uint128 base units (0 = unbounded, stored issuance balance 2^128-1); issuerKind 1 native or 2 paxeer_custody; custodyReference 0-128 bytes, must be empty for kind 1 (src/modules/asset/lx_asset_decode.c:38-82) | assetId | caller bound to the DID via 0x1004; issuer_did = actor; kind 1 requires assetId == H("LX:ASSET:v1"\|\|actor\|\|salt) (src/modules/asset/lx_asset_execution.h:189-234) | key "asset:"\|\|assetId (38 bytes, record value version 0x0003, src/modules/asset/lx_asset_registry.c:711-747); issuance account created with balance supplyCap or 2^128-1 (src/protocol/lxp_module_ctx.c:565-615); event AssetRegistered(bytes32 assetId) | LXP_NON_CANONICAL, LXP_ASSET_ALREADY_REGISTERED, LXP_ARENA_EXHAUSTED, LXP_ASSET_MISMATCH, LXP_UNAUTHORIZED_DEBIT, LXP_FATAL_SUPPLY_MISMATCH | asset_execute_typed REGISTER branch src/modules/asset/lx_asset_execution.h:189-234; lx_asset_register src/modules/asset/lx_asset_registry.c:625-649 | kernel gas payload_length+1 (src/modules/asset/lx_asset_registry.c:424); fee lxp_fee_compute asset price for ordinal 1 (src/protocol/lxp_fee.c:185-215) |
| pause(bytes32 assetId) | assetId bytes32 (payload 34 bytes, src/modules/asset/lx_asset_registry.c:33-40) | none | caller bound to the DID via 0x1004 and actor == asset issuer_did (src/modules/asset/lx_asset_execution.h:235-248) | asset record paused flag set; event AssetPaused(bytes32 assetId) | LXP_NON_CANONICAL, LXP_UNAUTHORIZED_DEBIT, LXP_PAUSED_SCOPE, LXP_ASSET_MISMATCH | asset_execute_typed PAUSE branch src/modules/asset/lx_asset_execution.h:235-248 | payload_length+1; fee ordinal 2 |
| unpause(bytes32 assetId) | as pause | none | as pause | paused flag cleared; event AssetUnpaused(bytes32 assetId) | LXP_NON_CANONICAL, LXP_UNAUTHORIZED_DEBIT, LXP_PAUSED_SCOPE, LXP_ASSET_MISMATCH | asset_execute_typed UNPAUSE branch src/modules/asset/lx_asset_execution.h:235-248 | payload_length+1; fee ordinal 3 |
| openAccount(bytes32 assetId) returns (bytes32 accountId) | assetId bytes32 (payload 34 bytes, src/modules/asset/lx_asset_decode.c:84-91) | accountId = H("LX:ACCOUNT:v1"\|\|len\|\|"agent:<did>:asset:<hex64 assetId>"); bank address D2 derive(did, "asset:<hex64 assetId>") | caller bound to the DID via 0x1004; account authority_key = caller's verified DID key; principal account sequence recorded (src/protocol/lxp_kernel.c:451-475) | account record kind AGENT_ASSET=14, parent DID, authority_key (src/protocol/lxp_module_ctx.c:617-685); ledger receipt op 4 from principal to new account; event AccountOpened(bytes32 accountId) | LXP_NON_CANONICAL, LXP_ASSET_PAUSED, LXP_ASSET_MISMATCH, LXP_CONTEXT_MISMATCH (account exists), LXP_UNAUTHORIZED_DEBIT (DID longer than 435 bytes) | asset_execute_typed ACCOUNT_OPEN branch src/modules/asset/lx_asset_execution.h:249-285 | payload_length+1; fee ordinal 4 |
| send(bytes sendEnvelope) | sendEnvelope = kernel send wire (see D-1): from, to, asset, amount uint128, sequence uint64, idempotencyKey bytes32, expiresAt uint64 ms, contextHash, 0-8 conditions, LXP_AUTH_OWNER authorization block (src/ledger/lxp_send.c:180-230); its ed25519 signature is not verified on the EVM path, controller/public_key/network_id/protocol_version/signed_context_hash checks kept (src/ledger/lxp_send.c:255-260, :276-277) | none | caller bound to the DID via 0x1004; envelope auth kind LXP_AUTH_OWNER, controller == from, public_key == from authority key == caller verified key, activity idempotency == envelope idempotency, from != to, from matches actor (src/modules/asset/lx_asset_registry.c:119-183; src/ledger/lxp_send.c:243-286) | one leg from -> to amount (reason PAYMENT); from sequence advanced; monetary transfer set (src/modules/asset/lx_asset_transfer.c:51) -> Transfer log; ledger receipt op 5; supply rebind (src/protocol/lxp_module_ctx.c:105-130); bank: MsgSend D2(from) -> D2(to) | LXP_MALFORMED_SEND, LXP_UNKNOWN_AUTHORITY_KIND, LXP_UNAUTHORIZED_DEBIT, LXP_SEQUENCE_EXHAUSTED, LXP_SEQUENCE_MISMATCH, LXP_EXPIRED, LXP_CONDITION_UNMET, LXP_CONTEXT_MISMATCH, LXP_BAD_SIGNATURE, LXP_ASSET_PAUSED, LXP_ASSET_MISMATCH, LXP_BALANCE_BYPASS, LXP_UNKNOWN_ACCOUNT_NAMESPACE, LXP_INVALID_AMOUNT, LXP_ACCOUNT_FROZEN, LXP_INSUFFICIENT_BALANCE, LXP_UNDERFLOW, LXP_OVERFLOW, LXP_SEQUENCE_REUSED, LXP_IDEMPOTENT_REPLAY (kernel receipt store, src/protocol/lxp_kernel.c:7091-7101), LXP_FATAL_SUPPLY_MISMATCH | module_execute_impl SEND path src/modules/asset/lx_asset_registry.c:429-514 -> lx_asset_send_execute src/modules/asset/lx_asset_transfer.c:54 | payload_length+1; fee ordinal 5 |
| draw(bytes32 from, bytes32 assetId, uint128 amount, bytes32 grantId, uint64 receiverSequence, bytes32 idempotencyKey, bytes32 contextHash) | typed form of the lxp_receive fields (include/layerx/lxp_ledger.h:216-227): from = payer account id; assetId; amount uint128 base units > 0, USD 0.001 per request (D10); grantId; receiverSequence uint64 == recipient next_sequence (src/ledger/lxp_receive.c:350-352); idempotencyKey == activity idempotency key; contextHash == hash_context_value(purpose_hash [\|\| reference_hash]) of the grant (purpose_check src/ledger/lxp_receive.c:365-381). Derived, not passed: to := stored grant recipient; payer_grant := stored grant by grantId (grant_load src/modules/asset/lx_asset_execution.h:81-104); receiver_authorization := {LXP_AUTH_OWNER, controller = to, public_key = bound key, signed_context_hash = contextHash, network_id, protocol_version}, no signature | none | caller bound to the DID via 0x1004; to matches actor (source_matches_actor), receiver kind LXP_AUTH_OWNER, public_key == verified key, idempotencyKey == activity key, from != to (src/modules/asset/lx_asset_execution.h:332-336); grant from/recipient/asset/per-draw max match (src/ledger/lxp_receive.c:423-428) | grant draw recorded (drawn_total, drawn_this_period, window_start; invoice_settled when the grant has a reference) on key "grant:"\|\|grantId (lxp_grant_draw_record src/ledger/lxp_grant_store.c:123-156; src/ledger/lxp_receive.c:455); one leg from -> to reason PAYMENT, sequence account = to (src/modules/asset/lx_asset_execution.h:369-370); asset pause refused in the lxp_apply_transfer_set precondition (src/ledger/lxp_apply.c:273, paused copied src/modules/asset/lx_asset_registry.c:877); logs Transfer and PaymentDrawn; bank: MsgSend address.Module("layerx", from) -> address.Module("layerx", to) | LXP_UNAUTHORIZED_DEBIT, LXP_MALFORMED_RECEIVE, LXP_NO_PAYER_GRANT, LXP_GRANT_SCOPE_VIOLATION, LXP_GRANT_REVOKED, LXP_GRANT_EXPIRED, LXP_GRANT_EXHAUSTED, LXP_ZERO_AMOUNT, LXP_PURPOSE_MISMATCH, LXP_INVOICE_ALREADY_SETTLED, LXP_SEQUENCE_MISMATCH, LXP_SEQUENCE_EXHAUSTED, LXP_CONTEXT_MISMATCH, LXP_UNKNOWN_AUTHORITY_KIND, LXP_BAD_SIGNATURE (stored grant re-verify, src/ledger/lxp_grant_store.c:72-100), LXP_ASSET_PAUSED, LXP_ASSET_MISMATCH, LXP_INSUFFICIENT_BALANCE, LXP_OVERFLOW, LXP_UNDERFLOW, LXP_ARENA_EXHAUSTED, LXP_IDEMPOTENT_REPLAY (src/protocol/lxp_kernel.c:7091-7101), LXP_FATAL_SUPPLY_MISMATCH | LX_ASSET_RECEIVE 0x00010006 (include/layerx/lx_asset.h:26) -> asset_execute_typed RECEIVE branch src/modules/asset/lx_asset_execution.h:316-373 -> lxp_receive_execute src/ledger/lxp_receive.c:383-467 -> lxp_grant_draw_record src/ledger/lxp_grant_store.c:123-156; sole entry to RECEIVE | payload_length+1; fee ordinal 6; D10 draw sender class min gas price at most 0.067 gwei, enforced by the sequencer |
| payExact(bytes32 from, bytes32 to, bytes32 assetId, uint128 amount, uint64 expiresAt, bytes32 paymentReference) | typed form of the lxp_send fields (include/layerx/lxp_ledger.h:138-150): from = caller account holding assetId (main or asset sub-account); to; assetId; amount uint128 base units > 0; expiresAt uint64 ms >= batch timestamp (src/ledger/lxp_send.c:265); paymentReference = the send idempotency_key, the only free 32-byte field of a send, == activity idempotency key (src/modules/asset/lx_asset_registry.c:149-150). Derived, not passed: sequence := from next_sequence (src/ledger/lxp_send.c:264); contextHash := send_context = hash_context_value(from\|\|to\|\|asset\|\|amount16\|\|idempotency_key) (src/modules/asset/lx_asset_registry.c:101-115, :177-181); conditions empty; authorization := {LXP_AUTH_OWNER, controller = from, public_key = bound key, signed_context_hash = contextHash, network_id, protocol_version}, no signature | none | caller bound to the DID via 0x1004; from matches actor (src/modules/asset/lx_asset_registry.c:176), kind LXP_AUTH_OWNER, public_key == verified key, from != to (src/modules/asset/lx_asset_registry.c:147-153) | as send: one leg from -> to reason PAYMENT; from sequence advanced; ledger receipt op 5 with context_hash (src/modules/asset/lx_asset_registry.c:490-513); logs Transfer and PaymentExact; bank: MsgSend address.Module("layerx", from) -> address.Module("layerx", to) | LXP_UNKNOWN_AUTHORITY_KIND, LXP_UNAUTHORIZED_DEBIT, LXP_SEQUENCE_EXHAUSTED, LXP_EXPIRED, LXP_ASSET_PAUSED, LXP_ASSET_MISMATCH, LXP_BALANCE_BYPASS, LXP_UNKNOWN_ACCOUNT_NAMESPACE, LXP_INVALID_AMOUNT, LXP_ACCOUNT_FROZEN, LXP_INSUFFICIENT_BALANCE, LXP_UNDERFLOW, LXP_OVERFLOW, LXP_SEQUENCE_REUSED, LXP_IDEMPOTENT_REPLAY (src/protocol/lxp_kernel.c:7091-7101), LXP_FATAL_SUPPLY_MISMATCH | LX_ASSET_SEND 0x00010005: module_execute_impl SEND path src/modules/asset/lx_asset_registry.c:429-514 -> lx_asset_send_execute src/modules/asset/lx_asset_transfer.c:54 | payload_length+1; fee ordinal 5 |
| issueGrant(bytes payerGrant) returns (bytes32 grantId) | payerGrant = kernel payer grant wire, exactly 346 bytes (see D-3): grantId 32, from 32, recipient 32, asset 32, perDrawMax uint128 16, allowance uint128 16, recurring 1, windowLength uint64 8, expiration uint64 8, purposeHash 32, referenceHash presence 1 and value 32, revocationSequence uint64 8, publicKey 32, signature 64 (src/ledger/lxp_receive.c:168-197) | grantId | caller bound to the DID via 0x1004; grant.from matches actor and asset; grant public_key == caller verified key (src/modules/asset/lx_asset_execution.h:374-393) | key "grant:"\|\|grantId value = grant encoding \|\| drawn_total16 \|\| drawn_this_period16 \|\| window_start8 \|\| revoked_at8 \|\| revoked1 \|\| invoice_settled1 (src/modules/asset/lx_asset_execution.h:64-79); event PayerGrantIssued(bytes32 grantId) | LXP_NON_CANONICAL, LXP_SEQUENCE_REUSED, LXP_ASSET_PAUSED, LXP_UNAUTHORIZED_DEBIT, LXP_ASSET_MISMATCH, LXP_MALFORMED_GRANT, LXP_BAD_SIGNATURE | asset_execute_typed GRANT_ISSUE branch src/modules/asset/lx_asset_execution.h:374-393; lxp_verify_payer_grant src/ledger/lxp_grant_store.c:72-100 | payload_length+1; fee ordinal 7 |
| revokeGrant(bytes32 grantId, uint64 revocationSequence) | grantId bytes32; revocationSequence uint64 (payload 42 bytes, src/modules/asset/lx_asset_decode.c:107-119) | none | caller bound to the DID via 0x1004; grant.from account matches actor and its authority key; revocationSequence == activity account_sequence (src/modules/asset/lx_asset_execution.h:394-408) | grant state revoked=1, revoked_at = global sequence; event PayerGrantRevoked(bytes32 grantId) | LXP_NON_CANONICAL, LXP_NO_PAYER_GRANT, LXP_UNAUTHORIZED_DEBIT, LXP_STALE_REVOCATION | asset_execute_typed GRANT_REVOKE branch src/modules/asset/lx_asset_execution.h:394-408 | payload_length+1; fee ordinal 8 |
| mint(bytes32 assetId, bytes32 accountId, uint128 amount) | assetId; accountId (AGENT_ASSET of assetId; any DID); amount uint128 > 0 (payload 82 bytes, src/modules/asset/lx_asset_decode.c:93-105) | none | caller bound to the DID via 0x1004 and actor == asset issuer_did (src/modules/asset/lx_asset_execution.h:286-315) | leg issuance -> accountId (reason PAYMENT); total_units += amount; bank: issuance module account -> address.Module("layerx", accountId) through the minter permission | LXP_NON_CANONICAL, LXP_INVALID_AMOUNT, LXP_ASSET_MISMATCH, LXP_UNAUTHORIZED_DEBIT, LXP_INSUFFICIENT_BALANCE (cap), LXP_FATAL_SUPPLY_MISMATCH, LXP_ASSET_PAUSED, LXP_OVERFLOW | asset_execute_typed MINT branch src/modules/asset/lx_asset_execution.h:286-315; asset_transfer src/modules/asset/lx_asset_execution.h:106-162 | payload_length+1; fee ordinal 10 |
| burn(bytes32 assetId, bytes32 accountId, uint128 amount) | as mint | none | caller bound to the DID via 0x1004; accountId matches actor (source_matches_actor) and account authority_key == caller verified key (src/modules/asset/lx_asset_execution.h:286-315) | leg accountId -> issuance; total_units -= amount; bank: address.Module("layerx", accountId) -> issuance module account, burned through the minter permission | LXP_NON_CANONICAL, LXP_INVALID_AMOUNT, LXP_ASSET_MISMATCH, LXP_UNAUTHORIZED_DEBIT, LXP_INSUFFICIENT_BALANCE, LXP_UNDERFLOW, LXP_FATAL_SUPPLY_MISMATCH | asset_execute_typed BURN branch src/modules/asset/lx_asset_execution.h:286-315 | payload_length+1; fee ordinal 11 |

draw and payExact are both kept: draw is RECEIVE 0x00010006 and payExact is a typed SEND. The draw caller form is the 7-field form above; pay.md A.5 (reference only) cites the same form.

## B Views

One line: read methods; no state written; gas per table D-G.

| Solidity signature | Returns | Kernel read helper |
|---|---|---|
| getAsset(bytes32 assetId) returns (string symbol, string name, uint8 decimals, uint128 supplyCap, uint8 issuerKind, bytes custodyReference, bytes32 issuerDid, bool paused, uint128 totalUnits) | lx_asset_record fields | lx_asset_lookup include/layerx/lx_asset.h:242; lx_asset_record_decode src/modules/asset/lx_asset_registry.c:749-792; record struct include/layerx/lx_asset.h:87-103 |
| balanceOf(bytes32 accountId, bytes32 assetId) returns (uint128) | kernel balance of accountId | lx_asset_balance_get include/layerx/lx_asset.h:263 |
| totalUnits(bytes32 assetId) returns (uint128) | circulating units | lx_asset_total_units include/layerx/lx_asset.h:273 |
| accountId(bytes32 didPublicKey, bytes32 assetId) returns (bytes32) | assetId zero -> main account id, else asset sub-account id | lx_account_id_from_string src/ledger/lx_account_id.c:196-226; modules/evm/types/layerx_binding.go:69 |
| accountOwner(bytes32 accountId) returns (bytes32 didId) | holder DID of an agent account | lx_account_owner_did src/ledger/lx_account_read.c:6-37 |
| nextSequence(bytes32 didPublicKey, bytes32 assetId) returns (uint64) (0xce2f921f) | next_sequence of the account named as in accountId (assetId zero -> main, else asset sub-account): the value the send envelope `sequence` and the draw `receiverSequence` must equal (src/ledger/lxp_send.c:264, src/ledger/lxp_receive.c:350-352); unknown account reverts with the lookup code | lx_account_lookup src/ledger/lx_account_registry.c:551-573 (declared include/layerx/lxp_ledger.h:340-343) over the account name and lx_account_id_from_string src/ledger/lx_account_id.c:196-226; field lx_account.next_sequence include/layerx/lxp_ledger.h:57 |
| getPayerGrant(bytes32 grantId) returns (bytes grant, uint128 drawnTotal, uint128 drawnThisPeriod, uint64 windowStart, uint64 revokedAt, bool revoked, bool invoiceSettled) | grant state; carries the drawn totals, so no separate drawn view | grant_load src/modules/asset/lx_asset_execution.h:81-104 |

## C Events

| Event signature | Fields | Kernel event constant |
|---|---|---|
| AssetRegistered(bytes32 indexed assetId) | assetId | event type 1, body assetId32, src/modules/asset/lx_asset_execution.h:189-234 |
| AssetPaused(bytes32 indexed assetId) | assetId | event type 2, src/modules/asset/lx_asset_execution.h:235-248 |
| AssetUnpaused(bytes32 indexed assetId) | assetId | event type 3, src/modules/asset/lx_asset_execution.h:235-248 |
| AccountOpened(bytes32 indexed accountId) | accountId | event type 4, src/modules/asset/lx_asset_execution.h:249-285 |
| Transfer(bytes32 indexed from, bytes32 indexed to, bytes32 indexed assetId, uint128 amount, uint8 reason) | one per leg of send, payExact, draw, mint, burn | effect LXP_EFFECT_TRANSFER from lxp_ctx_emit_monetary_transfer_set src/protocol/lxp_module_ctx.c:2187-2213; emitted for send src/modules/asset/lx_asset_transfer.c:51, for draw/mint/burn src/modules/asset/lx_asset_execution.h:145 |
| PayerGrantIssued(bytes32 indexed grantId) | grantId | event type 7, src/modules/asset/lx_asset_execution.h:374-393 |
| PayerGrantRevoked(bytes32 indexed grantId) | grantId | event type 8, src/modules/asset/lx_asset_execution.h:394-408 |
| PaymentDrawn(bytes32 indexed grantId, bytes32 indexed from, bytes32 indexed to, bytes32 assetId, uint128 amount, bytes32 idempotencyKey, bytes32 contextHash) | the applied draw: lxp_receive fields after a successful RECEIVE; idempotencyKey is the draw argument, non-indexed; the signed receipt effect commits sha256(data), so the key is authenticated by the receipt | precompile log, no kernel event constant (no PaymentDrawn in the tree); data from include/layerx/lxp_ledger.h:216-227 and the transfer leg src/modules/asset/lx_asset_execution.h:369-370 |
| PaymentExact(bytes32 indexed from, bytes32 indexed to, bytes32 indexed paymentReference, bytes32 assetId, uint128 amount) | the applied payExact send; paymentReference = send idempotency_key | precompile log, no kernel event constant; data from include/layerx/lxp_ledger.h:138-150 and the transfer leg src/modules/asset/lx_asset_transfer.c:42-51 |

## D Framework fit

| Item | Passed as | Reason |
|---|---|---|
| D-1 send envelope | bytes, kernel wire tag 0x5301, 10 fields, variable condition list (0-8 of kind u8 + ts u64), 167-byte authorization | ed25519 signature is over the canonical kernel bytes (src/ledger/lxp_send.c:180-286); ABI re-encoding would alter the signed preimage |
| D-3 payer grant | bytes, kernel grant wire | grantId = hash_authority(grant authorization message) and ed25519 under AUTHORITY_HASH domain (src/ledger/lxp_grant_store.c:72-100) |
| D-4 kernel signature over the activity | not passed | kernel checks an ed25519 activity signature by the DID key (src/modules/asset/lx_asset_execution.h:184); on the EVM path the 0x1004 binding substitutes |
| D-6 ILxPay | ABI subset file | ILxPay = the layerxledger ABI subset issueGrant, revokeGrant, draw, payExact at 0x101A for x402 clients; no chain change |

D-G Gas table: fixed per method, DefaultGasCost pattern (precompiles/common/precompiles.go:286-292, KVGasConfig sdk/store/types/gas.go:349-358) evaluated at the method's maximum ABI calldata; read = 1000 + 3 x bytes, write = 2000 + 30 x bytes, heavy = write class with variable-length input or a signature re-verify; the numbers below are provisional (final numbers set at activation rehearsal).

| Method | Gas class | Provisional gas |
|---|---|---|
| register | heavy | 18440 |
| pause | write | 3080 |
| unpause | write | 3080 |
| openAccount | write | 3080 |
| send | heavy | 17480 |
| draw | heavy | 8840 |
| payExact | write | 7880 |
| issueGrant | heavy | 16520 |
| revokeGrant | write | 4040 |
| mint | write | 5000 |
| burn | write | 5000 |
| getAsset | read | 1108 |
| balanceOf | read | 1204 |
| totalUnits | read | 1108 |
| accountId | read | 1204 |
| accountOwner | read | 1108 |
| nextSequence | read | 1204 |
| getPayerGrant | read | 1108 |

## E Parity vectors

| Method | Test path | What it asserts |
|---|---|---|
| register, pause, unpause, mint, burn, revokeGrant | tests/modules/test_asset_registry.c | pause dispatch refuses ops with ASSET_PAUSED, supply binding dispatch, ASSET_ALREADY_REGISTERED on re-register |
| all 11 | tests/modules/test_asset_payloads.h | canonical payload builders for every asset activity type |
| all 11 | tests/test_fees.c | per-ordinal asset fee schedule |
| send, payExact | tests/test_fees_v4.c | lxp_fee_compute SEND 0 (tests/test_fees_v4.c:93) |
| all 11 | tests/test_genesis_manifest.c | asset activity types present in the genesis module table |
| register, openAccount, mint, send | tests/test_state_commitment_transition.c | state commitment after asset ops |
| send | tests/ledger/test_send.c | send decode, validate, signature |
| draw | tests/ledger/test_receive.c | grant and receive signatures, draw rules |
| draw | tests/test_gateway_receive.c | end-to-end receive via gateway |
| send, payExact | tests/modules/test_asset_transfer.c | transfer leg validation |
| pause, send | tests/daemon/lxp_test_asset_inventory.c | daemon asset inventory |
| send | tests/state/lxp_test_idempotency.c | idempotency of send |
| send | tests/test_receipt_offline.c | offline receipt verification |
| accountId | tests/ledger/test_account_id.c | account id derivation and the retired issuance account rename |
| openAccount, issueGrant | to add: focused C execute test for ACCOUNT_OPEN and GRANT_ISSUE plus replay cases, added before the precompile, then re-targeted through the precompile | account record, event 4, receipt op 4; grant record, event 7 |

## F Internal

| Hook | Behaviour | Source |
|---|---|---|
| Fee | validation fee = base_fee + asset_prices[ordinal] (fee schedule v>=2), computed at admission | src/protocol/lxp_fee.c:7, :185-215; src/protocol/lxp_kernel.c:415 |
| Gas | module_validate charges payload_length+1; genesis charges manifest_length | src/modules/asset/lx_asset_registry.c:334, :424 |
| Pause | per-asset paused flag checked by every value op, draw included | src/modules/asset/lx_asset_transfer.c:7-34; src/modules/asset/lx_asset_execution.h:235-248; src/ledger/lxp_apply.c:273 |
| Epoch | begin and end hook module_epoch charges 1 gas, no state | src/modules/asset/lx_asset_registry.c:560-566, :580 |
| Supply binding | after every op, issuance + total_units must equal initial supply, else LXP_FATAL_SUPPLY_MISMATCH | src/protocol/lxp_module_ctx.c:105-130 |
| Admission | ACCOUNT_OPEN records principal sequence | src/protocol/lxp_kernel.c:427-510 |
| Host store | the shared cgo host store keeps module values under 0x01, the last sequence at 0x02, identity sequences under 0x03, idempotency records under 0x04, module blobs under 0x05 and kernel accounts under 0x06 (key 0x06 \|\| account id, value lx_account_record_encode); the host asset runtime declares the binding authority that replaces the activity, send envelope and receiver authorization ed25519 checks | modules/lxphost/ctx.go; modules/lxphost/host.go; src/state/lxp_snapshot.c |

# Precompile 2: layerxauth (0x101C)

One line: governance module grant sub-ops 5, 6, 8 (types 0x0007xxxx, iface src/modules/governance/lxp_governance.c:658-672) plus authority resolution in src/state/lxp_authority.c; payload prefix 0x71 \|\| ordinal \|\| sub \|\| fields (src/modules/governance/lxp_governance.c:103-181).

## A Methods

One line: every method requires protocol_version 3, the account-holder authority kind (LXP_AUTHORITY_OWNER, include/layerx/lxp_authority.h:12), actor DID, and identity state primary key == verified key (src/modules/governance/lxp_governance.c:183-250, :538-642); session and delegated authorities reach kernel checks by the precompile resolving the authz grant into the kernel authority of the matching kind before execute, grantor DID == grantee DID (src/state/lxp_authority.c:780-782); MULTISIG, TIMELOCK and BUDGET_ALLOWANCE outside sub-op 8 are issued by authz MsgGrant with the LayerX-scope Authorization carrying the kind, ESCROW and PROTOCOL_MODULE stay module-internal.

| Solidity signature | Inputs (type, unit, max length) | Outputs | Authority rule | Effects | Errors | Kernel execution function | Gas or fee rule |
|---|---|---|---|---|---|---|---|
| grantSession(bytes grant, uint64 expirySequence, bytes32 actionKey) returns (bytes32 grantId) | grant = kernel authority grant encoding, max 1024 bytes, kind SESSION_KEY=2 (see D-7); expirySequence uint64 > global sequence; actionKey bytes32 (sub-op 5, sub 1, fields 3) | grantId = hash_authority(grant encoding) (src/state/lxp_authority.c:263-280) | caller bound to the DID via 0x1004; grantor == grantee == actor; grantor_revocation_sequence == identity state REVOCATION; module_mask subset of 0x3fe; zero grantor signature (src/modules/governance/lxp_governance.c:370-455) | key 5\|\|grantId = grant body; key 0x15\|\|grantId = 209-byte summary "LXGS2"; identity state SEQUENCE advanced; events SessionGranted(summary), GrantBody chunks, IdentityState; D3: authz Grant(granter=D2(did,"main"), grantee=session key address, LayerXScopeAuthorization) | LXP_NON_CANONICAL, LXP_UNKNOWN_AUTHORITY_KIND, LXP_AUTH_REVOKED, LXP_AUTH_SCOPE (expiry <= global sequence, zero action key, grantor/grantee != actor, rev seq mismatch, not_after <= now, mask outside 0x3fe, non-zero signature), LXP_SEQUENCE_REUSED (same grant body under a new idempotency key: key 5\|\|grantId exists, src/modules/governance/lxp_governance.c:424-426), LXP_IDEMPOTENT_REPLAY (activity idempotency key already used by the actor DID: the kernel returns the stored receipt before execute, src/protocol/lxp_kernel.c:7091-7101, key H_context(u32be(len did)\|\|did\|\|key) src/state/lxp_idempotency.c:8-23, :123-155; actionKey is a summary field, not the idempotency key, src/modules/governance/lxp_governance.c:390, :433), LXP_MALFORMED_GRANT, LXP_GAS_EXHAUSTED | governance execute sub-op 5 src/modules/governance/lxp_governance.c:370-455 | kernel gas = payload length (src/modules/governance/lxp_governance.c:249) |
| grantSessionSuccessor(bytes grant, uint64 expirySequence, bytes32 actionKey, bytes32 predecessorId, bytes32 expectedCommitment) returns (bytes32 grantId) | as grantSession plus predecessorId, expectedCommitment (sub-op 5, sub 2, fields 5) | grantId | as grantSession; predecessor fee record inherited by successor key (src/modules/governance/lxp_governance.c:323-368) | as grantSession plus inherited fee record; event GrantBody chunks under 0x7125 | as grantSession plus LXP_CONTEXT_MISMATCH (expected commitment, src/state/lxp_authority.c:969-989) | session_inherit_fee src/modules/governance/lxp_governance.c:323-368; sub-op 5 src/modules/governance/lxp_governance.c:370-455 | payload length |
| grantCapability(bytes grant) returns (bytes32 grantId) | grant = kernel authority grant encoding, kind DELEGATED_CAPABILITY=3 or BUDGET_ALLOWANCE=4, max 1024 bytes (sub-op 8, sub 1, fields 1) | grantId | caller bound to the DID via 0x1004; grantor == grantee == actor; grant key != caller verified key; revocation sequence matches; not_after != max and > now; zero signature; scope valid (src/modules/governance/lxp_governance.c:457-536) | key 5\|\|grantId = body; events CapabilityGranted(grantId), GrantBody chunks; D3: authz Grant with LayerXScopeAuthorization carrying module_mask, ordinal range, asset, per-activity/total/period caps, purpose, not_before/not_after | LXP_NON_CANONICAL, LXP_AUTH_SCOPE (kind not 3/4, grantor/grantee != actor, key == verified key, rev seq mismatch, not_after max or <= now, revoked, non-zero signature), LXP_SEQUENCE_REUSED (grant already stored, src/modules/governance/lxp_governance.c:522-523), LXP_IDEMPOTENT_REPLAY (as grantSession, src/protocol/lxp_kernel.c:7091-7101), LXP_MALFORMED_GRANT (src/state/lxp_authority.c:138-191) | issue_grant src/modules/governance/lxp_governance.c:492-536; grant_scope_validate src/modules/governance/lxp_governance.c:457-490 | payload length |
| revokeGrant(bytes32 grantId, uint8 reason, uint64 sequence) | grantId; reason 1-5; sequence == current global sequence (sub-op 6, sub 0, fields 3, payload 45 bytes) | none | caller bound to the DID via 0x1004; stored grant's grantor == actor (src/modules/governance/lxp_governance.c:616-637) | key 6\|\|grantId = 41-byte revocation record; identity state REVOCATION = sequence, which invalidates every grant of the grantor via is_live (src/state/lxp_authority.c:1122-1146); event GrantRevoked(grantId, reason, sequence); D3: revocation epoch in the LayerX-scope Authorization set to sequence; Accept refuses grants issued under an older epoch | LXP_NON_CANONICAL, LXP_UNKNOWN_FIELD (no grant at 5\|\|grantId), LXP_AUTH_SCOPE (grantor != actor, sequence != global sequence, reason outside 1-5), LXP_AUTH_REVOKED | governance execute sub-op 6 src/modules/governance/lxp_governance.c:616-637 | payload length |

## B Views

| Solidity signature | Returns | Kernel read helper |
|---|---|---|
| resultByIdempotency(bytes32 didId, bytes32 idempotencyKey) returns (bytes receipt) | stored receipt for the actor-scoped key H_context(u32be(len did)\|\|did\|\|key) | lxp_idempotency key and lookup src/state/lxp_idempotency.c:8-59; kernel receipt lookup src/protocol/lxp_kernel.c:7091-7101 |
| getGrant(bytes32 grantId) returns (bytes grant, bool revoked, uint64 revokedAt) | stored grant encoding and revocation | lxp_authority_grant_lookup src/state/lxp_authority.c:691-718; lxp_authority_grant_load src/state/lxp_authority.c:720-742; lxp_grant_decode src/state/lxp_authority.c:416-512 |
| isLive(bytes32 grantId, uint64 sequence, uint64 timestampMs) returns (bool live, string code) | liveness with LXP_NOT_YET_VALID / LXP_AUTH_EXPIRED / LXP_AUTH_NOT_MATURE / LXP_AUTH_REVOKED | lxp_authority_is_live src/state/lxp_authority.c:1122-1146 |
| sessionKey(bytes32 grantId) returns (bytes summary) | 209-byte LXGS2 summary at key 0x15\|\|grantId | written at src/modules/governance/lxp_governance.c:440-446; read through an exported C read helper for the 0x15 LXGS2 summary (required change), no raw-key reads from Go |
| capabilityScope(bytes32 grantId) returns (uint64 moduleMask, uint16 ordinalMin, uint16 ordinalMax, bytes32 assetId, uint128 maxPerActivity, uint128 maxTotal, uint128 spentTotal, uint64 periodLength, uint128 maxPerPeriod, uint128 spentThisPeriod, uint64 periodStart, bytes32 purposeHash) | scope struct | include/layerx/lxp_authority.h:27-47; lxp_authority_check_scope src/state/lxp_authority.c:341-360 |
| checkActivity(bytes32 grantId, uint32 activityType, bytes32 assetId, uint128 amount) returns (string code) | LXP_OK or the refusal | lxp_authority_resolve src/state/lxp_authority.c:362-405; check_debit src/state/lxp_authority.c:885-898 |

## C Events

| Event signature | Fields | Kernel event constant |
|---|---|---|
| SessionGranted(bytes32 indexed grantId, bytes summary) | 209-byte LXGS2 summary | 0x7145 src/modules/governance/lxp_governance.c:446 |
| GrantBody(bytes32 indexed grantId, uint8 sub, uint8 chunk, bytes body) | grant body in 256-byte chunks | 0x7105 / 0x7125 (session), 0x7108 / 0x7128 (capability) src/modules/governance/lxp_governance.c:446-455, :527-536 |
| CapabilityGranted(bytes32 indexed grantId) | grantId | 0x7148 src/modules/governance/lxp_governance.c:527 |
| GrantRevoked(bytes32 indexed grantId, bytes record) | 41-byte revocation record (grantId, reason, sequence) | 0x7106 src/modules/governance/lxp_governance.c:634 |
| IdentityStateUpdated(bytes32 indexed didId, bytes state) | 223-byte LXGI1 identity state | 0x7110 src/modules/governance/lxp_governance.c:538-642 |

## D Framework fit

| Item | Passed as | Reason |
|---|---|---|
| D-7 authority grant | bytes, kernel encoding header 0x2001, version 1-4, u32-prefixed byte fields, optional fee budget (v2), authentication-only (v3), signers (v4) | grantId = hash_authority(exact encoding) and canonical re-encode check (src/state/lxp_authority.c:193-280; src/modules/governance/lxp_governance.c:370-536) |
| D-8 summary and identity state | bytes | fixed 209/223-byte kernel records kept byte-identical |
| D-9 multisig signers/approvals | bytes inside grant v4 | up to 8 signers of 32 bytes plus approvals (include/layerx/lxp_authority.h:27-47; src/state/lxp_authority.c:90-136) |

D-G Gas table: same rule as layerxledger D-G; the numbers below are provisional (final numbers set at activation rehearsal).

| Method | Gas class | Provisional gas |
|---|---|---|
| grantSession | heavy | 36680 |
| grantSessionSuccessor | heavy | 38600 |
| grantCapability | heavy | 34760 |
| revokeGrant | write | 5000 |
| resultByIdempotency | read | 1204 |
| getGrant | read | 1108 |
| isLive | read | 1300 |
| sessionKey | read | 1108 |
| capabilityScope | read | 1108 |
| checkActivity | read | 1396 |

## E Parity vectors

| Method | Test path | What it asserts |
|---|---|---|
| grantCapability, revokeGrant | tests/daemon/lxp_test_grant_issuance.c | event 0x7148 on issue, 0x7106 on revoke, SEQUENCE_REUSED duplicates, AUTH_REVOKED after revoke, delegate cannot issue |
| grantSession, grantSessionSuccessor | tests/fixtures/authority/native-sessions (authentication, fee, legacy, replacement), consumed by human/crates/layerx-intents/tests/native_session.rs | golden activity, activity id, receipt per session variant |
| grantCapability | tests/fixtures/authority/native-fee-grants (asset-mismatch, budget, capability, period-bound, total-bound), consumed by agent/crates/layerx-crypto/tests/native_fee_grant.rs | golden grant bytes, id, activity, receipt |
| grantSession, grantSessionSuccessor | tests/daemon/lxp_test_metered_allowance.c | metered session issue and session fee state |
| getGrant, encoding | tests/test_authority_multisig.c | grant encodings byte-identical, AUTH_THRESHOLD_UNMET, AUTH_NOT_MATURE |
| checkActivity | tests/state/lxp_test_authority_resolve.c | authority resolve per kind |
| capabilityScope | tests/state/lxp_test_allowance.c | allowance scope and spend |
| all | tests/test_fees_v4.c | governance activity fee |
| capabilityScope | tests/daemon/lxp_test_daemon_allowance.c | daemon allowance charge |
| revokeGrant | to add: focused C execute test and replay case for governance sub-op 6, added before the precompile | revocation record, event 0x7106, REVOCATION bump |

## F Internal

| Hook | Behaviour | Source |
|---|---|---|
| Gas | governance validate charges payload length | src/modules/governance/lxp_governance.c:249 |
| Epoch | governance epoch hook is a no-op | src/modules/governance/lxp_governance.c:648-653 |
| Revocation epoch | revokeGrant bumps grantor REVOCATION; all prior grants fail is_live; reproduced by the LayerX-scope Authorization revocation epoch | src/modules/governance/lxp_governance.c:616-637; src/state/lxp_authority.c:1122-1146 |
| Grant id | hash_authority over canonical encoding | src/state/lxp_authority.c:263-280 |
| Record tags | GRANT=5, REVOCATION=6, CHARGE=7, FEE=8, keys 33 bytes; summary 0x15 | src/state/lxp_authority.c:407-414 |
| Idempotency retention | idempotency entries are pruned by a retention window over the global sequence, in C and in the keeper alike, so the 8192-entry capacity check that returns LXP_ERR_ARENA_EXHAUSTED (src/state/lxp_idempotency.c:107-108) is not reached by expired entries (required change) | src/state/lxp_idempotency.c:107-108 |
