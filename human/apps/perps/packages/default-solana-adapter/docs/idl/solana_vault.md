# solana_vault.ts (IDL)

## solana_vault.ts responsibility

Defines the Anchor IDL type `SolanaVault` for the Solana vault program. It describes instructions (e.g. setVault, deposit, depositSol, oappQuote), their accounts and args, and related types. This file is consumed by helper.ts as `IDL` and `SolanaVault` for `Program<SolanaVault>` to build deposit and quote instructions.

## solana_vault.ts exports

| Name | Type | Role | Description |
|------|------|------|--------------|
| SolanaVault | type | IDL | Full IDL shape: version, name, instructions, accounts, types, events, errors |

## SolanaVault structure (summary)

- **version**: "0.1.0"
- **name**: "solana_vault"
- **instructions**: Array of instruction definitions; each has name, accounts (name, isMut, isSigner), args (name, type), and optional returns.
- **accounts**: Account struct definitions (name, type with fields).
- **types**: Custom types (e.g. SetVaultParams, DepositParams, OAppSendParams, MessagingReceipt) used in instructions.
- **events / errors**: Optional; not inferable from high-level summary.

Key instructions used by this package:

| Instruction | Purpose |
|-------------|---------|
| setVault | Admin set vault params |
| deposit | SPL token deposit into vault with OApp send |
| depositSol | Native SOL deposit into vault with OApp send |
| oappQuote | Quote native fee for OApp send (used by getDepositQuoteFee) |

## solana_vault.ts dependency and usage

- **Upstream**: None (hand-authored or generated IDL).
- **Downstream**: helper.ts imports IDL and SolanaVault for Program and method calls (deposit, depositSol, oappQuote).

## solana_vault.ts Example

```typescript
import { Program } from "@coral-xyz/anchor";
import { IDL as VaultIDL, SolanaVault } from "./idl/solana_vault";

const programId = new PublicKey("...");
const program = new Program<SolanaVault>(VaultIDL, programId, { connection });

const ix = await program.methods
  .deposit(depositParams, sendParam)
  .accounts({ /* ... */ })
  .remainingAccounts([/* ... */])
  .instruction();
```
