# Solidity API

## UpgradeableBeacon

Stores and upgrades the implementation address for beacon proxies

_Owner can upgrade. All BeaconProxy instances read implementation from here._

### InvalidImplementation

```solidity
error InvalidImplementation()
```

### Unauthorized

```solidity
error Unauthorized()
```

### ZeroAddress

```solidity
error ZeroAddress()
```

### Upgraded

```solidity
event Upgraded(address implementation)
```

### OwnershipTransferred

```solidity
event OwnershipTransferred(address previousOwner, address newOwner)
```

### constructor

```solidity
constructor(address initialImplementation, address initialOwner) public
```

### onlyOwner

```solidity
modifier onlyOwner()
```

### implementation

```solidity
function implementation() public view returns (address)
```

Returns the current implementation address

### owner

```solidity
function owner() public view returns (address)
```

Returns the current owner

### upgradeTo

```solidity
function upgradeTo(address newImplementation) external
```

Upgrades the implementation to a new address

### transferOwnership

```solidity
function transferOwnership(address newOwner) external
```

Transfers ownership to a new address

