# getChainConfirmations.ts

> Location: `packages/ui-transfer/src/contract/getChainConfirmations.ts`

## Overview

Reads required deposit confirmations via **paxeer-network Receive** LayerZero ULN config:

1. Resolve the deposit OApp (mainnet EVM RelayV2 / Solana peer).
2. `EndpointV2.getReceiveLibrary(oapp, sourceEid)`.
3. `ReceiveLib.getAppUlnConfig(oapp, sourceEid)` → `confirmations`.

When the OApp ULN config is unset (`confirmations = 0`), falls back to the legacy default ULN config (`oapp = 0x0`).

## Exports

### getChainConfirmations(chain: API.Chain): Promise\<number\>

| Parameter | Type      | Description                                       |
| --------- | --------- | ------------------------------------------------- |
| chain     | API.Chain | Chain with `network_infos.chain_id` and `mainnet` |

**Returns:** Required confirmations count for the deposit pathway.
