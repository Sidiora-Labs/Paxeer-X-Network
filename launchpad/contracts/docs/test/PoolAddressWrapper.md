# Solidity API

## PoolAddressWrapper

Wrapper to expose PoolAddress library functions for testing

### computeAddress

```solidity
function computeAddress(address factory, address beacon, address token, bytes creationCode) external pure returns (address)
```

### computeSalt

```solidity
function computeSalt(address token) external pure returns (bytes32)
```

