# Solidity API

## IPoolRegistry

Interface for on-chain pool discovery and metadata storage

### Unauthorized

```solidity
error Unauthorized()
```

### ZeroAddress

```solidity
error ZeroAddress()
```

### DuplicateToken

```solidity
error DuplicateToken()
```

### PoolNotFound

```solidity
error PoolNotFound()
```

### PoolMetadata

```solidity
struct PoolMetadata {
  address creator;
  address token;
  address optical;
  uint256 nftId;
  uint256 createdAt;
  uint256 createdBlock;
}
```

### PoolRegistered

```solidity
event PoolRegistered(address pool, address token, address creator, address optical, uint256 nftId, uint256 timestamp)
```

### register

```solidity
function register(address pool, address token, address creator, address optical, uint256 nftId) external
```

Register a new pool (factory-only)

### getPoolByToken

```solidity
function getPoolByToken(address token) external view returns (address)
```

Get pool address by token address

### getPoolsByCreator

```solidity
function getPoolsByCreator(address creator) external view returns (address[])
```

Get all pools created by a specific address

### getNftIdByPool

```solidity
function getNftIdByPool(address pool) external view returns (uint256)
```

Get the NFT token ID associated with a pool

### getPoolMetadata

```solidity
function getPoolMetadata(address pool) external view returns (struct IPoolRegistry.PoolMetadata)
```

Get full metadata for a pool

### getAllPools

```solidity
function getAllPools(uint256 offset, uint256 limit) external view returns (address[])
```

Get paginated list of all pools

### getPoolCount

```solidity
function getPoolCount() external view returns (uint256)
```

Get total number of registered pools

### isRegisteredPool

```solidity
function isRegisteredPool(address pool) external view returns (bool)
```

Check if a pool is registered

