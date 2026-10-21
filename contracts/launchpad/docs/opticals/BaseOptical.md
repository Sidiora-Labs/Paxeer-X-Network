# Solidity API

## BaseOptical

Abstract base contract for building custom opticals.

_Provides default no-op implementations for all 4 hooks.
     Inherit this to build custom opticals — override only the hooks you need.
     Each optical is IMMUTABLE once deployed (audited once, deployed once)._

### NotPool

```solidity
error NotPool()
```

### NotOwner

```solidity
error NotOwner()
```

### poolRegistry

```solidity
address poolRegistry
```

The PoolRegistry address for validating pool callers

### owner

```solidity
address owner
```

The deployer/owner of this optical (for configuration)

### constructor

```solidity
constructor(address _poolRegistry, address _owner) internal
```

### onlyPool

```solidity
modifier onlyPool()
```

_Modifier to restrict calls to registered pools only.
     Validates caller is a pool registered in PoolRegistry._

### onlyOwner

```solidity
modifier onlyOwner()
```

_Modifier to restrict calls to the optical owner._

### beforeSwap

```solidity
function beforeSwap(address, address, bool, uint256) external virtual returns (bool proceed, int256 amountDelta)
```

Called before a swap executes in the pool.

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
|  | address |  |
|  | address |  |
|  | bool |  |
|  | uint256 |  |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| proceed | bool | Whether the swap should continue (false = reject) |
| amountDelta | int256 | Adjustment to amountIn (positive = increase, negative = decrease) |

### afterSwap

```solidity
function afterSwap(address, address, bool, uint256, uint256) external virtual returns (bytes4)
```

Called after a swap completes in the pool.

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
|  | address |  |
|  | address |  |
|  | bool |  |
|  | uint256 |  |
|  | uint256 |  |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| [0] | bytes4 | selector The function selector to confirm execution (bytes4) |

### beforeFeeDistribution

```solidity
function beforeFeeDistribution(address, uint256 feeAmount) external virtual returns (uint256 adjustedFee)
```

Called before fee distribution occurs.

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
|  | address |  |
| feeAmount | uint256 | The fee amount about to be distributed |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| adjustedFee | uint256 | The adjusted fee amount after optical processing |

### afterFeeDistribution

```solidity
function afterFeeDistribution(address, uint256) external virtual returns (bytes4)
```

Called after fee distribution occurs.

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
|  | address |  |
|  | uint256 |  |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| [0] | bytes4 | selector The function selector to confirm execution (bytes4) |

### getFlags

```solidity
function getFlags() external view virtual returns (uint8)
```

Returns a bitmap of active hooks for this optical.

_Bit 0: beforeSwap, Bit 1: afterSwap,
     Bit 2: beforeFeeDistribution, Bit 3: afterFeeDistribution
     Pool checks this to skip unused callbacks (gas optimization)._

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| [0] | uint8 |  |

### _isRegisteredPool

```solidity
function _isRegisteredPool(address pool) internal view returns (bool)
```

_Checks if an address is a registered pool by calling PoolRegistry._

### _getPoolReserves

```solidity
function _getPoolReserves(address pool) internal view returns (uint256 virtualUsdl, uint256 realUsdl, uint256 tokenRes)
```

_Helper to read pool reserves for opticals that need pricing data._

