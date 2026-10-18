# walletAdapter.ts

## walletAdapter.ts responsibility

Implements the paxeer-network Solana wallet adapter by extending `BaseWalletAdapter`. It handles Solana connection setup, message signing (including Ledger path), balance queries (native SOL and SPL), and vault deposit transactions. It generates and signs all paxeer-network message types (register account, add paxeer-network key, withdraw, settle, internal transfer, dex request) for the Solana chain.

## walletAdapter.ts exports

| Name | Type | Role | Description |
|------|------|------|--------------|
| DefaultSolanaWalletAdapter | class | Adapter | Solana implementation of BaseWalletAdapter |
| DefaultSolanaWalletAdapter (export) | class | Same | Named export of the class |

## DefaultSolanaWalletAdapter responsibility

Bridges a Solana wallet (via SolanaWalletProvider) to paxeer-network: provides address, chainId, connection, signMessage, getBalance, getBalances, getBalanceByAddress, call, sendTransaction, callOnChain, estimateGasFee, and pollTransactionReceiptWithBackoff. Builds and signs paxeer-network messages for Solana (chainType "SOL") and executes vault deposits via the helper deposit flow.

## DefaultSolanaWalletAdapter input and output

- **Input**: Config via `SolanaAdapterOption` (provider, address, chain) in `active()` / `update()`. Various inputs for message generation (RegisterAccountInputs, WithdrawInputs, etc.) and for contract calls (address, method, params, payload).
- **Output**: Address string, chainId number, Connection instance, signature strings, balance bigints, transaction results from sendTransaction / deposit.

## DefaultSolanaWalletAdapter main properties and methods

| Name | Type | Description |
|------|------|-------------|
| chainNamespace | ChainNamespace | Set to `ChainNamespace.solana` |
| address | getter | Wallet address (string) |
| chainId | getter/setter | Chain id (number) |
| connection | getter | Solana Connection (from provider.rpcUrl, network, or API proxy) |
| active(config) | method | Applies config (address, chainId, provider) |
| deactivate() | method | Lifecycle hook (log only) |
| update(config) | method | Re-applies config |
| generateSecretKey() | method | Returns new random secret key as base58 string (44 chars) |
| signMessage(message) | method | Signs Uint8Array; Ledger uses memo instruction path |
| generateRegisterAccountMessage(inputs) | method | Returns signed Message (chainType "SOL") |
| generateWithdrawMessage(inputs) | method | Returns signed Message with SignatureDomain |
| generateInternalTransferMessage(inputs) | method | Returns signed Message with SignatureDomain |
| generateAddPaxeerNetworkKeyMessage(inputs) | method | Returns signed Message |
| generateSettleMessage(inputs) | method | Returns signed Message with SignatureDomain |
| generateDexRequestMessage(inputs) | method | Returns signed Message with domain |
| getBalance() | method | Native SOL balance (lamports) as bigint |
| getBalances(addresses) | method | Balances for token addresses (SOL + SPL) in same order |
| getBalanceByAddress(address) | method | SPL token balance for token mint address |
| call(address, method, params, options) | method | balanceOf → getBalanceByAddress; allowance → MaxUint256 |
| sendTransaction(contractAddress, method, payload, options) | method | deposit → helper deposit() |
| callOnChain(chain, address, method, params, options) | method | getDepositFee → getDepositQuoteFee |
| estimateGasFee(...) | method | Returns 0n |
| pollTransactionReceiptWithBackoff(...) | method | Resolves with { status: 1 } |

## DefaultSolanaWalletAdapter dependency and call relationship

- **Upstream**: Configured by wallet connector with `SolanaAdapterOption`; called by core for signing and sending.
- **Downstream**: `@paxeer-network/core` (BaseWalletAdapter, message types, Account, SimpleDI), `@paxeer-network/types` (API, MaxUint256, isNativeTokenChecker), `./helper` (message builders, deposit, getDepositQuoteFee, checkIsLedgerWallet), `./solana.util` (getTokenAccounts), `./types`.

## DefaultSolanaWalletAdapter execution flow (sign and deposit)

1. **Connection**: Resolve Connection from config (provider.connection, provider.rpcUrl, or Devnet clusterApiUrl, or API proxy with paxeer-network key signing).
2. **Sign message**: If Ledger (checkIsLedgerWallet), build memo-based transaction and sign via provider.signTransaction; else sign raw bytes via provider.signMessage.
3. **Message generation**: Each generate* method uses helper to build [message, toSignatureMessage], then signMessage(toSignatureMessage), then return { message: { ...message, chainType: "SOL" }, signatured, domain? }.
4. **Deposit**: sendTransaction(..., method: "deposit") delegates to helper deposit() with vault address, user address, connection, provider.sendTransaction, and payload.data[0] as depositData.

## DefaultSolanaWalletAdapter errors and boundaries

| Scenario | Condition | Behavior | Handling |
|----------|-----------|----------|----------|
| Ledger sign failure | Unsupported signature from signTransaction | Throws "Unsupported signature" | Caller must handle |
| getDepositQuoteFee failure | AccountNotFound in simulate | Throws "Account gas is insufficient." | Caller must handle |
| Other RPC/simulate errors | feeRes.value.err | Throws with error info | Caller must handle |

## DefaultSolanaWalletAdapter extension and modification points

- **Connection source**: Change logic in `connection` getter to add more RPC sources or middleware.
- **Ledger signing**: Adjust memo/compute budget instructions in `signMessage` for Ledger.
- **New contract methods**: Extend `call`, `sendTransaction`, `callOnChain` for new method names and parameters.

## DefaultSolanaWalletAdapter Example

```typescript
import { DefaultSolanaWalletAdapter } from "@paxeer-network/default-solana-adapter";
import type { SolanaAdapterOption } from "@paxeer-network/default-solana-adapter";

const adapter = new DefaultSolanaWalletAdapter();
const option: SolanaAdapterOption = {
  provider: { /* SolanaWalletProvider */ },
  address: "9WzDXwBbmkg8ZTbNMqUxvQRAyrZzDsGYdLVL9zYtAWWM",
  chain: { id: 101 },
};
adapter.active(option);
const balance = await adapter.getBalance();
const msg = await adapter.generateRegisterAccountMessage({
  brokerId: "broker",
  registrationNonce: 1,
  timestamp: Date.now(),
});
```
