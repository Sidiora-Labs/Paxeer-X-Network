# Solidity API

## UUPSUpgradeable

UUPS (Universal Upgradeable Proxy Standard) upgrade logic

_Implementation contracts inherit this. Proxy delegates to implementation.
     Authorization for upgrades is defined by subclasses via _authorizeUpgrade._

### UnauthorizedUpgrade

```solidity
error UnauthorizedUpgrade()
```

### UUPSNotThroughProxy

```solidity
error UUPSNotThroughProxy()
```

### UUPSNotThroughActiveProxy

```solidity
error UUPSNotThroughActiveProxy()
```

### InvalidImplementation

```solidity
error InvalidImplementation()
```

### onlyProxy

```solidity
modifier onlyProxy()
```

Checks that the function is called through a proxy

### notDelegated

```solidity
modifier notDelegated()
```

Checks that the function is NOT called through a proxy

### proxiableUUID

```solidity
function proxiableUUID() external view returns (bytes32)
```

Returns the ERC-1822 proxiable UUID

_Must return the ERC-1967 implementation slot to be compatible_

### upgradeToAndCall

```solidity
function upgradeToAndCall(address newImplementation, bytes data) external payable virtual
```

Upgrades the implementation to a new address

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| newImplementation | address | The address of the new implementation |
| data | bytes | Optional calldata for initialization after upgrade |

### _authorizeUpgrade

```solidity
function _authorizeUpgrade(address newImplementation) internal virtual
```

Override this to define who can authorize upgrades

### _upgradeToAndCallUUPS

```solidity
function _upgradeToAndCallUUPS(address newImplementation, bytes data) internal
```

_Performs the upgrade, verifying the new implementation is UUPS-compatible_

