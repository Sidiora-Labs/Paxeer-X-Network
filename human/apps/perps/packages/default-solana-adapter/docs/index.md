# default-solana-adapter Package Documentation

## Package overview

The `default-solana-adapter` package provides a Solana wallet adapter implementation for the paxeer-network Network. It bridges Solana wallets (including Ledger) with paxeer-network's core wallet interface for registration, signing, deposits, withdrawals, internal transfers, and DEX requests on Solana.

## Module responsibilities

| Responsibility | Description |
|----------------|-------------|
| Wallet adapter | Implements `BaseWalletAdapter` for Solana: address, chainId, connection, signMessage, balance, sendTransaction |
| Message generation | Builds and signs paxeer-network messages (register, add key, withdraw, settle, internal transfer, dex request) for Solana |
| Deposit / fee | Solana vault deposit (SOL and SPL) and deposit quote fee via LayerZero/OApp |
| PDA / RPC helpers | Solana PDAs (vault, broker, token, OApp, peer, etc.) and RPC utilities (token accounts, lookup tables) |

## Key entities

| Entity | Type | Role | Entry |
|--------|------|------|--------|
| DefaultSolanaWalletAdapter | class | Solana wallet adapter for paxeer-network | `walletAdapter.ts` |
| SolanaWalletProvider | interface | Provider contract for Solana wallet (connection, sign, send) | `types.ts` |
| SolanaAdapterOption | interface | Adapter config: provider, address, chain | `types.ts` |
| helper functions | functions | Message builders and deposit/quote fee | `helper.ts` |
| solana.util | module | PDA and connection utilities | `solana.util.ts` |
| constant | module | Program IDs, seeds, peer addresses, lookup tables | `constant.ts` |

## Directory structure

- [idl/](idl/index.md) — Solana vault IDL type definitions

## Top-level files

| File | Language | Responsibility | Entry symbol | Link |
|------|-----------|-----------------|--------------|------|
| index.ts | TypeScript | Package exports | version, DefaultSolanaWalletAdapter, SolanaWalletProvider | [index.ts.md](index.ts.md) |
| types.ts | TypeScript | Adapter and provider types | SolanaAdapterOption, SolanaWalletProvider | [types.md](types.md) |
| walletAdapter.ts | TypeScript | Solana wallet adapter implementation | DefaultSolanaWalletAdapter | [walletAdapter.md](walletAdapter.md) |
| constant.ts | TypeScript | Program IDs, seeds, peer/lookup addresses | ENDPOINT_PROGRAM_ID, VAULT_AUTHORITY_SEED, etc. | [constant.md](constant.md) |
| helper.ts | TypeScript | Message encoding and deposit/fee logic | addPaxeerNetworkKeyMessage, deposit, getDepositQuoteFee, checkIsLedgerWallet | [helper.md](helper.md) |
| solana.util.ts | TypeScript | PDA and lookup table helpers | getTokenAccounts, getVaultAuthorityPda, getLookupTableAddress | [solana.util.md](solana.util.md) |
| version.ts | TypeScript | Package version | default export string | [version.md](version.md) |

## Search keywords

Solana, wallet adapter, paxeer-network, BaseWalletAdapter, SolanaWalletProvider, deposit, withdraw, register account, add paxeer-network key, settle, internal transfer, dex request, Ledger, LayerZero, OApp, vault, PDA.
