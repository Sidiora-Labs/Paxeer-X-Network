# wallet Directory Index

## Directory Description

`wallet` provides multi-chain wallet adapter definitions and implementations: WalletAdapter interface (EVM/Solana), BaseWalletAdapter, and legacy IWalletAdapter (EtherAdapter, Web3WalletAdapter) plus adapter option types.

## Directory Responsibility and Boundary

- **Responsible for**: WalletAdapter lifecycle (active/update/deactivate), EIP-712 message generation (register, withdraw, settle, addPaxeerNetworkKey, dexRequest), call/sendTransaction, parseUnits/formatUnits.
- **Not responsible for**: Wallet UI, connection flow, chain ID enums (provided by types package).

## File List

| File | Language | Summary | Entry symbols | Link |
|------|----------|--------|---------------|------|
| walletAdapter.ts | TS | WalletAdapter interface and message input types | WalletAdapter, RegisterAccountInputs, WithdrawInputs, SettleInputs, AddPaxeerNetworkKeyInputs, DexRequestInputs, Message | [walletAdapter.md](./walletAdapter.md) |
| baseWalletAdapter.ts | TS | Abstract base (generateSecretKey, parseUnits/formatUnits) | BaseWalletAdapter | [baseWalletAdapter.md](./baseWalletAdapter.md) |
| adapter.ts | TS | Legacy IWalletAdapter and WalletAdapterOptions | IWalletAdapter, WalletAdapterOptions, getWalletAdapterFunc | [adapter.md](./adapter.md) |
| etherAdapter.ts | TS | EVM adapter using ethers BrowserProvider (@deprecated) | EtherAdapter | [etherAdapter.md](./etherAdapter.md) |
| web3Adapter.ts | TS | Web3-based placeholder impl | Web3WalletAdapter | [web3Adapter.md](./web3Adapter.md) |
| index.ts | TS | Re-exports adapter types | See adapter | [entry.md](./entry.md) |

## Key Entities

| Entity | File | Responsibility | Dependency |
|--------|------|----------------|------------|
| WalletAdapter | walletAdapter.ts | New adapter contract: chainNamespace, message generation, call, sendTransaction | SignatureDomain (utils) |
| BaseWalletAdapter | baseWalletAdapter.ts | Abstract base; default generateSecretKey/parseUnits/formatUnits | WalletAdapter, SimpleDI, Account |
| IWalletAdapter | adapter.ts | Legacy adapter (chainId, addresses, sendTransaction, signTypedData) | None |
| EtherAdapter | etherAdapter.ts | ethers EVM impl (call, sendTransaction, pollTransactionReceiptWithBackoff) | IWalletAdapter |

## Subdirectories

None.
