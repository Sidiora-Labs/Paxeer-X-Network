# Solidity API

## MockTxTracker

Deliberately-minimal stub for {ITransactionTracker} used by
        {PECORVault} tests. Phase 3 exercises the vault's tracker wiring
        (setTransactionTracker + event + zero-address accepted at
        bootstrap); the full tracker surface lands in Phase 7 (Task 7.2).

_The vault never reads back from the tracker during Phase 3, so this
     mock exposes no-op recorders. It simply captures the last call on
     each hook to keep future-phase compatibility trivial._

### lastCaller

```solidity
address lastCaller
```

### lastPayload

```solidity
bytes lastPayload
```

### callCount

```solidity
uint256 callCount
```

### Recorded

```solidity
event Recorded(address caller, bytes payload)
```

### record

```solidity
function record(bytes payload) external
```

_Generic catch-all recorder used by later phases; kept optional._

