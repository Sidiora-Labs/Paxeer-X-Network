# Solidity API

## BitFlagWrapper

Wrapper to expose BitFlag library functions for testing

### BEFORE_SWAP

```solidity
function BEFORE_SWAP() external pure returns (uint8)
```

### AFTER_SWAP

```solidity
function AFTER_SWAP() external pure returns (uint8)
```

### BEFORE_FEE_DISTRIBUTION

```solidity
function BEFORE_FEE_DISTRIBUTION() external pure returns (uint8)
```

### AFTER_FEE_DISTRIBUTION

```solidity
function AFTER_FEE_DISTRIBUTION() external pure returns (uint8)
```

### hasFlag

```solidity
function hasFlag(uint8 flags, uint8 flag) external pure returns (bool)
```

### setFlag

```solidity
function setFlag(uint8 flags, uint8 flag) external pure returns (uint8)
```

### clearFlag

```solidity
function clearFlag(uint8 flags, uint8 flag) external pure returns (uint8)
```

