# Hosted gateway

`layerx-platform-gateway` builds the `layerx-gateway` binary, the
receipt-verifying public ingress of the Paxeer X Network. It serves one JSON-RPC
endpoint for both execution domains: LayerX kernel methods (`lx_*`), unified
cross-domain reads (`px_*`) and the Paxeer X chain's EVM methods relayed to
`paxd`. Routes, scopes and TLS are described in
[`docs/wiki/HostedGateway.md`](../../../docs/wiki/HostedGateway.md).

The crate is a member of the [`platform`](../../Cargo.toml) Cargo workspace:

```sh
cargo build --locked --manifest-path platform/Cargo.toml -p layerx-platform-gateway
cargo test --locked --manifest-path platform/Cargo.toml -p layerx-platform-gateway
```

The limited beta has not opened yet. The gateway API becomes available when it
does. This is a mainnet beta on real value, so there is no faucet for general
use; approved developers receive test allocations from the team.

## Public JSON-RPC

`POST /rpc` is JSON-RPC 2.0. `GET /rpc/schema` serves
[`openrpc.json`](openrpc.json), which defines method names, parameter order,
commitment levels (`executed`, `batched`, `finalised`) and error codes.
`GET /rpc/ws` is the authenticated WebSocket upgrade for `lx_subscribe`; over
plain `POST /rpc`, `lx_subscribe` and `lx_unsubscribe` answer `-32004`. Kernel
reads forward to the public core URL (`LAYERX_GATEWAY_PUBLIC_CORE_URL`), and
`lx_estimateFee` forwards the canonical activity bytes to the core's fee
estimate.

Reads are unauthenticated. `lx_sendActivity` requires
`Authorization: LayerX-Key <key>` with the `activity:write` scope. The gateway
decodes the signed activity strictly against the provisioned module registry
(below), checks protocol version, network ID and the key's bound signer
signature, and routes Programs deploy (1), upgrade (2), call (3) and wind-down
(7) to the core's Programs routes and every other admitted activity to the
core's activity route. Which activity types are admitted is decided by the
registry file.

## Single network endpoint

`POST /rpc` also carries the Paxeer X chain. `eth_*`, `net_*` and `web3_*`
relay byte-for-byte to the Paxeer RPC names in `LAYERX_GATEWAY_PAXEER_RPC_URLS`,
a JSON array of two to eight distinct URLs (for example
`["https://api1.mainnet-beta.paxeer.network","https://api2.mainnet-beta.paxeer.network"]`),
each fronting `paxd`. A call tries the names in order and answers from the first
that answers 200 without a transport failure; each skipped name is logged as
`paxeer_endpoint_failed`. `paxeer_chain` readiness reports available while any
name answers. An invalid array refuses startup. `eth_sendRawTransaction` is
relayed like any other method, so an already signed transaction reaches the
chain through this endpoint. The gateway never signs for a caller:
`eth_accounts`, `eth_coinbase`, `eth_sendTransaction`, `eth_sign`,
`eth_signTransaction`, `eth_signTypedData`, `eth_signTypedData_v4` and
`eth_mining` are refused with `-32601`, and `eth_subscribe`/`eth_unsubscribe`
with `-32004`. Without the variable the relay answers `-32001` with code
`paxeer_rpc_not_configured`; `lx_*` is unaffected.

`GET /rpc/evm/ws` is an authenticated (`LayerX-Key`) WebSocket relayed to
`LAYERX_GATEWAY_PAXEER_WS_URL`, whose host must be one of the configured Paxeer
RPC names; without it the upgrade answers 503 `paxeer_websocket_not_configured`.

The EVM policy (namespaces, refused signing methods, precompile addresses and
selectors, and answer decoders) lives in [`src/evm.rs`](src/evm.rs) and is
unit-tested against vectors generated from the precompiles' ABI
([`tests/fixtures/paxeer-abi-vectors.json`](tests/fixtures/paxeer-abi-vectors.json)).

`px_*` are the unified cross-domain reads: `px_resolveAccount`,
`px_getAccount`, `px_getBalances`, `px_listAssets`, `px_getNetwork`,
`px_getCapabilities`, `px_getHistory`, `px_getUnifiedHistory` and
`px_getRouteCatalogue`. The account and balance reads join the `bank`
(`0x0000000000000000000000000000000000001001`), `addr` (`…1004`),
`layerxcustody` (`…1013`) and `layerxanchor` (`…1014`) precompiles to the public
core reads. A batch may mix `eth_`, `lx_` and `px_` entries. Shapes and error
codes: [`openrpc.json`](openrpc.json).

Method list: [`docs/wiki/PublicRpc.md`](../../../docs/wiki/PublicRpc.md).
Commitment parameter: [`docs/wiki/CommitmentLevels.md`](../../../docs/wiki/CommitmentLevels.md).
Request and response pairs: [`docs/wiki/PublicAPI.md`](../../../docs/wiki/PublicAPI.md).
Public RPC names: [`docs/site/docs/reference/public-rpc.md`](../../../docs/site/docs/reference/public-rpc.md).

## Canonical module registry

`LAYERX_GATEWAY_MODULE_REGISTRY_FILE` names a JSON file that the gateway shares
with the receipt authority ([`../authority`](../authority/README.md)). Shape:

```json
{
  "schema_version": 2,
  "assets": [{
    "asset": "0202020202020202020202020202020202020202020202020202020202020202",
    "currency": "USD",
    "decimals": 6,
    "symbol": "$"
  }],
  "modules": [
    {"module": 9, "ordinals": [1, 2, 7]}
  ]
}
```

A fuller example is
[`interop/deploy/gateway/module-registry.example.json`](../../../interop/deploy/gateway/module-registry.example.json).
The asset shown is illustrative. `schema_version` must be 2. There must be
1..256 assets; every asset ID is nonzero lowercase 64-digit hex and unique;
currency and symbol hold 1..32 bytes without control characters; decimals is at
most 38. Modules must be known module IDs, at most one entry per module ID in
`layerx-types`. For the Programs module the gateway always adds ordinals 1, 2
and 7. The gateway only reads this file.
