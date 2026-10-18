# Solidity API

## MockERC1967Proxy

### constructor

```solidity
constructor(address implementation, bytes data) public
```

### _implementation

```solidity
function _implementation() internal view returns (address)
```

Returns the current implementation address

## MockImplementation

### value

```solidity
uint256 value
```

### setValue

```solidity
function setValue(uint256 _value) external
```

### getValue

```solidity
function getValue() external view returns (uint256)
```

## MockImplementationV2

### value

```solidity
uint256 value
```

### extra

```solidity
uint256 extra
```

### setValue

```solidity
function setValue(uint256 _value) external
```

### getValue

```solidity
function getValue() external view returns (uint256)
```

### setExtra

```solidity
function setExtra(uint256 _extra) external
```

### version

```solidity
function version() external pure returns (uint256)
```

## ERC1967UtilsWrapper

### getImplementation

```solidity
function getImplementation() external view returns (address)
```

### setImplementation

```solidity
function setImplementation(address impl) external
```

### getBeacon

```solidity
function getBeacon() external view returns (address)
```

### setBeacon

```solidity
function setBeacon(address beacon) external
```

### getAdmin

```solidity
function getAdmin() external view returns (address)
```

### setAdmin

```solidity
function setAdmin(address admin) external
```

