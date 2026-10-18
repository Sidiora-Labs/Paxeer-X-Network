# keyStore.ts

## keyStore.ts Responsibility

Defines the paxeer-network key storage abstraction (`PaxeerNetworkKeyStore`) and implementations: `BaseKeyStore` (abstract), `LocalStorageStore` (browser localStorage keyed by networkId and address), and `MockKeyStore` (in-memory with fixed secret key for tests).

## keyStore.ts Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| PaxeerNetworkKeyStore | interface | Contract | getPaxeerNetworkKey, getAccountId, setAccountId, getAddress, setAddress, removeAddress, generateKey, cleanKey, cleanAllKey, setKey |
| BaseKeyStore | abstract class | Base | Implements PaxeerNetworkKeyStore with keyPrefix `paxeer-network_${networkId}_` |
| LocalStorageStore | class | Impl | Persists paxeerNetworkKey, accountId, address in localStorage |
| MockKeyStore | class | Impl | Returns same BasePaxeerNetworkKeyPair from fixed secretKey; no persistence |

## PaxeerNetworkKeyStore Responsibility

Stores and retrieves the current wallet address, account ID per address, and paxeer-network Key (as PaxeerNetworkKeyPair) per address. Used by Signer and Account.

## PaxeerNetworkKeyStore Methods

| Method | Description |
|--------|-------------|
| getPaxeerNetworkKey(address?) | Returns PaxeerNetworkKeyPair for address or default address; null if none |
| getAccountId(address) | Returns account ID for address |
| setAccountId(address, accountId) | Saves account ID for address |
| getAddress() | Returns current/default address |
| setAddress(address) | Sets current address |
| removeAddress() | Clears current address |
| generateKey() | Creates new PaxeerNetworkKeyPair |
| setKey(paxeerNetworkKey, secretKey) | Stores PaxeerNetworkKeyPair for address (second param is PaxeerNetworkKeyPair) |
| cleanKey(address, key) | Removes one key (e.g. "paxeerNetworkKey") for address |
| cleanAllKey(address) | Removes all data for address |

## LocalStorageStore Storage Shape

- Key prefix: `paxeer-network_${networkId}_`.
- Address key: `${keyPrefix}address` (current address).
- Per-address data: `${keyPrefix}${address}` as JSON with keys: paxeerNetworkKey (secretKey string), accountId.

## keyStore.ts Dependencies and Call Relationships

- **Upstream**: keyPair (BasePaxeerNetworkKeyPair, PaxeerNetworkKeyPair).
- **Downstream**: Account, BaseSigner; getDefaultSigner/getMockSigner in helper.

## keyStore.ts Extension and Modification Points

- **New backend**: Implement PaxeerNetworkKeyStore (e.g. secure storage, server-backed) and pass to Account/Signer.
- **Key schema**: Change keyPrefix or per-address JSON shape in LocalStorageStore; keep PaxeerNetworkKeyStore interface stable.

## keyStore.ts Example

```typescript
import { LocalStorageStore, MockKeyStore, BasePaxeerNetworkKeyPair } from "@paxeer-network/core";

const keyStore = new LocalStorageStore("testnet");
keyStore.setAddress("0x123...");
keyStore.setAccountId("0x123...", "account_id_1");
const kp = keyStore.generateKey();
keyStore.setKey("0x123...", kp);
const key = keyStore.getPaxeerNetworkKey();
const accountId = keyStore.getAccountId("0x123...");

// Test
const mockStore = new MockKeyStore("base58SecretKey...");
const signer = new BaseSigner(mockStore);
```
