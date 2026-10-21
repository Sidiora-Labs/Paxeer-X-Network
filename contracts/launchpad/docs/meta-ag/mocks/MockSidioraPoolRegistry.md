# Solidity API

## MockSidioraPoolRegistry

Minimal Sidiora pool registry used by SidioraFeedAdapter tests.

_Matches the live `IPoolRegistry.getPoolByToken(address)` selector only; all other
     registry methods are out of scope for the Phase 2 oracle tests._

### revertOnLookup

```solidity
bool revertOnLookup
```

### setPoolByToken

```solidity
function setPoolByToken(address token, address pool) external
```

### setRevertOnLookup

```solidity
function setRevertOnLookup(bool v) external
```

### getPoolByToken

```solidity
function getPoolByToken(address token) external view returns (address)
```

