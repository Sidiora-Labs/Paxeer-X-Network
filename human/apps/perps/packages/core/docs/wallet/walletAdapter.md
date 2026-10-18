# walletAdapter.ts

## walletAdapter.ts Responsibility

Defines the main wallet adapter contract for paxeer-network: `WalletAdapter` interface (chainNamespace, chainId, address, lifecycle active/update/deactivate, generateSecretKey, EIP-712 message generators for register/withdraw/internalTransfer/settle/addPaxeerNetworkKey/dexRequest, call/sendTransaction/estimateGasFee/callOnChain, getBalance/getBalances, parseUnits/formatUnits, pollTransactionReceiptWithBackoff, on/off). Also exports input types: RegisterAccountInputs, WithdrawInputs, InternalTransferInputs, SettleInputs, AddPaxeerNetworkKeyInputs, DexRequestInputs, Message.

## walletAdapter.ts Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| ChainType | type | "EVM" \| "SOL" | Chain type |
| Message | type | { message, signatured } | Signed message shape |
| RegisterAccountInputs | type | Input | registrationNonce, brokerId, timestamp |
| WithdrawInputs | type | Input | brokerId, receiver, token, amount, nonce, timestamp, verifyContract? |
| InternalTransferInputs | type | Input | receiver, token, amount, nonce, verifyContract? |
| SettleInputs | type | Input | brokerId, settlePnlNonce, timestamp, verifyContract? |
| AddPaxeerNetworkKeyInputs | type | Input | publicKey, brokerId, expiration, timestamp, scope?, tag?, subAccountId? |
| DexRequestInputs | type | Input | payloadType, nonce, receiver, amount, vaultId, token, dexBrokerId, timestamp, domain |
| WalletAdapter | interface | Contract | Full adapter API |

## WalletAdapter Responsibility

Single interface for EVM and Solana (and other) wallets: lifecycle, EIP-712 message generation for paxeer-network flows, contract call/sendTransaction, balance and units. Account and Assets use the active adapter from WalletAdapterManager.

## WalletAdapter Key Members

| Member | Type | Description |
|--------|------|-------------|
| chainNamespace | ChainNamespace | EVM / SOL |
| chainId | number (get/set) | Current chain ID |
| address | string (get/set) | Wallet address |
| active(config) | void | Activate with config |
| update(config) | void | Update config |
| deactivate() | void | Tear down |
| generateSecretKey() | string | New Ed25519 secret key (base58) |
| generateRegisterAccountMessage(inputs) | Promise<Message> | Registration EIP-712 |
| generateWithdrawMessage(inputs) | Promise<Message & { domain }> | Withdraw EIP-712 |
| generateInternalTransferMessage(inputs) | Promise<Message & { domain }> | Internal transfer EIP-712 |
| generateSettleMessage(inputs) | Promise<Message & { domain }> | Settle EIP-712 |
| generateAddPaxeerNetworkKeyMessage(inputs) | Promise<Message> | Add paxeer-network Key EIP-712 |
| generateDexRequestMessage(inputs) | Promise<Message & { domain }> | DEX request EIP-712 |
| call(address, method, params, options?) | Promise<any> | Contract read |
| sendTransaction(contractAddress, method, payload, options) | Promise<any> | Contract write |
| estimateGasFee?(...) | Promise<bigint> | Gas estimate (optional) |
| callOnChain(chain, address, method, params, options) | Promise<any> | Read on another chain |
| getBalance() | Promise<bigint> | Native balance |
| getBalances(addresses) | Promise<any> | Token balances |
| pollTransactionReceiptWithBackoff(txHash, ...) | Promise<any> | Wait for receipt |
| parseUnits, formatUnits | functions | Amount encoding |
| on, off | event | Event subscription |

## walletAdapter.ts Dependencies and Call Relationships

- **Upstream**: @paxeer-network/types (API, ChainNamespace), ../utils (SignatureDomain).
- **Downstream**: baseWalletAdapter.ts implements; Account/Assets use adapter for signing and contract calls.

## walletAdapter.ts Example

```typescript
import type {
  WalletAdapter,
  RegisterAccountInputs,
  WithdrawInputs,
  Message,
} from "@paxeer-network/core";

const adapter: WalletAdapter = evmWalletAdapter;
const msg: Message = await adapter.generateRegisterAccountMessage({
  registrationNonce: 1,
  brokerId: "paxeer-network",
  timestamp: Date.now(),
});
await adapter.call(tokenAddress, "balanceOf", [userAddress], { abi });
```
