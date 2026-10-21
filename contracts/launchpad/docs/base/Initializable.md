# Solidity API

## Initializable

Initialization guard for proxied contracts. Replaces constructors.

_Supports versioned reinitializers and disableInitializers for implementations._

### InitializableStorage

```solidity
struct InitializableStorage {
  uint64 initialized;
  bool initializing;
}
```

### AlreadyInitialized

```solidity
error AlreadyInitialized()
```

### NotInitializing

```solidity
error NotInitializing()
```

### Initialized

```solidity
event Initialized(uint64 version)
```

### initializer

```solidity
modifier initializer()
```

### reinitializer

```solidity
modifier reinitializer(uint64 version)
```

### onlyInitializing

```solidity
modifier onlyInitializing()
```

### _disableInitializers

```solidity
function _disableInitializers() internal virtual
```

Locks the contract, preventing any future initialization

_Call in the constructor of implementation contracts_

### _getInitializedVersion

```solidity
function _getInitializedVersion() internal view returns (uint64)
```

### _isInitializing

```solidity
function _isInitializing() internal view returns (bool)
```

