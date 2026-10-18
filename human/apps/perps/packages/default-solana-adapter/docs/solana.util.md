# solana.util.ts

## solana.util.ts responsibility

Provides Solana PDA derivation and RPC helpers used by the vault and LayerZero/OApp flow: token accounts, vault authority, broker/token PDAs, OApp config, peer, enforced options, send lib/config, endpoint/nonce/event/ULN/executor/price feed/DVN PDAs, destination EID, lookup table address and account fetch. For ULN 3-DVN send, `appendThreeDvnQuoteRemainingAccounts` / `appendThreeDvnDepositRemainingAccounts` build the CPI tail (three DVN groups, each with price-feed pair) aligned with solana-vault `utils.ts`. Address lookup tables used for v0 txs must include Canary and Nevermind program and PDA addresses (extend via solana-vault `extend_lookuptable_dvn.ts` on each network).

## solana.util.ts exports

| Name                                   | Type     | Role         | Description                                                        |
| -------------------------------------- | -------- | ------------ | ------------------------------------------------------------------ |
| getTokenAccounts                       | function | ATA          | Associated token account for (token, owner)                        |
| getVaultAuthorityPda                   | function | PDA          | Vault authority PDA for program                                    |
| getSolVaultPda                         | function | PDA          | Sol vault PDA for program                                          |
| getBrokerPDA                           | function | PDA          | Broker PDA (programId, brokerHash)                                 |
| getTokenPDA                            | function | PDA          | Token PDA (programId, tokenHash)                                   |
| getOAppConfigPda                       | function | PDA          | OApp config PDA                                                    |
| getLzReceiveTypesPda                   | function | PDA          | LZ receive types PDA                                               |
| getPeerPda                             | function | PDA          | Peer PDA (programId, oappConfigPda, dstEid)                        |
| getEndorcedOptionsPda                  | function | PDA          | Enforced options PDA                                               |
| getSendLibPda                          | function | PDA          | Send lib PDA                                                       |
| getSendLibConfigPda                    | function | PDA          | Send lib config PDA (oappConfigPda, dstEid)                        |
| getDefaultSendLibConfigPda             | function | PDA          | Default send lib config PDA (dstEid)                               |
| getSendLibInfoPda                      | function | PDA          | Send lib info PDA                                                  |
| getEndpointSettingPda                  | function | PDA          | Endpoint setting PDA                                               |
| getPeerAddress                         | function | Resolver     | Peer bytes by OApp program id (dev/qa/staging/mainnet)             |
| getNoncePda                            | function | PDA          | Nonce PDA (programId, oappConfigPda, dstEid)                       |
| getEventAuthorityPda                   | function | PDA          | Event authority PDA (endpoint)                                     |
| getUlnSettingPda                       | function | PDA          | ULN setting PDA                                                    |
| getSendConfigPda                       | function | PDA          | Send config PDA (oappConfigPda, dstEid)                            |
| getDefaultSendConfigPda                | function | PDA          | Default send config PDA (dstEid)                                   |
| getUlnEventAuthorityPda                | function | PDA          | ULN event authority PDA                                            |
| getExecutorConfigPda                   | function | PDA          | Executor config PDA                                                |
| getPriceFeedPda                        | function | PDA          | Price feed PDA                                                     |
| getDvnConfigPda                        | function | PDA          | LZ DVN config PDA; asserts derivation matches `LZ_DVN_PDA`         |
| appendThreeDvnQuoteRemainingAccounts   | function | CPI accounts | Tail for `oappQuote` after executor + first price-feed pair        |
| appendThreeDvnDepositRemainingAccounts | function | CPI accounts | Tail for `deposit` / `depositSol` (DVN PDAs writable)              |
| getDstEID                              | function | Resolver     | MAIN_DST_EID or DEV_DST_EID by OApp program id                     |
| getLookupTableAddress                  | function | Resolver     | Lookup table PublicKey by OApp program id (dev/qa/staging/mainnet) |
| getMessageLibPda                       | function | PDA          | Message lib PDA (optional programId)                               |
| getMessageLibInfoPda                   | function | PDA          | Message lib info PDA (msgLibPda, optional programId)               |
| getLookupTableAccount                  | function | async RPC    | Fetches address lookup table account from connection               |

## getTokenAccounts parameters and return

- **Parameters**: token (PublicKey), owner (PublicKey).
- **Return**: PublicKey of the associated token account (getAssociatedTokenAddressSync with allowOwnerOffCurve: true).

## getVaultAuthorityPda / getSolVaultPda parameters

- **getVaultAuthorityPda(VAULT_PROGRAM_ID)**: Seed VAULT_AUTHORITY_SEED.
- **getSolVaultPda(VAULT_PROGRAM_ID)**: Seed SOL_VAULT_SEED.

## getBrokerPDA / getTokenPDA parameters

- **getBrokerPDA(programId, brokerHash)**: brokerHash is hex string (e.g. "0x..."); converted to buffer and used with BROKER_SEED.
- **getTokenPDA(programId, tokenHash)**: tokenHash hex with TOKEN_SEED.

## getPeerAddress / getDstEID / getLookupTableAddress behavior

Each compares OAPP_PROGRAM_ID (or OAPP_PROGRAM_ID) to DEV/QA/STAGING/MAINNET_OAPP_PROGRAM_ID from constant and returns the corresponding peer address (Uint8Array), DST EID (number), or lookup table PublicKey. Defaults to dev when no match.

## solana.util.ts dependency and usage

- **Upstream**: @layerzerolabs/lz-solana-sdk-v2 (seeds), @solana/spl-token (getAssociatedTokenAddressSync), @solana/web3.js, ./constant.
- **Downstream**: helper.ts, walletAdapter.ts (getTokenAccounts).

## solana.util.ts Example

```typescript
import { PublicKey } from "@solana/web3.js";
import {
  getTokenAccounts,
  getVaultAuthorityPda,
  getBrokerPDA,
  getLookupTableAddress,
  getLookupTableAccount,
} from "./solana.util";

const programId = new PublicKey("...");
const vaultAuthority = getVaultAuthorityPda(programId);
const brokerPda = getBrokerPDA(programId, "0x1234...");
const ata = getTokenAccounts(tokenMint, owner);
const lookupAddress = getLookupTableAddress(programId);
const lookupAccount = await getLookupTableAccount(connection, lookupAddress);
```
