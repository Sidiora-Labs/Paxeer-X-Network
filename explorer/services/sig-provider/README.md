# <h1 align="center"> Sig-provider </h1>

**Sig-provider** is a service that aggregates and decodes Ethereum signatures 
for transactions and events from various sources. 
Given a transaction input or an unparsed event, 
it identifies possible decodings and parses the input accordingly.

Supported decoding sources include:

- [4byte directory](https://www.4byte.directory/)
- [Openchain signatures](https://openchain.xyz/signatures)
- [Ethereum bytecode database](https://docs.blockscout.com/about/features/ethereum-bytecode-database-microservice#solution-ethereum-bytecode-database-blockscout-ebd)

Sig-provider is used by Blockscout to display decoded transaction data 
on transaction pages and to determine transaction actions.

## Requirements
No additional dependencies

## How to enable
Set the following ENVs on blockscout instance:
- `MICROSERVICE_SIG_PROVIDER_ENABLED=true`
- `MICROSERVICE_SIG_PROVIDER_URL={service_url}`

## Envs
Here, we describe variables specific to this service. Variables common to all services (server, metrics, tracing) are defined in [`blockscout-service-launcher`](../libs/blockscout-service-launcher).

[anchor]: <> (anchors.envs.start)

| Variable                                          | Required | Description                                                          | Default value                                      |
|---------------------------------------------------|----------|----------------------------------------------------------------------|----------------------------------------------------|
| `SIG_PROVIDER__SOURCES__FOURBYTE`                 |          | 4bytes directory HTTP URL                                            | `https://www.4byte.directory/`                     |
| `SIG_PROVIDER__SOURCES__SIGETH`                   |          | Openchain Signature Database HTTP URL                                | `https://sig.eth.samczsun.com/`                    |
| `SIG_PROVIDER__SOURCES__ETH_BYTECODE_DB__ENABLED` |          | If enabled, will use ethereum bytecode database as one of data sources | `true`                                             |
| `SIG_PROVIDER__SOURCES__ETH_BYTECODE_DB__URL`     |          | Ethereum bytecode database HTTP URL                                  | `https://eth-bytecode-db.services.blockscout.com/` |

[anchor]: <> (anchors.envs.end)

## In this repository
The service is built from `docker/explorer-sig-provider/Dockerfile` at the repository root and published by `.github/workflows/explorer-images.yml` as `ghcr.io/sidiora-labs/paxeer-x-explorer-sig-provider`. `cargo check` in this directory builds it against the shared crates in `../libs`; [the explorer README](../../README.md) covers the lint script and the local stack.

## Upstream links
- Demo - https://sig-provider.services.blockscout.com
- [Swagger](https://blockscout.github.io/swaggers/services/sig-provider/index.html)
- [Packages](https://github.com/blockscout/blockscout-rs/pkgs/container/sig-provider)
- [Releases](https://github.com/blockscout/blockscout-rs/releases?q=sig-provider&expanded=true)