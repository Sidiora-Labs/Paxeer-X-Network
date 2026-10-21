# Solidity API

## ERC1967Proxy

Production ERC-1967 proxy used to wrap every Sidiora Meta-AG UUPS implementation

_Stores the implementation address in the ERC-1967 implementation slot and
     delegates every call through the in-house {Proxy} base. On construction,
     the proxy runs `initialize(...)` via delegatecall so that the proxy storage
     is live from its first transaction.

Plan ref: docs/plans/pecor-sidiora-merge-plan.md Phase 10.2 ("deploy impl, deploy
`ERC1967Proxy`, call `initialize(...)`") — the same factory is reused by every
Phase 2–7 test fixture so that unit tests exercise real proxy wiring rather than
raw implementations.

Zero external dependencies — composes only `contracts/base/` primitives that are
already shipped for the live Launchpad._

### InitializationFailed

```solidity
error InitializationFailed()
```

### constructor

```solidity
constructor(address implementation, bytes data) public payable
```

_On initializer failure we bubble up the implementation's revert
     data verbatim (standard ERC-1967 pattern) so callers observe the
     real custom error (e.g. {ZeroAddress}) instead of a generic
     {InitializationFailed}. The latter is retained only as the
     explicit fallback when the initializer reverted without any
     return data._

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| implementation | address | UUPS implementation contract |
| data | bytes | ABI-encoded call to `initialize(...)`. Pass `""` to skip. |

### _implementation

```solidity
function _implementation() internal view returns (address)
```

Returns the current implementation address

