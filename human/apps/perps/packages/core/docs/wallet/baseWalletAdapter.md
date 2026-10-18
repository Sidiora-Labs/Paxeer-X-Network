# baseWalletAdapter.ts

## baseWalletAdapter.ts Responsibility

Abstract base class for `WalletAdapter`: declares abstract methods for message generation (register, withdraw, internalTransfer, settle, addPaxeerNetworkKey, dexRequest), call, sendTransaction, callOnChain, estimateGasFee, getBalance, getBalances, pollTransactionReceiptWithBackoff, deactivate, and chainNamespace. Implements generateSecretKey (ed25519 + bs58), parseUnits/formatUnits (ethers), and signMessageByPaxeerNetworkKey (via SimpleDI.get Account and account.signer).

## baseWalletAdapter.ts Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| BaseWalletAdapter | class | Abstract base | Partial WalletAdapter impl |

## BaseWalletAdapter Responsibility

Shared implementation for generateSecretKey and unit parsing; signMessageByPaxeerNetworkKey for paxeer-network API headers. Concrete adapters (EVM, Solana) extend and implement chain-specific message generation and contract calls.

## BaseWalletAdapter Implemented Methods

| Method | Description |
|--------|-------------|
| generateSecretKey() | Random ed25519 private key, bs58-encoded; loop until length 44 |
| parseUnits(amount, decimals) | ethers.parseUnits(amount, decimals).toString() |
| formatUnits(amount, decimals) | ethers.formatUnits(amount, decimals) |
| signMessageByPaxeerNetworkKey(payload) | Gets Account from SimpleDI.get("account"), uses account.signer.sign(payload), adds paxeer-network-account-id |

## BaseWalletAdapter Abstract Methods

- generateRegisterAccountMessage, generateWithdrawMessage, generateInternalTransferMessage, generateSettleMessage, generateAddPaxeerNetworkKeyMessage, generateDexRequestMessage.
- call, sendTransaction, callOnChain, estimateGasFee.
- getBalance, getBalances.
- pollTransactionReceiptWithBackoff.
- deactivate.
- chainNamespace (getter).

## baseWalletAdapter.ts Dependencies and Call Relationships

- **Upstream**: @noble/ed25519, bs58, ethers, @paxeer-network/types, ../account (Account), ../di/simpleDI, ../utils (getTimestamp, SignatureDomain), ./walletAdapter (Message, *Inputs, WalletAdapter).
- **Downstream**: EVM and Solana adapter classes extend BaseWalletAdapter.

## baseWalletAdapter.ts Example

```typescript
import { BaseWalletAdapter } from "@paxeer-network/core";

class MyChainAdapter extends BaseWalletAdapter<MyConfig> {
  chainNamespace = ChainNamespace.evm;
  // ... implement abstract methods
}
const secretKey = adapter.generateSecretKey();
const headers = await adapter.signMessageByPaxeerNetworkKey(payload);
```
