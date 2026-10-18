# Solidity API

## TokenAddressWrapper

Wrapper to expose TokenAddress library functions for testing

### computeAddress

```solidity
function computeAddress(address factory, address creator, string name, string symbol, uint256 nonce, bytes creationCode, uint256 totalSupply, address recipient) external pure returns (address)
```

### computeSalt

```solidity
function computeSalt(address creator, string name, string symbol, uint256 nonce) external pure returns (bytes32)
```

