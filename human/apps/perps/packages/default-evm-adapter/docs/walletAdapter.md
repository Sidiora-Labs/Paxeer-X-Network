# walletAdapter.ts

## walletAdapter.ts Responsibility

Implements the default EVM wallet adapter by extending `BaseWalletAdapter`. It uses a `Web3Provider` for EIP-712 signing and RPC (call, sendTransaction, getBalance, pollTransactionReceiptWithBackoff). The adapter builds typed messages via helper functions and delegates signing and chain interaction to the provider.

## walletAdapter.ts Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| DefaultEVMWalletAdapter | class | EVM wallet adapter | Single public export |

## DefaultEVMWalletAdapter Responsibility

EVM implementation of paxeer-network wallet adapter: lifecycle (active/update/deactivate), message generation for registration, add key, withdraw, internal transfer, settle, DEX request, and provider-backed call/sendTransaction/getBalance/estimateGasFee. Uses EIP-712 domain from contract manager when on-chain verification is required.

## DefaultEVMWalletAdapter Input and Output

- **Input**: Constructor takes `Web3Provider`. Methods take `EVMAdapterOptions` (active, update) or operation-specific inputs from `@paxeer-network/core`.
- **Output**: Getters (address, chainId), message objects with `message` and `signatured`, or raw results from provider (call, sendTransaction, getBalance, etc.).

## DefaultEVMWalletAdapter Constructor and Config

| Item | Type | Description |
|------|------|-------------|
| constructor(web3Provider) | Web3Provider | Injected once; used for all signing and RPC |
| setConfig(config) | EVMAdapterOptions | Sets _address, _chainId, optional provider swap, and contractManager (required) |
| active(config) | EVMAdapterOptions | setConfig + lifecycle event "active" |
| update(config) | EVMAdapterOptions | lifecycle "update" + setConfig |
| deactivate() | void | lifecycle "deactivate" |

## DefaultEVMWalletAdapter Public Properties and Getters

| Name | Type | Description |
|------|------|-------------|
| chainNamespace | ChainNamespace | Always `ChainNamespace.evm` |
| address | string | Current wallet address (from config) |
| chainId | number | Current chain id (get/set) |

## DefaultEVMWalletAdapter Message Generation Methods

Each returns a structure with `message` (including `chainType: "EVM"`) and `signatured` (result of signTypedData). Withdraw, internal transfer, settle, and DEX request also attach `domain` to the return value.

| Method | Input | Return |
|--------|--------|--------|
| generateRegisterAccountMessage | RegisterAccountInputs | Promise\<Message\> |
| generateAddPaxeerNetworkKeyMessage | AddPaxeerNetworkKeyInputs | Promise\<Message\> |
| generateWithdrawMessage | WithdrawInputs | Promise\<Message & { domain }\> |
| generateInternalTransferMessage | InternalTransferInputs | Promise\<Message & { domain }\> |
| generateSettleMessage | SettleInputs | Promise\<Message & { domain }\> |
| generateDexRequestMessage | DexRequestInputs | Promise\<Message & { domain }\> |

## DefaultEVMWalletAdapter Provider Delegation Methods

| Method | Purpose |
|--------|---------|
| call(address, method, params, options?) | Read contract via Web3Provider.call |
| sendTransaction(contractAddress, method, payload, options) | Send tx via Web3Provider.sendTransaction |
| callOnChain(chain, address, method, params, options) | Read on a specific chain |
| getDomain(onChainDomain?) | Build EIP-712 domain; if onChainDomain use contractManager.getContractInfoByEnv().verifyContractAddress |
| pollTransactionReceiptWithBackoff(txHash, baseInterval?, maxInterval?, maxRetries?) | Poll receipt via provider |
| getBalance() | Web3Provider.getBalance(this.address) |
| getBalances(addresses) | Web3Provider.getBalances |
| estimateGasFee(contractAddress, method, payload, options) | Web3Provider.estimateGasFee |

## DefaultEVMWalletAdapter Other Methods

| Method | Description |
|--------|-------------|
| generateSecretKey() | Generates a 44-char base58 secret key using @noble/ed25519 and bs58 |

## DefaultEVMWalletAdapter Dependencies and Callers

- **Upstream**: `@paxeer-network/core` (BaseWalletAdapter, IContract, input types, Message, SignatureDomain), `@paxeer-network/types` (API, ChainNamespace), `./helper` (message builders), `./provider/web3Provider.interface` (Web3Provider), `./types` (EVMAdapterOptions).
- **Downstream**: Consumers that instantiate the adapter and pass a Web3Provider; core uses the adapter for registration, trading, withdraw, etc.

## DefaultEVMWalletAdapter Implementation Flow (Message Generation)

1. Resolve domain via getDomain (onChainDomain true for withdraw/internalTransfer/settle/dexRequest where applicable).
2. Call the corresponding helper (e.g. registerAccountMessage) with inputs plus chainId and domain.
3. Call signTypedData with the current address and stringified toSignatureMessage.
4. Return { message: { ...message, chainType: "EVM" }, signatured } (and domain when required).

## DefaultEVMWalletAdapter Errors and Boundaries

| Scenario | Condition | Behavior | Handling |
|----------|-----------|----------|----------|
| Missing contract manager | config.contractManager not provided in setConfig | throw "Please provide contract manager class" | Caller must pass contractManager in EVMAdapterOptions |
| Missing web3Provider | getDomain called but web3Provider falsy | throw "web3Provider is undefined" | Ensure adapter is constructed with a valid Web3Provider |

## DefaultEVMWalletAdapter Extension and Modification Points

- **Lifecycle logging**: `lifecycleName` currently logs to console; replace or extend for analytics.
- **Message builders**: All EIP-712 message shapes are built in `helper.ts`; change domain or primary types there and in `@paxeer-network/types` definedTypes.
- **Domain**: getDomain logic (name, version, verifyingContract) is the single place for EIP-712 domain for the EVM adapter.

## DefaultEVMWalletAdapter Example

```typescript
import { DefaultEVMWalletAdapter } from "@paxeer-network/default-evm-adapter";
import type { Web3Provider } from "@paxeer-network/default-evm-adapter";
import { myContractManager } from "./contractManager";

const web3Provider: Web3Provider = createMyWeb3Provider(window.ethereum);
const adapter = new DefaultEVMWalletAdapter(web3Provider);

adapter.active({
  provider: window.ethereum,
  address: "0x...",
  chain: { id: 421614 },
  contractManager: myContractManager,
});

const msg = await adapter.generateRegisterAccountMessage({
  brokerId: "broker",
  registrationNonce: 1,
  timestamp: Date.now(),
});
// msg.message, msg.signatured used by core for registration.
const balance = await adapter.getBalance();
```
