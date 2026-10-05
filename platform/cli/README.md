# `layerx` developer CLI

`platform/cli` builds the `layerx` binary (package `layerx-platform-cli` in the
[`platform`](../Cargo.toml) Cargo workspace), the developer command line for the
LayerX kernel domain of Paxeer X Network. The complete payment walkthrough is
[`docs/wiki/PaymentsQuickstart.md`](../../docs/wiki/PaymentsQuickstart.md); hosted
documentation lives at [docs.paxeer.app](https://docs.paxeer.app/).

The top-level commands are `wallet`, `token`, `new`, `workspace`, `environment`,
`key`, `auth`, `account`, `register`, `faucet`, `payment`, `receipt`, `program`,
`emulator`, `install`, `mcp` and `a2a` (`src/main.rs`). `wallet` and `token`
carry the native key, account, transfer, token and receipt operations described
below. `account` creates or reads a developer account, `register` registers a
self-service identity principal for a local signing key, and `payment test`
requests a move quote (`/v1/moves/quote`) and commits it (`/v1/moves`) with a
caller-supplied idempotency key (`src/payment.rs`).

`--json`, `--rpc` and `--gateway-credential` are global flags. `--rpc` takes a
public gateway JSON-RPC endpoint ending in `/rpc`; `--gateway-credential` names a
stored gateway credential alias. `a2a serve` additionally requires its own
`--gateway-credential` argument.

## Build

```bash
cargo build --manifest-path platform/cli/Cargo.toml
```

The executable is `platform/target/debug/layerx`; put that directory on your PATH.

## Wallet quickstart

Wallet creation runs against the local emulator. Imports, listing, balance
reads and verified RPC receipt waits are available. Send, token transfer, token
creation, mint, burn and asset-account opening disclose and sign with separate
identity and source-account sequences. Public writes require `--rpc`, a trusted
receipt policy and a fee limit. On a public gateway, `wallet create` returns
`wallet_registration_unavailable` without generating a key (use `layerx
register` there), and DID history returns `wallet_history_unavailable`.

### Create a local wallet

Use the OS keyring or configure the encrypted file store below. Create a private
profile and provision an emulator:

```bash
umask 077
export LAYERX_CONFIG="$HOME/.config/layerx/config.json"
mkdir -p "$(dirname "$LAYERX_CONFIG")"
layerx emulator provision
layerx emulator up --sequencer-seed-file "$HOME/.config/layerx/emulator/sequencer.seed"
```

`emulator up` listens on loopback port 9402 with network ID 402 by default and
refuses non-loopback listen addresses. In another terminal with the same
configuration and credential-store settings:

```bash
layerx environment use emulator --endpoint http://localhost:9402 \
  --network-id 402 \
  --sequencer-trust-anchor-file "$HOME/.config/layerx/emulator/sequencer.anchor"
layerx wallet create alice
layerx wallet list
layerx wallet balance
```

`--endpoint`, `--network-id` and a sequencer trust anchor must be supplied
together. Plain `http://` endpoints are accepted only for loopback hosts.

Creation registers the local DID and opens its main account with zero units.
If registration fails after key creation, the key is retained; retry using the
same wallet name. Import an existing 32-byte hexadecimal seed with
`layerx wallet import alice`, supplying the seed on stdin. Importing does not
register or fund an identity. `layerx wallet derive` derives the EVM account and
LayerX identity of a BIP-39 phrase read from stdin or a file.

### Beta funds

The limited beta has not opened yet. The gateway API becomes available when it
does. This is a mainnet beta on real value, so there is no faucet for general
use; approved developers receive test allocations from the team.

`layerx faucet [--key NAME]` sends the `lx_requestFunds` JSON-RPC call for the
key's DID and public key to the active environment. A claim counts as funded
only when the response reports `funded: true` with a hexadecimal
`funding_id`; any refusal or missing field is an error.

### Send and create a token

Amounts are integer base units. `ASSET_ID` is the asset's 64-character
hexadecimal identifier; `RECIPIENT_DID` is the recipient's DID.
The recipient's account must already exist for that asset.

Configure your beta environment with its network ID and store your gateway
credential under the `beta` alias. Obtain a receipt policy from an
independently trusted operator; do not derive trust pins from the RPC response
being verified. The JSON file contains exactly `protocol_version` (3),
`network_id`, `sequencer_id` and `sequencer_key` (64 hexadecimal characters
each), `first_batch` and `last_batch` (an inclusive authorized range), and
`checkpoint_context_digest` (a SHA-256 hexadecimal digest, required for
finality). Use `null` for the checkpoint digest when only execution or batch
verification is needed. Set `RECEIPT_POLICY` to this file and `FEE_LIMIT` to
your maximum fee in base units.

```bash
layerx --rpc "$RPC_URL" --gateway-credential beta token create --symbol PAY --name 'Payment Token' \
  --decimals 6 --supply-cap 1000000000 --salt "$(openssl rand -hex 32)" \
  --receipt-policy "$RECEIPT_POLICY" --fee-limit "$FEE_LIMIT" --wait executed
layerx --rpc "$RPC_URL" --gateway-credential beta wallet open-account --asset "$ASSET_ID" \
  --receipt-policy "$RECEIPT_POLICY" --fee-limit "$FEE_LIMIT"
layerx --rpc "$RPC_URL" --gateway-credential beta token mint --asset "$ASSET_ID" \
  --to "$RECIPIENT_DID" --amount 100 \
  --receipt-policy "$RECEIPT_POLICY" --fee-limit "$FEE_LIMIT"
layerx --rpc "$RPC_URL" --gateway-credential beta token burn --asset "$ASSET_ID" --amount 1 \
  --receipt-policy "$RECEIPT_POLICY" --fee-limit "$FEE_LIMIT"
```

Token creation prints its derived asset ID in the disclosure. Use that identifier
for subsequent commands. Writes print the complete disclosure to stderr before
signing. They submit canonical signed bytes and verify the receipt against the
locally computed activity ID. A successful result reports the activity ID,
receipt result and commitment reached. A failed native receipt exits nonzero.
An acknowledgement alone never counts as success. `--timeout-seconds` accepts
1–300 seconds and defaults to 60; `--wait` defaults to `executed`. Pending
outcomes retain the activity ID for later receipt retrieval. Do not blindly
repeat a pending write: each invocation creates a new idempotency key.

Send and transfer:

```bash
layerx --rpc "$RPC_URL" --gateway-credential beta wallet send --to "$RECIPIENT_DID" \
  --asset "$ASSET_ID" --amount 100 --wait executed \
  --receipt-policy "$RECEIPT_POLICY" --fee-limit "$FEE_LIMIT"
layerx --rpc "$RPC_URL" --gateway-credential beta token transfer --to "$RECIPIENT_DID" \
  --asset "$ASSET_ID" --amount 10 --wait finalised \
  --receipt-policy "$RECEIPT_POLICY" --fee-limit "$FEE_LIMIT"
```

Send reads the identity sequence using `lx_getSequence([did, "identity"])`
and reads the source-account sequence independently. The source and DID
destination accounts are selected from the authenticated `lx_getBalances`
snapshot by exact asset ID, account name and recomputed account ID. The CLI
discloses and signs the native debit authorization before disclosing and
signing the shared envelope. An unavailable identity/account snapshot,
ambiguous account, invalid signature or missing commitment evidence produces an
error. Send against the emulator returns `identity_sequence_unavailable`
without signing.

| Commitment | Required evidence |
| --- | --- |
| `executed` | A sequencer-signed receipt bound to the activity ID |
| `batched` | Receipt inclusion in an authorized, signed batch header |
| `finalised` | A verified guarantor checkpoint certificate for that same header |

Retrieve or wait for a receipt with:

```bash
layerx --rpc "$RPC_URL" --gateway-credential beta wallet receipt "$ACTIVITY_ID" \
  --receipt-policy "$RECEIPT_POLICY" --wait finalised
```

### Fee estimates and live notifications

Estimate fees using already encoded canonical activity bytes, or read one live
notification:

```bash
layerx --rpc "$RPC_URL" --gateway-credential beta wallet estimate-fee "$CANONICAL_HEX"
layerx --rpc "$RPC_URL" --gateway-credential beta wallet watch receipts
layerx --rpc "$RPC_URL" --gateway-credential beta wallet watch checkpoints
layerx --rpc "$RPC_URL" --gateway-credential beta wallet watch account --account-id "$ACCOUNT_ID"
```

Fee estimation forwards to the native fee schedule and preserves unavailable
errors. Each watch reads one notification over the authenticated WebSocket at
`/rpc/ws` and exits; `--timeout-seconds` is bounded to 1–300 seconds and
defaults to 60. Notifications are marked `verified: false`; they establish no
commitment. Use `wallet receipt` with your receipt policy to verify commitment.
On timeout, closure or feed loss, reconcile using RPC reads before starting
another watch.

### Public JSON-RPC reads

```bash
layerx --rpc "$RPC_URL" --gateway-credential beta wallet balance --did "$WALLET_DID"
layerx --rpc "$RPC_URL" --gateway-credential beta wallet balance --did "$WALLET_DID" --asset "$ASSET_ID"
layerx --rpc "$RPC_URL" --gateway-credential beta wallet receipt "$ACTIVITY_ID" --receipt-policy "$RECEIPT_POLICY"
layerx --rpc "$RPC_URL" --gateway-credential beta token info "$ASSET_ID"
layerx --rpc "$RPC_URL" --gateway-credential beta token list --limit 64
```

`RPC_URL` must end in `/rpc`; non-loopback endpoints require HTTPS. Unknown
methods, malformed responses and unavailable native evidence exit nonzero.
Token info and list call `lx_getAsset` and `lx_listAssets`; `token list` takes
an optional `--cursor` and a `--limit` of 1–256 (the gateway applies 64 when it
is absent).

## MCP payment surface

The MCP catalogue (`agent/crates/layerx-mcp`) exposes `wallet.send`,
`token.create`, `token.mint` and `token.transfer` through the daemon's ordinary
submission path. MCP does not expose the CLI's burn, open-account, token info or
token list operations. `layerx install mcp` installs the daemon-bound MCP
server; `layerx mcp serve --daemon-binding FILE` serves it on stdin and stdout.

## Headless credential storage

The OS keyring is the default. On a headless Linux server, container, or CI
runner, explicitly select the encrypted file store before creating a key:

```bash
export LAYERX_CREDENTIAL_STORE=file
read -r -s -p 'Credential passphrase: ' LAYERX_CREDENTIAL_PASSPHRASE
export LAYERX_CREDENTIAL_PASSPHRASE
layerx --json key create quickstart
layerx --json key list
layerx --json auth status
```

Use a passphrase of 12–16384 bytes. In CI, supply
`LAYERX_CREDENTIAL_PASSPHRASE` through the CI secret environment. Keep it
available for subsequent CLI and MCP/A2A processes, and unset it when finished.
Secret imports still read stdin; the passphrase does not consume that input.
`auth status` reports whether an API token is stored, independently of keys;
store one with `layerx auth set` using the token on stdin.

The store writes `credentials/vault` beside the resolved CLI config file
(`LAYERX_CONFIG`, otherwise `$XDG_CONFIG_HOME/layerx/config.json`, otherwise
`$HOME/.config/layerx/config.json`). It encrypts its contents using AES-256-GCM
with a key from PBKDF2-HMAC-SHA256 at 600,000 iterations, a fresh 16-byte salt
and a fresh 12-byte nonce on every update. Vault and lock files use mode 0600
inside a mode 0700 directory. Updates hold a file lock and replace the vault
atomically. This backend requires Unix file permissions.

There is no automatic fallback or migration between stores. Unset
`LAYERX_CREDENTIAL_STORE` (or set it to `os`) to use the OS keyring. Retain the
passphrase and encrypted vault together in your backup procedure: a lost
passphrase cannot be recovered. Changing the environment passphrase does not
rotate the vault password; it makes authentication fail.
