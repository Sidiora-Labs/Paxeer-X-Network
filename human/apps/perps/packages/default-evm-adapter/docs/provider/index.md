# provider Directory Index

## Directory Responsibility and Boundary

- **Responsibility**: Defines the Web3 provider contract used by the default EVM adapter. The adapter delegates all chain RPC, signing, and transaction polling to this interface.
- **Not responsible for**: Concrete provider implementations (e.g. MetaMask, WalletConnect); those live in consumers or other packages.

## Files in This Directory

| File | Language | Summary | Entry symbols | Link |
|------|----------|---------|---------------|------|
| web3Provider.interface.ts | TypeScript | Web3Provider interface and Eip1193Provider type for EVM RPC and EIP-712 signing | Web3Provider, Eip1193Provider | [web3Provider.interface.md](web3Provider.interface.md) |

## Key Entities

| Entity | File | Responsibility | Dependency |
|--------|------|----------------|------------|
| Web3Provider | web3Provider.interface.ts | Interface for signTypedData, call, sendTransaction, getBalance, pollTransactionReceiptWithBackoff, etc. | Used by DefaultEVMWalletAdapter |
| Eip1193Provider | web3Provider.interface.ts | Type for injected provider with `request({ method, params })` | Set on Web3Provider.provider by adapter |
