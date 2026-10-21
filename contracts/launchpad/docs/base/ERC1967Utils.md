# Solidity API

## ERC1967Utils

Storage slot helpers for ERC-1967 proxy patterns

_Defines standard storage slots for implementation, admin, and beacon addresses_

### IMPLEMENTATION_SLOT

```solidity
bytes32 IMPLEMENTATION_SLOT
```

_bytes32(uint256(keccak256("eip1967.proxy.implementation")) - 1)_

### BEACON_SLOT

```solidity
bytes32 BEACON_SLOT
```

_bytes32(uint256(keccak256("eip1967.proxy.beacon")) - 1)_

### ADMIN_SLOT

```solidity
bytes32 ADMIN_SLOT
```

_bytes32(uint256(keccak256("eip1967.proxy.admin")) - 1)_

### InvalidImplementation

```solidity
error InvalidImplementation()
```

### InvalidBeacon

```solidity
error InvalidBeacon()
```

### Upgraded

```solidity
event Upgraded(address implementation)
```

### BeaconUpgraded

```solidity
event BeaconUpgraded(address beacon)
```

### AdminChanged

```solidity
event AdminChanged(address previousAdmin, address newAdmin)
```

### getImplementation

```solidity
function getImplementation() internal view returns (address impl)
```

Returns the current implementation address

### setImplementation

```solidity
function setImplementation(address newImplementation) internal
```

Sets the implementation address

### getBeacon

```solidity
function getBeacon() internal view returns (address beacon)
```

Returns the current beacon address

### setBeacon

```solidity
function setBeacon(address newBeacon) internal
```

Sets the beacon address

### getAdmin

```solidity
function getAdmin() internal view returns (address admin)
```

Returns the current admin address

### setAdmin

```solidity
function setAdmin(address newAdmin) internal
```

Sets the admin address

