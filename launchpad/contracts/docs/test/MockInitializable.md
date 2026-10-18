# Solidity API

## MockInitializable

### value

```solidity
uint256 value
```

### initialized

```solidity
bool initialized
```

### initialize

```solidity
function initialize(uint256 _value) external
```

### reinitialize2

```solidity
function reinitialize2(uint256 _value) external
```

### reinitialize3

```solidity
function reinitialize3(uint256 _value) external
```

### getInitializedVersion

```solidity
function getInitializedVersion() external view returns (uint64)
```

### isInitializing

```solidity
function isInitializing() external view returns (bool)
```

## MockInitializableDisabled

### value

```solidity
uint256 value
```

### constructor

```solidity
constructor() public
```

### initialize

```solidity
function initialize(uint256 _value) external
```

## MockInitializableChild

### childValue

```solidity
uint256 childValue
```

### parentValue

```solidity
uint256 parentValue
```

### initialize

```solidity
function initialize(uint256 _parent, uint256 _child) external
```

### _initParent

```solidity
function _initParent(uint256 _val) internal
```

