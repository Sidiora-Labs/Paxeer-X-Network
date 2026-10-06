# interchain (ibc-go)

This directory is the vendored copy of [ibc-go](https://github.com/cosmos/ibc-go), the Golang implementation of the Inter-Blockchain Communication protocol (IBC), as used by the Paxeer X chain (`paxd`). Packages are imported as `github.com/Sidiora-Labs/Paxeer-X-Network/interchain/...`.

IBC allows blockchains to talk to each other. It handles transport across different sovereign blockchains: an end-to-end, connection-oriented, stateful protocol that provides reliable, ordered, and authenticated communication between heterogeneous blockchains. This implementation is built as a Cosmos SDK module.

In this repo, `node/app.go` wires the IBC core module and the ICS 20 transfer application into `paxd`, and the CosmWasm module under [`wasm/x/wasm`](../wasm/x/wasm) uses the core channel and port packages for contract IBC.

## Contents

1. **[Core IBC Implementation](modules/core)**

    1.1 [ICS 02 Client](modules/core/02-client)

    1.2 [ICS 03 Connection](modules/core/03-connection)

    1.3 [ICS 04 Channel](modules/core/04-channel)

    1.4 [ICS 05 Port](modules/core/05-port)

    1.5 [ICS 23 Commitment](modules/core/23-commitment/types)

    1.6 [ICS 24 Host](modules/core/24-host)

2. **Applications**

    2.1 [ICS 20 Fungible Token Transfers](modules/apps/transfer)

    2.2 [ICS 27 Interchain Accounts](modules/apps/27-interchain-accounts) (vendored; not wired into `paxd`)

3. **Light Clients**

    3.1 [ICS 07 Tendermint](modules/light-clients/07-tendermint)

    3.2 [ICS 06 Solo Machine](modules/light-clients/06-solomachine)

    3.3 [ICS 09 Localhost](modules/light-clients/09-localhost) (non-functional)

Also here: [`proto`](proto) definitions, protobuf generation [`scripts`](scripts), the [`testing`](testing) package with its simapp, and [`third_party`](third_party) proto dependencies.

## Resources

- [IBC Website](https://ibcprotocol.org/)
- [IBC Specification](https://github.com/cosmos/ibc)
- [Documentation](https://ibc.cosmos.network/main/ibc/overview.html)
