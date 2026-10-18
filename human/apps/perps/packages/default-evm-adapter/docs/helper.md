# helper.ts

## helper.ts Responsibility

Builds EIP-712 typed data (message + full struct for signing) for paxeer-network EVM flows: registration, add paxeer-network key, withdraw, internal transfer, settle PnL, and DEX request. Each function returns a tuple `[message, toSignatureMessage]` for the adapter to sign via `Web3Provider.signTypedData`.

## helper.ts Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| withdrawMessage | function | EIP-712 builder | Withdraw message and toSignatureMessage |
| internalTransferMessage | function | EIP-712 builder | Internal transfer message and toSignatureMessage |
| addPaxeerNetworkKeyMessage | function | EIP-712 builder | AddPaxeerNetworkKey message and toSignatureMessage |
| registerAccountMessage | function | EIP-712 builder | Registration message and toSignatureMessage |
| settleMessage | function | EIP-712 builder | SettlePnl message and toSignatureMessage |
| dexRequestMessage | function | EIP-712 builder | DexRequest message and toSignatureMessage |

## Common Input Shape

All functions accept an object that extends the core input type with:

- **domain**: `SignatureDomain` (EIP-712 domain)
- **chainId**: `number`

Other fields come from `@paxeer-network/core` input types (e.g. `RegisterAccountInputs`, `WithdrawInputs`).

## Common Output

Each function returns `Promise<[message, toSignatureMessage]>` where:

- **message**: Plain object for the primary type (e.g. brokerId, chainId, timestamp, nonces).
- **toSignatureMessage**: Object with `domain`, `message`, `primaryType`, `types` for EIP-712 signing.

## withdrawMessage

- **Input**: `WithdrawInputs & { domain: SignatureDomain; chainId: number }`.
- **Output**: `[message, toSignatureMessage]` for primary type `"Withdraw"`. Message includes brokerId, chainId, receiver, token, amount, timestamp, withdrawNonce.

## internalTransferMessage

- **Input**: `InternalTransferInputs & { domain: SignatureDomain; chainId: number }`.
- **Output**: `[message, toSignatureMessage]` for primary type `"InternalTransfer"`. Message includes chainId, receiver, token, amount, transferNonce.

## addPaxeerNetworkKeyMessage

- **Input**: `AddPaxeerNetworkKeyInputs & { domain: SignatureDomain; chainId: number }`.
- **Output**: `[message, toSignatureMessage]` for primary type `"AddPaxeerNetworkKey"`. Message includes brokerId, paxeerNetworkKey (publicKey), scope, chainId, timestamp, expiration, optional tag and subAccountId.

## registerAccountMessage

- **Input**: `RegisterAccountInputs & { domain: SignatureDomain; chainId: number }`.
- **Output**: `[message, toSignatureMessage]` for primary type `"Registration"`. Message includes brokerId, chainId, timestamp, registrationNonce.

## settleMessage

- **Input**: `SettleInputs & { domain: SignatureDomain; chainId: number }`.
- **Output**: `[message, toSignatureMessage]` for primary type `"SettlePnl"`. Message includes brokerId, chainId, timestamp, settleNonce (from settlePnlNonce).

## dexRequestMessage

- **Input**: `DexRequestInputs & { domain: SignatureDomain; chainId: number }`.
- **Output**: `[message, toSignatureMessage]` for primary type `"DexRequest"`. Message includes payloadType, nonce, receiver, amount, vaultId, token, dexBrokerId.

## helper.ts Dependencies

- **Upstream**: `@paxeer-network/core` (input types, SignatureDomain), `@paxeer-network/types` (DEFAUL_PAXEER_NETWORK_KEY_SCOPE, definedTypes for EIP-712).
- **Downstream**: `DefaultEVMWalletAdapter` in `walletAdapter.ts` (calls these to build messages then signs via web3Provider).

## helper.ts Execution Flow

1. Destructure inputs (domain, chainId, and operation-specific fields).
2. Set primaryType and build typeDefinition from `definedTypes.EIP712Domain` and `definedTypes[primaryType]`.
3. Build message object for the primary type.
4. Build toSignatureMessage with domain, message, primaryType, types.
5. Return `[message, toSignatureMessage]` as const.

## helper.ts Example

```typescript
import {
  registerAccountMessage,
  addPaxeerNetworkKeyMessage,
} from "./helper";
import type { SignatureDomain } from "@paxeer-network/core";

const domain: SignatureDomain = {
  name: "paxeer-network",
  version: "1",
  chainId: 421614,
  verifyingContract: "0x...",
};

const [regMessage, regToSign] = await registerAccountMessage({
  chainId: 421614,
  domain,
  registrationNonce: 1,
  brokerId: "broker",
  timestamp: Date.now(),
});
// Use regToSign with signTypedData(address, JSON.stringify(regToSign)).
```
