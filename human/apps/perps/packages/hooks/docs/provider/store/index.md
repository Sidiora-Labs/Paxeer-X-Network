# provider/store — Directory Index

## Directory Responsibility

Zustand stores for chain info (mainnet/testnet), main/test token config, symbol store, and swap support. Used by config and UI to resolve chains and tokens.

## Files

| File | Language | Summary | Entry symbol(s) | Link |
|------|----------|---------|------------------|------|
| index.ts | TS | Re-exports stores | useMainnetChainsStore, useTestnetChainsStore, useMainTokenStore, useTestTokenStore, useSwapSupportStore | [index.md](index.md) |
| chainInfoMainStore.ts | TS | Mainnet chains store | useMainnetChainsStore | [chainInfoMainStore.md](chainInfoMainStore.md) |
| chainInfoTestStore.ts | TS | Testnet chains store | useTestnetChainsStore | [chainInfoTestStore.md](chainInfoTestStore.md) |
| mainTokenStore.ts | TS | Main token store | useMainTokenStore | [mainTokenStore.md](mainTokenStore.md) |
| testTokenStore.ts | TS | Test token store | useTestTokenStore | [testTokenStore.md](testTokenStore.md) |
| swapSupportStore.ts | TS | Swap support store | useSwapSupportStore | [swapSupportStore.md](swapSupportStore.md) |
| symbolStore.ts | TS | Symbol store | (store) | [symbolStore.md](symbolStore.md) |
| createDataStore.ts | TS | Factory for data stores | createDataStore | [createDataStore.md](createDataStore.md) |
| storeContext.tsx | TSX | Store context | (context) | [storeContext.md](storeContext.md) |
| storeProvider.tsx | TSX | Store provider | (provider) | [storeProvider.md](storeProvider.md) |
