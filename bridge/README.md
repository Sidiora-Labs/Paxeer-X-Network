# Paxeer X Network bridge

The Paxeer X Network bridge is lock and release in both directions: a foreign chain locks a deposit in the bridge's vault and Paxeer X Network releases the bridged denom through the layerxBridge precompile at `0x0000000000000000000000000000000000001016` against a threshold of attestor signatures, and a burn of that denom on Paxeer X Network is attested so the foreign vault releases what it holds. The default pair on every chain is PAX against that chain's native coin, and SID - Sidiora, the second official coin - bridges between Paxeer X Network and Solana, its foreign home.

This page is the operator runbook: what to deploy, in what order, with which environment variables, how the Paxeer side is configured through governance, and what must read back before the bridge is opened. The Paxeer side is the `layerxbridge` module in `modules/layerxbridge` and its precompile in `precompiles/layerxbridge`. The source is [the Paxeer X Network repository](https://github.com/Sidiora-Labs/Paxeer-X-Network).

---

## What is here

| Path | What it is |
| --- | --- |
| `bridge/evm` | the Foundry project of `PaxeerXVault`, one source deployed as identical bytecode to every EVM chain |
| `bridge/evm/chains/<name>` | one `config.json` and one page per EVM chain |
| `bridge/solana` | the Solana custody program, a raw `solana-program` crate |
| `bridge/solana/admin` | `paxeer-x-bridge-solana-admin`, the program's admin client, a member of the `bridge/solana` Cargo workspace |
| `bridge/solana/chains/solana` | the Solana `config.json` and its page |
| `bridge/deploy` | the configuration schema (`bridge/deploy/chainconfig`), the proposal generator, the attestor-set manifest, the deploy and verify scripts, the post-deploy checklist and their offline tests under `bridge/deploy/tests` |
| `bridge/vectors` | the Solana handle derivation and the pinned digest vectors |
| `bridge/ATTESTATION-SOLANA.md` | how Solana keys, mints and transactions enter the attestation digests |
| `modules/layerxbridge/ATTESTATION.md` | the byte-exact specification of both preimages and the signature rules |

## The chains

| Chain | Chain id | Pair | Page |
| --- | --- | --- | --- |
| `ethereum` | `1` | PAX against ETH | `bridge/evm/chains/ethereum/README.md` |
| `base` | `8453` | PAX against ETH | `bridge/evm/chains/base/README.md` |
| `arbitrum` | `42161` | PAX against ETH | `bridge/evm/chains/arbitrum/README.md` |
| `optimism` | `10` | PAX against ETH | `bridge/evm/chains/optimism/README.md` |
| `bnb` | `56` | PAX against BNB | `bridge/evm/chains/bnb/README.md` |
| `polygon` | `137` | PAX against POL | `bridge/evm/chains/polygon/README.md` |
| `avalanche` | `43114` | PAX against AVAX | `bridge/evm/chains/avalanche/README.md` |
| `hyperevm` | `999` | PAX against HYPE | `bridge/evm/chains/hyperevm/README.md` |
| `solana` | `91600046870081` | PAX against SOL, and SID | `bridge/solana/chains/solana/README.md` |

The native coin is the first asset of every configuration and is always registered: `0x0000000000000000000000000000000000000000` with eighteen decimals on an EVM chain, deposited through `depositNative`, and the wrapped SOL mint `So11111111111111111111111111111111111111112` with nine decimals on Solana. Every chain differs from the others only in its configuration file.

## Tools

| Tool | Needed by |
| --- | --- |
| `git` | `bridge/evm/bootstrap-libs.sh` |
| `jq`, `forge`, `cast` | `bridge/deploy/deploy-evm-chain.sh`, `bridge/deploy/verify-evm-chain.sh` |
| `jq`, `python3`, `cast`, `sha256sum` and the pinned Solana toolchain | `bridge/deploy/deploy-solana-program.sh` |
| `go` | the proposal generator under `bridge/deploy/proposals` |
| `cargo` | the Solana admin client under `bridge/solana/admin` |
| `jq`, `curl`, `cast`, `go`, `base64`, `od` | `bridge/deploy/checklist.sh` |

## Fill in the configuration

Every committed configuration carries values nobody has filled in, and every tool refuses them, naming the file and the field. Nothing defaults.

- `owner` is `PLACEHOLDER:owner` in every configuration: the vault owner address on an EVM chain, the program owner's base58 key on Solana.
- `attestors` are `PLACEHOLDER:attestor-1` to `PLACEHOLDER:attestor-5` in every configuration, at a threshold of `3`. There is one attestor set for the whole bridge: fill in the same five secp256k1 addresses, strictly ascending, everywhere.
- `bridge/deploy/attestors.json` is the attestor-set manifest, the one committed record of the attestor addresses and the threshold. It carries the repeated-byte addresses `0x1111111111111111111111111111111111111111` to `0x5555555555555555555555555555555555555555`, which the proposal generator refuses; it also refuses a configuration whose set or threshold differs from the manifest.
- `big_blocks.acknowledged` is `false` in `bridge/evm/chains/hyperevm/config.json`. The HyperEVM page says what to do before setting it to `true`.
- `solana.program_id` is `PLACEHOLDER:program-id` until the first Solana deployment prints the real id.

## Order of operations

### 1. Bootstrap the libraries

```sh
bash bridge/evm/bootstrap-libs.sh
```

Clones `forge-std` `v1.9.6` and `openzeppelin-contracts` `v5.3.0` into the `lib` directory beside `bridge/evm/foundry.toml`, which is never committed. A second run with both libraries already at their tag and unmodified does nothing; an edited checkout is replaced. The deploy script runs this itself before it builds.

### 2. Deploy each EVM chain

| Variable | Holds |
| --- | --- |
| the variable in `environment.rpc_url`, `PAXEER_BRIDGE_<CHAIN>_RPC_URL` | the chain's endpoint |
| the variable in `environment.deploy_key`, `PAXEER_BRIDGE_<CHAIN>_DEPLOY_KEY` | the deployer key |
| `PAXEER_BRIDGE_DEPLOYMENT_RECORD` | the path the deployment record is written to; its directory must exist and be writable |
| `PAXEER_BRIDGE_EVM_CHAINS_ROOT` | optional; a chains root in place of `bridge/evm/chains` |

```sh
bash bridge/deploy/deploy-evm-chain.sh --preflight <chain>
bash bridge/deploy/deploy-evm-chain.sh <chain>
```

`--preflight` runs every check that needs no endpoint - the configuration, the variables it names and the tools - and stops before the first call to the chain. The deployment then confirms the endpoint's chain id against the configuration, builds with the pinned libraries, deploys `PaxeerXVault` with the attestor set and threshold, sets the caps of every asset the configuration lists, and proposes ownership to the configured owner. It reads the deployed code, owner, threshold, attestors and caps back, stops if the code hash is not the built one or the threshold, the attestors or a cap differs from the configuration, and writes a record naming the chain, the vault address, the deployer, the code hash, the block, the owner, the ownership state, the threshold, the attestors and the caps.

Ownership moves in two steps. Until the configured owner calls `acceptOwnership()` on the vault, the deployer is still the owner and the record reads `"ownership": "proposed"`.

The deploy key reaches `forge script` as a command argument, so run a deployment only where another user cannot read process arguments.

A vault accepts deposits as soon as it exists: it starts unpaused with its caps set. Until the Paxeer side reads back as registered for the chain, the owner can hold it with `pause()`.

### 3. Verify the source

| Variable | Holds |
| --- | --- |
| the variable in `environment.explorer_key`, `PAXEER_BRIDGE_<CHAIN>_EXPLORER_KEY` | the explorer verification key |
| `PAXEER_BRIDGE_DEPLOYMENT_RECORD` | the record step 2 wrote |

```sh
bash bridge/deploy/verify-evm-chain.sh --preflight <chain>
bash bridge/deploy/verify-evm-chain.sh <chain>
```

The vault address and the constructor arguments come from the deployment record, so a source cannot be claimed for a deployment that was never made. The script submits through `forge verify-contract` and reports the explorer's own answer; anything other than a verified or already-verified answer stops it.

### 4. Deploy and initialise the Solana program

```sh
bash bridge/deploy/deploy-solana-program.sh --preflight
bash bridge/deploy/deploy-solana-program.sh
```

The Solana page, `bridge/solana/chains/solana/README.md`, lists the variables this step needs - the endpoint, the publisher keypair file, the pinned toolchain directory, the deployment record and the program's admin client - and what each must hold. The admin client is `paxeer-x-bridge-solana-admin` in `bridge/solana/admin`; build it and name the built binary in `PAXEER_BRIDGE_SOLANA_ADMIN_CLI`:

```sh
cargo build --locked --release --manifest-path bridge/solana/Cargo.toml -p paxeer-x-bridge-solana-admin
```

The script builds the program with `cargo-build-sbf` at the platform tools release it pins, deploys it, checks the upgrade authority, that the program becomes executable, that the deployed ELF hashes to the built one and that the deployment is rooted. While `solana.program_id` is still `PLACEHOLDER:program-id`, the run stops there: it writes a record naming the program id and the vault authority with its handle, keeps the generated program keypair beside the record, and names the id to write into the configuration. The next run, with `solana.program_id` filled in and `PAXEER_BRIDGE_SOLANA_PROGRAM_KEYPAIR_FILE` naming that keypair, deploys to the same id, initialises the program with the owner, the attestor set and the threshold, registers the wrapped SOL mint and then the Sidiora mint with their caps, and writes the full deployment record. The record's `vault_handle` is the handle of the program's `vault-authority` PDA, the vault Paxeer registers for Solana.

### 5. Generate the proposals

```sh
go run ./bridge/deploy/proposals/cmd/paxeer-bridge-proposals -manifest bridge/deploy/attestors.json -proposals <proposals directory> <proposal input> <output directory>
```

or, from the chain configuration itself:

```sh
go run ./bridge/deploy/proposals/cmd/paxeer-bridge-proposals -authority <governance authority> -vault <vault> -readback <read-back file> -proposals <proposals directory> bridge/evm/chains/<chain>/config.json <output directory>
```

`-manifest` defaults to `bridge/deploy/attestors.json`. Given `-authority` and `-vault`, the generator reads the chain configuration through `bridge/deploy/chainconfig`, refuses any placeholder still in it, and takes the two values no configuration carries from those flags: the governance authority and the deployed vault, on Solana the vault-authority handle; `-readback` then also writes the values the deployed chain and the Paxeer side must read back. Without those flags it reads a proposal input in its own schema, described below. Either way it reads one chain and the manifest, and writes the message bodies into an empty or absent output directory, in the order the proposals carry them:

| File | Body |
| --- | --- |
| `01-register-chain.json` | `MsgRegisterChain`: the chain id, the vault - on Solana the vault handle - and the finality depth, with the chain enabled |
| `02-set-attestors.json` | `MsgSetAttestors`: the shared attestor set and the threshold |
| `03-set-cap-<NN>-<asset id>.json` | one `MsgSetCap` per asset, the native coin first; for Solana, `SID` under `0x21f7b20a555199fa73A238B1a91FD0f549068fEe` |

Given `-proposals`, it also writes the governance proposals into a second empty or absent directory, which must not be the output directory:

| File | Proposal |
| --- | --- |
| `04-proposal-open-chain.json` | the proposal that opens the chain: `MsgRegisterChain`, `MsgSetAttestors` and every `MsgSetCap` except Sidiora's, in the order of the bodies, the native coin's cap first |
| `05-proposal-sidiora-cap.json` | Solana only: `MsgRegisterSidioraPair`, which records the Sidiora pair against `usid`, and then Sidiora's `MsgSetCap`, in that order, in a proposal of its own |

Each file is the content of one governance proposal as a transaction carries it: a `BridgeProposal` under the type URL `/paxprotocol.paxchain.layerxbridge.BridgeProposal`, with its title, its description and every message packed under its own type URL. The proposals are written apart from the bodies because the checklist counts every JSON file in the bodies directory.

Every body and every proposal is marshalled from the message types in `modules/layerxbridge/types`, so it cannot drift from what the keeper accepts. The generator refuses a placeholder or zero authority, owner, vault or attestor, a zero threshold or one above the attestor count, a zero cap, and a set that differs from the manifest, and it writes nothing when it refuses.

The proposal input of the first form is a JSON file in the generator's own schema, decoded with unknown fields refused: `name`, `chain_id`, `native_symbol`, `native_decimals`, `rpc_endpoint_env`, `explorer_key_env`, `governance_authority`, `owner`, `vault`, `attestors`, `threshold`, `finality_depth`, `program_id`, `commitment`, `big_blocks_required` and `assets`, each asset carrying `address`, `asset_id`, `decimals`, `max_per_tx` and `max_total`. It is not the chain configuration file: the generator refuses the configuration's own fields. Write it from three sources - the chain configuration for the chain, the attestors, the finality depth and the assets with their caps; the deployment record for `vault`; and the bridge authority for `governance_authority`. `bridge/deploy/proposals/testdata/ethereum.json` and `bridge/deploy/proposals/testdata/solana.json` show the shape.

`governance_authority` is the bech32 account of the governance module, the authority a governance proposal executes with; the generator refuses any other account. The keeper applies each message only when its authority is that account.

### 6. Submit the proposals

The bodies reach the chain inside governance proposals. Submit `04-proposal-open-chain.json` as the content of one governance proposal; it carries `01-register-chain.json`, `02-set-attestors.json` and the `03-set-cap-*` bodies in their numbered order, Sidiora's excepted. For Solana, submit `05-proposal-sidiora-cap.json` as a second proposal once the first has passed: it carries `MsgRegisterSidioraPair` ahead of Sidiora's `MsgSetCap`, so the proposal itself registers the pair against `usid` before it caps it, as the Sidiora section below explains.

When a proposal passes, the governance module account executes it through the application's `layerxbridge` proposal route: `NewProposalHandler` in `modules/layerxbridge/handler.go` runs every message it carries, in order, through the bridge module's message service and so through the keeper. It refuses a proposal carrying a message for any authority other than the governance module account, and if one message fails none of them changes the state.

Submit each proposal with the node's governance submit command, `paxd tx gov submit-proposal`, through its bridge subcommand, `layerxbridge-proposal [proposal-file]`, with the generated file as its argument, the initial deposit in `--deposit` and the proposer as the signer `--from` names:

```
paxd tx gov submit-proposal layerxbridge-proposal <proposals directory>/04-proposal-open-chain.json --deposit <coins> --from <key> --chain-id <chain id>
paxd tx gov submit-proposal layerxbridge-proposal <proposals directory>/05-proposal-sidiora-cap.json --deposit <coins> --from <key> --chain-id <chain id>
```

The subcommand, `BridgeProposalHandler` in `modules/layerxbridge/client/cli/tx.go`, reads the file exactly as the generator writes it and refuses a file with an unknown or a missing field, a proposal the route above would refuse, and a missing deposit.

### 7. Read the deployment back

The bridge is opened only when every value below reads back as the configuration and the generated bodies say, with the native coin checked first on every chain. A read changes nothing anywhere.

`bash bridge/deploy/checklist.sh <chain>` performs this read-back for one chain: it reads the chain through the endpoint its configuration names and the Paxeer side through `PAXEER_BRIDGE_PAXEER_RPC_URL`, generates the bodies itself from the configuration, the vault in `PAXEER_BRIDGE_DEPLOYMENT_RECORD` and the authority in `PAXEER_BRIDGE_GOVERNANCE_AUTHORITY`, prints one line per check and exits 1 on the first mismatch. `PAXEER_BRIDGE_ATTESTOR_MANIFEST`, `PAXEER_BRIDGE_EVM_CHAINS_ROOT` and `PAXEER_BRIDGE_SOLANA_CHAINS_ROOT` are optional. The reads it makes are the ones below.

On each EVM chain, against the vault address in the deployment record:

```sh
cast code <vault> --rpc-url "$PAXEER_BRIDGE_<CHAIN>_RPC_URL"
cast call <vault> 'owner()(address)' --rpc-url "$PAXEER_BRIDGE_<CHAIN>_RPC_URL"
cast call <vault> 'attestors()(address[])' --rpc-url "$PAXEER_BRIDGE_<CHAIN>_RPC_URL"
cast call <vault> 'threshold()(uint256)' --rpc-url "$PAXEER_BRIDGE_<CHAIN>_RPC_URL"
cast call <vault> 'caps(address)(uint256,uint256)' <asset> --rpc-url "$PAXEER_BRIDGE_<CHAIN>_RPC_URL"
cast call <vault> 'paused()(bool)' --rpc-url "$PAXEER_BRIDGE_<CHAIN>_RPC_URL"
```

`cast keccak` of the code must equal the record's `code_hash`; the owner must be the configured owner, not the deployer; the attestors and threshold must be the manifest's; every asset, the native coin first, must carry the configured per-transaction and total cap; and `paused` must be `false`.

On Solana, read the config account (seed `config`) and every asset account (seed `asset` and the mint) of the program with `solana account`; their layouts are in `bridge/solana/src/state.rs`. The owner, the attestors, the threshold and the paused flag must match the configuration, and each asset account must carry its mint, its asset id and its caps, wrapped SOL first.

On Paxeer X Network, through the layerxBridge precompile:

```sh
cast call 0x0000000000000000000000000000000000001016 'getChain(uint64)(bool,address,uint64,bool)' <chain id> --rpc-url <Paxeer X Network EVM endpoint>
cast call 0x0000000000000000000000000000000000001016 'getAttestors()(address[],uint256[],uint32)' --rpc-url <Paxeer X Network EVM endpoint>
cast call 0x0000000000000000000000000000000000001016 'getCap(uint64,address)(string,uint256,uint256,uint256)' <chain id> <asset id> --rpc-url <Paxeer X Network EVM endpoint>
cast call 0x0000000000000000000000000000000000001016 'isPaused()(bool)' --rpc-url <Paxeer X Network EVM endpoint>
```

`getChain` must report the chain registered and enabled with the vault - on Solana the vault handle - and the finality depth of `01-register-chain.json`. `getAttestors` must return the manifest's set and threshold. `getCap` must return a denom and the `max_in_flight` and `max_per_tx` of each cap body, the native coin first; an empty denom means the asset is not registered. `isPaused` must be `false`.

Any disagreement, any placeholder still in place, an unregistered or uncapped native coin, a paused vault, program or precompile, or a chain the Paxeer side does not report as registered keeps the bridge closed.

## Outbound admission and coordinated cap changes

For each registered chain and asset, Paxeer's `max_per_tx` admission cap must be no greater than the active foreign release cap, in the same asset base units. `BridgeOut` checks that registered cap before debiting or burning, changing in-flight supply, issuing a nonce or emitting the burn. A missing or zero cap admits no positive burn. Deposits that individually fit the cap do not permit their combined balance to be burned above it; an amount exactly at the cap is admissible when the other checks pass. Reading and reconciling both caps is an owner/governance opening condition, not an automatic cross-chain update.

The cap-change contract is ordered:

1. Before a decrease, governance pauses Paxeer bridge admission. Wait for that pause to finalize and inventory every finalized burn through the pause height, keyed by chain, Paxeer transaction hash and outbound nonce, with its original asset, recipient and amount. Reconcile each item with the relayer journal and the foreign nullifier. Keep the foreign release path open at its existing cap while these pending obligations drain.
2. Governance lowers the Paxeer cap to the proposed value first. The foreign owner must not lower the release cap below any already-authorized, unresolved burn. Complete those releases under the old foreign cap and record their finalized release and consumed nullifier before lowering the foreign cap. If an obligation cannot be resolved, retain the old foreign cap and keep admission paused; a cap change never cancels it.
3. After the foreign decrease finalizes, read both values back and confirm `Paxeer max_per_tx <= foreign release cap`. Governance may then unpause admission. For an increase, the foreign owner raises and finalizes the release cap first; governance raises the Paxeer cap only after readback confirms the same inequality. Pauses and cap updates retain the existing owner and governance authority checks.

An existing burn blocked by a prematurely reduced cap needs an explicit operator-visible resolution record linked to the immutable journal item, not a replacement attestation. Record `cap-blocked` with the original amount, active foreign cap, failed transaction or observation, responsible owner and intended corrective cap action. Keep its resolution visible as `awaiting-owner-cap-restoration`, then `release-submitted`, and only `released` after finalized foreign evidence and the once-only nullifier confirm payment. These are required coordination-record states; they do not add or rename relayer journal variants. The journal already retains submitted, reverted/dropped or failed releases and completed releases in `interop/crates/layerx-bridge-relayer/src/journal.rs`; preserve those records across restart and link them to the resolution record. Never report a failed or merely submitted release as paid.

The owner may restore enough release capacity to settle the original authorized amount, then repeat the coordinated decrease. Do not rewrite the amount, split one burn into multiple attestations, reset its nonce, discard its pending record, or bypass its nullifier. `bridge/evm/src/PaxeerXVault.sol::release` and `bridge/solana/src/release.rs` with `state.rs::Asset::withdraw` continue enforcing the release cap, available custody, fixed attested amount and once-only burn identity. A transport retry may carry the same attestation in a replacement transaction under the existing retry rules; it cannot change the economic obligation.

## The Sidiora pair on Solana

An inbound SID deposit from Solana resolves to the `usid` denom only if the Paxeer side records the pair (`91600046870081`, `0x21f7b20a555199fa73A238B1a91FD0f549068fEe`) against `usid` before any cap is set for it.

- `MsgSetCap` for a pair the Paxeer side has not registered registers it itself, under the bridge's generic denom `factory/<bridge module account>/lxb<hex>`, not under `usid`. Submitting Sidiora's cap body first binds Solana's SID to the wrong denom, and `EnsureSidioraDenom` then refuses to rebind it.
- `MsgRegisterSidioraPair` registers the pair against `usid`. The keeper executes it through `EnsureSidioraDenom` in `modules/layerxbridge/keeper/sidiora.go`, for the governance authority only, and refuses it for any chain but `91600046870081`, Solana, Sidiora's foreign home. It needs the chain registered, and registering the pair again changes nothing.

The ordering rule is carried by the proposal itself. `05-proposal-sidiora-cap.json` carries `MsgRegisterSidioraPair` first and Sidiora's `MsgSetCap` second, and a proposal executes whole or not at all, so the cap can only land on the pair the same proposal has just recorded against `usid`. The generator writes it for Solana only and refuses Sidiora's asset id on any other chain.

So, for Solana: submit `04-proposal-open-chain.json`, which registers the chain, installs the attestors and sets the wrapped SOL cap without Sidiora's. Once it has passed, submit `05-proposal-sidiora-cap.json`; submitted earlier, it fails because the chain is not registered yet, and changes nothing. Once it has passed, read the pair back:

```sh
cast call 0x0000000000000000000000000000000000001016 'getCap(uint64,address)(string,uint256,uint256,uint256)' 91600046870081 0x21f7b20a555199fa73A238B1a91FD0f549068fEe --rpc-url <Paxeer X Network EVM endpoint>
```

The denom it returns must be `factory/pax1dzfx9mk4fl9kl2mysjmtvk2xp75ljumk6nynhf/usid`, the `usid` denom of the bridge module account, with the caps of the proposal. A denom ending in `lxb` followed by hex means a cap for the pair was set by something other than this proposal before it, and the proposal's registration of the pair is then refused.

## Deposit recipients

Paxeer X Network mints an inbound deposit only to a nonzero EVM address left-padded to 32 bytes, `bytes32(uint256(uint160(address)))`: `RecipientAddress` in `modules/layerxbridge/types/attestation.go` is the rule, `Keeper.BridgeIn` refuses any other `paxeerRecipient`, and the relayer refuses to attest one. `PaxeerXVault` applies the same rule in `deposit` and `depositNative` before anything moves: a recipient with a nonzero high twelve bytes, or with a zero low twenty bytes, reverts with `InvalidRecipient()`, so no balance, `outstanding`, `depositNonce` or `BridgeDeposit` changes. Every accepted recipient reaches `BridgeDeposit`, the inbound digest, the relayer and `Keeper.BridgeIn` byte for byte; the selectors, the event and both digest layouts are the ones `modules/layerxbridge/ATTESTATION.md` specifies.

A vault deployed before this rule is not upgradeable and keeps accepting the other encodings. A deposit it accepted with a recipient Paxeer refuses is locked in that vault: it counts against the asset's total cap, the relayer journals it as refused and attests nothing, and the keeper mints nothing for it. No path of the vault returns it - `release` needs an attested Paxeer burn and `rescue` refuses every registered asset and the native coin. Every such deposit is reported, by chain, vault, transaction hash and log index, as requiring an explicit recovery disposition decided by the bridge authority. Neither the relayer, an attestor nor the keeper rewrites the signed recipient bytes to another address or credits anyone on Paxeer for it; a recovery happens only as that authorized disposition, never as a bridge mint.

## The relayer

`interop/crates/layerx-bridge-relayer` observes the vaults, has attestations signed through the remote signer and submits both directions. It runs as `layerx-bridge-relayer --config <path>` against one JSON file: `journal_path`, optional `cosign_directory`, `poll_interval_ms`, `max_submissions`, `signer`, `attestor`, `paxeer`, `chains` and optional `solana`. Unknown fields, including nested fields, are refused by the production `RelayerConfig` loader in `interop/crates/layerx-bridge-relayer/src/config.rs`. Each `chains` entry is an EVM chain carrying `chain_id`, `vault`, `finality_depth`, `start_block`, `max_block_range`, `rpc`, `submitter` and `gas`; the current validator requires at least one such entry. Omitting `solana` leaves only the EVM destinations configured. The file carries public keys, opaque signer handles and protected-file references, never private keys or bearer-token values.

`bridge/deploy/relayer-solana.example.json` is a complete, deliberately non-production configuration instance of this schema. Every `EXAMPLE_ONLY` handle/path/backend name, `.invalid` endpoint, EVM vault, public key, program identity and mint is a fixture value, not an approved deployment. Its program and vault handle are the existing public PDA test vector from `interop/crates/layerx-bridge-relayer/src/solana/release.rs`; its mint decodes to 32 bytes of `01`. The public signing keys are test vectors with no provisioned signer handles. Do not fund them or use them as operator identities. The example can pass offline configuration loading without any of those paths or services existing; that is not evidence of signer access, an RPC quorum, custody or outbound readiness.

The optional `solana` object has this contract:

| Field | Operator value |
| --- | --- |
| `chain_id` | Exactly `91600046870081` (`0x0000534f4c414e41`). No EVM `chains` entry may use this reserved identity when Solana is configured. |
| `program_id` | The deployed custody program's nonzero 32-byte public key in base58. |
| `vault` | The 20-byte, `0x`-prefixed handle of that program's `vault-authority` PDA: the last 20 bytes of keccak256 of the PDA's 32-byte key. It must equal the vault registered on Paxeer. It is not the program's handle or the vault token-account address. |
| `commitment` | `confirmed` or `finalized`; `processed`, misspellings and other values are refused. |
| `finality_depth` | Positive slot depth, additional to the selected commitment. Select it from the approved chain policy; it is not an EVM block count. |
| `start_slot`, `max_slot_range` | Initial observation cursor and positive bounded slot range per scan. Preserve the journal on restart; these fields do not authorize discarding recorded obligations. |
| `rpc` | The authenticated HTTPS quorum configuration described below. |
| `fee_payer` | A separate remote ed25519 `handle` and its 32-byte `public_key` as `0x`-prefixed hex. The attestor remains secp256k1; reusing its handle for any fee payer is refused. |
| `release_mints` | Distinct, nonzero 32-byte mint public keys in base58. Each must resolve to a program-owned asset PDA carrying the burn's registered asset id and that exact mint. |

`rpc` carries `endpoints`, `quorum`, `connect_timeout_ms`, `request_timeout_ms` and `maximum_response_bytes`. Each endpoint supplies `url`, `ca_certificate_der`, `bearer_token_file` and `independent_backend`. Use independently operated backends, not aliases of one provider: production `RpcCluster` requires 2–8 endpoints with distinct host/port identities and distinct normalized backend identities, a strict-majority quorum of at least two, connect timeouts of 100–30000 ms, request timeouts of 100–120000 ms and response bounds of 1024–16777216 bytes. The example uses two agreeing replies out of three. Substitute genuine HTTPS origins, DER trust anchors, protected bearer-token file paths and audited backend identities. These transport checks and protected-file reads occur when constructing the production transport; offline `RelayerConfig::load` alone does not establish them. Paxeer uses its separate `endpoints` schema with `trust_anchor_der` and `request_timeout_ms`; `local_emulator` is not a production TLS substitute.

`signer` supplies a positive `timeout_ms` and an `endpoint`. The example uses `{ "transport": "uds", "socket": "/EXAMPLE_ONLY/operator-a/signer.sock" }`. A mutually authenticated endpoint instead uses `transport: "mutual_tls"`, `endpoint` (socket address), `server_name`, `trust_anchor`, `client_certificate` and `client_private_key` (protected file path only). Replace all signer handles and public keys together with the operator's existing provisioned policy bindings. Keep the attestor, Paxeer submitter, each EVM submitter and Solana ed25519 fee payer independent, including across operators, as required by the inventory below. A public key in this file neither provisions a key nor grants bridge authority.

With `solana` present but `release_mints` omitted or `[]`, the Solana path is observation-only: `RelayerConfig::build` creates the observer and no `SolanaRelease`. The `fee_payer` schema remains required and validated even in that mode. A nonempty list enables construction of the outbound path; it does not establish readiness. Before releasing a burn, the production relayer reads the custody config and asset PDA, checks the active attestor set, pause state and registered vault identity, and resolves the burn's 20-byte recipient handle through the program-owned `recipient` PDA. The PDA record must contain the same handle and its matching 32-byte recipient key. Register that recipient through the custody program before expecting a release.

The vault-authority and recipient associated token accounts must already exist under the SPL Token program, each with the exact mint and expected owner and an initialized token-account state. The relayer does not create missing token accounts or invent a recipient from a handle. Missing recipient registration, an absent/unmatched asset, paused custody or uninitialized token accounts keeps that release waiting; a malformed identity is refused. Custody balance, caps, authorized attestor quorum and the burn's once-only nullifier still apply. Provision these accounts, read back the real program/PDA identities and registered Paxeer chain/caps, fund the separate fee payer through the operator's approved procedure, and retain the existing journal and per-item failure/retry records. A blocked Solana item is not a paid release and does not erase independent destination progress.

To adapt the example, replace every fixture identity and reference with the operator-approved deployment records, including the Paxeer/EVM chain and vault identities, program and derived vault handle, mint registrations, signer bindings, storage paths, RPC trust/authentication references, observation start points and policy depths. Select gas limits/fees and polling/submission bounds for that deployment; example numbers are not an operational recommendation. Leave `release_mints` absent until outbound prerequisites have been established. Reconcile the resulting configuration with the membership, independent storage and destination-policy inventory below; the example supplies none of that external authority.

The focused source contract is `interop/crates/layerx-bridge-relayer/tests/operator_config.rs`. It loads the actual example through `RelayerConfig`, checks the public identity vector and observation-only variants, and refuses malformed/unknown fields, wrong chain identity, invalid commitment and attestor/fee-payer handle reuse:

```sh
timeout 10m cargo test --locked --manifest-path interop/Cargo.toml -p layerx-bridge-relayer --test operator_config
```

This test performs no live chain calls, signer operations or deployments. Passing it says nothing about operator inputs or a live run.

With a threshold above one, each approved attestor runs its own instance of the relayer app, and each instance runs the cosign share transport `layerx-bridge-cosign` beside the relayer. The transport is enabled by `LAYERX_BRIDGE_COSIGN_TRANSPORT_CONFIG`, which `interop/deploy/bridge-relayer/fly.toml` sets to `/run/secrets/bridge/cosign/transport.json`; the app mounts four cosign secrets: `BRIDGE_COSIGN_TRANSPORT_CONFIG` at `/run/secrets/bridge/cosign/transport.json`, `BRIDGE_COSIGN_TRUST_ANCHOR` at `/run/secrets/bridge/cosign/ca.pem`, `BRIDGE_COSIGN_CERTIFICATE` at `/run/secrets/bridge/cosign/certificate.pem` and `BRIDGE_COSIGN_PRIVATE_KEY` at `/run/secrets/bridge/cosign/private-key.pem`. The entrypoint hands them to the relayer user, owner-read-only, before dropping privileges, and refuses to start when the variable names a missing file. `transport.json` is decoded with unknown fields refused: `attestor` (this instance's attestor address), `listen`, `server_name`, `trust_anchor`, `certificate`, `private_key` (outside the shared directories, no group or other access), `cosign_directory` (`/data/cosign`, the relayer's `cosign_directory`), `delivery_directory` (`/data/cosign-delivery`), `peers` (one entry per other approved attestor with `attestor`, `address`, `server_name` and `spki_sha256`, the SHA-256 of that peer's certificate public key), `timeout_ms`, `sweep_interval_ms` and `max_connections`. Instances deliver their own shares to every peer over mutually authenticated, pinned TLS 1.3 and admit a peer's share only when it recovers to that authenticated peer; each instance keeps its own journal, cosign and delivery directories and its own fee-payer handles. Run one instance per attestor in the approved set in `bridge/deploy/attestors.json`, and only for that set: placeholder addresses, fixture keys or test certificates are not approval of an operator.

Before starting an operator, supply `BRIDGE_OPERATOR_INVENTORY` and `BRIDGE_APPROVED_MEMBERSHIP` for the existing deployment; `interop/deploy/bridge-relayer/fly.toml` mounts them at `/run/secrets/bridge/cosign/operators.json`, which `LAYERX_BRIDGE_OPERATOR_INVENTORY` names, and `/run/secrets/bridge/cosign/membership.json`. The entrypoint requires the inventory when share transport is enabled. The transport checks that inventory against its actual certificate, complete peer roster, relayer public-key handles, destination vaults and storage paths before listening. A missing inventory, example membership, duplicate operator/storage/TLS identity, shared fee-payer key or mismatched destination policy refuses startup. Each operator retains its own volume; no journal or key directory is synchronized. `CosignDirectory` verifies a share before publication, persists it without replacing an existing share, and fsyncs both the share directory and its parent before acknowledging it. Identical delivery is idempotent; conflicting delivery preserves the first bytes and remains visible as a conflict.

The version-1 operator inventory is strict JSON with `version: 1`, `authority: "bridge"`, an owner-supplied nonempty `approval` reference, `membership` (the absolute path `/run/secrets/bridge/cosign/membership.json`), `operators`, and `destinations`. The membership file uses the existing `{ "attestors": [...], "threshold": ... }` format and must contain the actual approved bridge addresses. Every operator record supplies `instance`, `storage` (its independently owned durable-volume identifier), `attestor`, `signer_handle`, `signer_public_key`, `journal_path`, `cosign_directory`, `delivery_directory`, `spki_sha256`, and `fee_payers`. Each fee-payer entry names `chain_id`, `handle`, and `public_key`; keys are independent across operators and chains. Handles are scoped to the operator's signer. Each destination record supplies `chain_id`, `vault`, the same `attestors` and `threshold`, and the genuine policy observation's `observed_block_hash`. Include the Paxeer bridge precompile, every configured EVM vault and any configured Solana vault. These observations reconcile the supplied inventory; the relayer still reads the destination's current authority before assembling a transaction. Wallet and xweb membership confer no bridge authority.

The `operator_cosign_contract` test, `interop/crates/layerx-bridge-relayer/tests/operator_cosign_contract.rs`, retains the real generated-signer cardinality exercise and separately requires `LAYERX_BRIDGE_OPERATOR_CONTRACT_INPUTS`, an absolute path to strict JSON containing `inventory` and `operators` (each with absolute `transport_config` and `relayer_config` paths). The supplied-input case opens current production configuration and authenticates real destination-policy reads with the configured RPC adapters. It never starts an operator, submits a transaction or signs using supplied operator handles. Without `LAYERX_BRIDGE_OPERATOR_CONTRACT_INPUTS` and the approved membership, storage ownership, independent handle mappings, TLS identities, public relayer configurations and policy observations it points to, the supplied-input case fails rather than passing on generated signers. The generated signer case demonstrates transport and journal behavior only; it does not approve those identities as deployed operators.
