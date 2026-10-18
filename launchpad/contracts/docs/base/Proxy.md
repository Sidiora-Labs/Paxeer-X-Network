# Solidity API

## Proxy

Base delegatecall proxy. Subclasses must implement _implementation().

_All calls are forwarded to the implementation via delegatecall._

### _implementation

```solidity
function _implementation() internal view virtual returns (address)
```

Returns the current implementation address

### _delegate

```solidity
function _delegate(address implementation) internal virtual
```

Delegates the current call to the implementation

### fallback

```solidity
fallback() external payable virtual
```

Fallback function delegates all calls to the implementation

### receive

```solidity
receive() external payable virtual
```

Receive function delegates to the implementation

