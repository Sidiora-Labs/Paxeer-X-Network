# idl directory

## idl directory responsibility

Contains the Anchor IDL type definition for the Solana vault program. Used by helper (Program, methods deposit/depositSol/oappQuote) to build instructions and simulate fee. Not a runtime executable; types only.

## Files in this directory

| File | Language | Responsibility | Link |
|------|----------|----------------|------|
| solana_vault.ts | TypeScript | SolanaVault IDL type (instructions, accounts, args, types) | [solana_vault.md](solana_vault.md) |
