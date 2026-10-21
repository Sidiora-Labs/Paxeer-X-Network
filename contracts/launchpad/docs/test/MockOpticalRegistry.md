# Solidity API

## MockOpticalRegistry

Minimal stand-in for IOpticalRegistry used by EventEmitter v2
        auth-mesh tests. Exposes the `isApproved(address)` selector the
        EventEmitter expects without spinning up the full registry.

### setApproved

```solidity
function setApproved(address optical, bool approved) external
```

### isApproved

```solidity
function isApproved(address optical) external view returns (bool)
```

