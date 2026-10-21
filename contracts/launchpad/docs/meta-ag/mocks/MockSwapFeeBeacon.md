# Solidity API

## MockSwapFeeBeacon

Minimal stand-in for the PECOR engine surface used by MetaAGQuoter.
        The quoter only ever reads `swapFeeBps()` via low-level staticcall,
        so this mock exposes that single function with a configurable value
        plus an optional revert switch to exercise the fail-soft path in
        `_getFeeBps`.

_Scope-isolated under `contracts/meta-ag/mocks/`. Do NOT use in
     production — this is for unit tests only._

### ForcedRevert

```solidity
error ForcedRevert()
```

### constructor

```solidity
constructor(uint256 initialFeeBps_) public
```

### setFeeBps

```solidity
function setFeeBps(uint256 newFeeBps) external
```

### setRevert

```solidity
function setRevert(bool flag) external
```

### swapFeeBps

```solidity
function swapFeeBps() external view returns (uint256)
```

