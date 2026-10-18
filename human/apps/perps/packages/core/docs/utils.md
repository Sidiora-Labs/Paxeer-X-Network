# utils.ts

## utils.ts Responsibility

Provides hashing, encoding, and time utilities for paxeer-network: parseBrokerHash, parseAccountId, parseTokenHash (keccak256/solidityPackedKeccak256), base64url, getTimestamp (with optional __PAXEER_NETWORK_timestamp_offset), formatByUnits, isHex, isHexString, getGlobalObject. Re-exports parseUnits from ethers.

## utils.ts Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| SignatureDomain | type | EIP-712 | { name, version, chainId, verifyingContract } |
| base64url | function | Encoding | Replaces +/ with -/_ in base64 string |
| parseBrokerHash | function | Hash | calculateStringHash(brokerId) for broker id |
| parseAccountId | function | Hash | keccak256(encode(["address","bytes32"], [userAddress, parseBrokerHash(brokerId)])) |
| parseTokenHash | function | Hash | calculateStringHash(tokenSymbol) |
| calculateStringHash | function | Hash | solidityPackedKeccak256(["string"], [input]) |
| formatByUnits | function | Format | formatUnits(amount, unit) |
| isHex | function | Check | true if string is hex chars |
| isHexString | function | Check | string starts with "0x" and isHex |
| getGlobalObject | function | Env | globalThis \|\| self \|\| window \|\| global |
| getTimestamp | function | Time | Date.now() or Date.now() + __PAXEER_NETWORK_timestamp_offset |
| parseUnits | re-export | ethers | From ethers |

## SignatureDomain Fields

| Field | Type | Description |
|-------|------|-------------|
| name | string | EIP-712 domain name (e.g. "paxeer-network") |
| version | string | Domain version |
| chainId | number | Chain ID |
| verifyingContract | string | Contract address for verification |

## getTimestamp Behavior

If running in browser and getGlobalObject() has numeric `__PAXEER_NETWORK_timestamp_offset`, returns Date.now() + that offset; otherwise returns Date.now(). Used to align client time with server for signing.

## utils.ts Dependencies and Call Relationships

- **Upstream**: ethers (keccak256, AbiCoder, solidityPackedKeccak256, parseUnits, formatUnits).
- **Downstream**: Account (parseAccountId for accountIdHashStr; getTimestamp), Assets (parseBrokerHash, parseTokenHash, getTimestamp), helper (getTimestamp, SignatureDomain), signer (base64url, getTimestamp).

## utils.ts Example

```typescript
import {
  parseAccountId,
  parseBrokerHash,
  getTimestamp,
  base64url,
  SignatureDomain,
  parseUnits,
} from "@paxeer-network/core";

const accountIdHash = parseAccountId("0x...", "paxeer-network");
const brokerHash = parseBrokerHash("paxeer-network");
const ts = getTimestamp();
const domain: SignatureDomain = { name: "paxeer-network", version: "1", chainId: 421614, verifyingContract: "0x..." };
```
