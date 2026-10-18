# Solidity API

## ReentrancyGuard

Prevents reentrant calls to protected functions

_Uses a storage slot flag to detect and block reentrancy_

### ReentrancyGuardReentrantCall

```solidity
error ReentrancyGuardReentrantCall()
```

### constructor

```solidity
constructor() internal
```

### nonReentrant

```solidity
modifier nonReentrant()
```

### _initReentrancyGuard

```solidity
function _initReentrancyGuard() internal
```

_Initialize reentrancy guard for proxied contracts (no constructor)_

