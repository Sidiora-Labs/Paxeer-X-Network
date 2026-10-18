# Solidity API

## PoolRegistry

On-chain pool discovery and metadata storage

_UUPS proxy. Only FACTORY_ROLE can register pools._

### FACTORY_ROLE

```solidity
bytes32 FACTORY_ROLE
```

### eventEmitter

```solidity
address eventEmitter
```

### constructor

```solidity
constructor() public
```

### initialize

```solidity
function initialize(address _eventEmitter, address _admin) external
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

### _authorizeUpgrade

```solidity
function _authorizeUpgrade(address) internal
```

