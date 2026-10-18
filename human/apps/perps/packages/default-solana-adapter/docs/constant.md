# constant.ts

## constant.ts responsibility

Exports Solana program IDs (Endpoint, SendLib, Executor, PriceFeed, DVN, etc.), PDA seeds (VaultAuthority, Broker, Token, SolVault), and environment-specific peer addresses and lookup table addresses for dev, QA, staging, and mainnet. LayerZero send uses three required DVNs (LZ, Canary, Nevermind): program IDs and config PDAs match solana-vault `constants.ts`. Used by helper and solana.util for vault and LayerZero/OApp PDAs and routing.

## constant.ts exports (selected)

| Name                         | Type       | Description                                        |
| ---------------------------- | ---------- | -------------------------------------------------- |
| ENDPOINT_PROGRAM_ID          | PublicKey  | Endpoint program                                   |
| SEND_LIB_PROGRAM_ID          | PublicKey  | Send library program                               |
| EXECUTOR_PROGRAM_ID          | PublicKey  | Executor program                                   |
| PRICE_FEED_PROGRAM_ID        | PublicKey  | Price feed program                                 |
| RECEIVE_LIB_PROGRAM_ID       | PublicKey  | Same as SEND_LIB_PROGRAM_ID                        |
| TREASURY_PROGRAM_ID          | PublicKey  | Same as SEND_LIB_PROGRAM_ID                        |
| DVN_PROGRAM_ID               | PublicKey  | DVN program (LZ)                                   |
| LZ_DVN_PROGRAM_ID            | PublicKey  | Alias of DVN_PROGRAM_ID                            |
| CANARY_DVN_PROGRAM_ID        | PublicKey  | Canary DVN program                                 |
| NEVERMIND_DVN_PROGRAM_ID     | PublicKey  | Nevermind DVN program                              |
| LZ_DVN_PDA                   | PublicKey  | LZ DVN config PDA (must match `getDvnConfigPda()`) |
| CANARY_DVN_PDA               | PublicKey  | Canary DVN config PDA                              |
| NEVERMIND_DVN_PDA            | PublicKey  | Nevermind DVN config PDA                           |
| VAULT_AUTHORITY_SEED         | string     | "VaultAuthority"                                   |
| BROKER_SEED                  | string     | "Broker"                                           |
| TOKEN_SEED                   | string     | "Token"                                            |
| SOL_VAULT_SEED               | string     | "SolVault"                                         |
| DEV_PEER_ADDRESS             | Uint8Array | Dev peer (bytes32)                                 |
| QA_PEER_ADDRESS              | Uint8Array | QA peer                                            |
| STAGING_PEER_ADDRESS         | Uint8Array | Staging peer                                       |
| MAINNET_PEER_ADDRESS         | Uint8Array | Mainnet peer                                       |
| DEV_DST_EID                  | number     | 40200                                              |
| MAIN_DST_EID                 | number     | 30213                                              |
| DEV_LOOKUP_TABLE_ADDRESS     | PublicKey  | Dev address lookup table                           |
| QA_LOOKUP_TABLE_ADDRESS      | PublicKey  | QA lookup table                                    |
| STAGING_LOOKUP_TABLE_ADDRESS | PublicKey  | Staging lookup table                               |
| MAINNET_LOOKUP_TABLE_ADDRESS | PublicKey  | Mainnet lookup table                               |
| DEV_OAPP_PROGRAM_ID          | PublicKey  | Dev OApp program                                   |
| QA_OAPP_PROGRAM_ID           | PublicKey  | QA OApp program                                    |
| STAGING_OAPP_PROGRAM_ID      | PublicKey  | Staging OApp program                               |
| MAINNET_OAPP_PROGRAM_ID      | PublicKey  | Mainnet OApp program                               |

## constant.ts dependency and usage

- **Upstream**: `@layerzerolabs/lz-v2-utilities` (addressToBytes32), `@solana/web3.js` (PublicKey).
- **Downstream**: `helper.ts`, `solana.util.ts` use these for PDAs and network selection.

## constant.ts Example

```typescript
import {
  VAULT_AUTHORITY_SEED,
  MAINNET_PEER_ADDRESS,
  MAINNET_LOOKUP_TABLE_ADDRESS,
  MAIN_DST_EID,
} from "@paxeer-network/default-solana-adapter/src/constant";

// Use with PublicKey.findProgramAddressSync([Buffer.from(VAULT_AUTHORITY_SEED, "utf8")], programId)
```
