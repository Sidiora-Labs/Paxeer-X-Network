# Solidity API

## AntiSnipeOptical

Blocks buys exceeding maxBuyPercent of token supply in the first N blocks after pool creation.

_Uses beforeSwap hook only. Immutable once deployed.
     After the protection period, all buys are allowed regardless of size.
     Sells are never affected by this optical._

### ProtectionActive

```solidity
error ProtectionActive()
```

### maxBuyBps

```solidity
uint256 maxBuyBps
```

Maximum buy percentage in basis points (e.g., 100 = 1%)

### protectionBlocks

```solidity
uint256 protectionBlocks
```

Number of blocks after pool creation during which protection is active

### poolCreationBlock

```solidity
mapping(address => uint256) poolCreationBlock
```

Mapping of pool address to the block number at which it was created

### constructor

```solidity
constructor(address _poolRegistry, address _owner, uint256 _maxBuyBps, uint256 _protectionBlocks) public
```

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| _poolRegistry | address | PoolRegistry address for pool validation |
| _owner | address | Deployer/owner address |
| _maxBuyBps | uint256 | Maximum buy size in bps of token supply (e.g., 100 = 1%) |
| _protectionBlocks | uint256 | Number of blocks the protection lasts |

### registerPool

```solidity
function registerPool(address pool) external
```

Register a pool's creation block (called once when pool is created)

_Anyone can call this, but it only records the first time for each pool_

### getFlags

```solidity
function getFlags() external pure returns (uint8)
```

### beforeSwap

```solidity
function beforeSwap(address pool, address, bool isBuy, uint256 amountIn) external returns (bool proceed, int256 amountDelta)
```

