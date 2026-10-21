# Solidity API

## MockUUPS

### value

```solidity
uint256 value
```

### upgrader

```solidity
address upgrader
```

### initialize

```solidity
function initialize(address _upgrader) external
```

### setValue

```solidity
function setValue(uint256 _val) external
```

### version

```solidity
function version() external pure returns (uint256)
```

### _authorizeUpgrade

```solidity
function _authorizeUpgrade(address) internal view
```

## MockUUPSV2

### value

```solidity
uint256 value
```

### upgrader

```solidity
address upgrader
```

### extra

```solidity
uint256 extra
```

### setValue

```solidity
function setValue(uint256 _val) external
```

### setExtra

```solidity
function setExtra(uint256 _val) external
```

### version

```solidity
function version() external pure returns (uint256)
```

### _authorizeUpgrade

```solidity
function _authorizeUpgrade(address) internal view
```

## UUPSProxy

_Simple ERC1967 proxy for UUPS testing_

### constructor

```solidity
constructor(address implementation, bytes data) public
```

### _implementation

```solidity
function _implementation() internal view returns (address)
```

Returns the current implementation address

