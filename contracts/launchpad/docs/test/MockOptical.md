# Solidity API

## MockOptical

Test mock that inherits BaseOptical with configurable behavior

### constructor

```solidity
constructor(address _poolRegistry, address _owner) public
```

## MockOpticalWithFlags

Test mock with configurable flags and hook behavior

### beforeSwapRejectNext

```solidity
bool beforeSwapRejectNext
```

### beforeSwapDeltaNext

```solidity
int256 beforeSwapDeltaNext
```

### beforeSwapCalled

```solidity
bool beforeSwapCalled
```

### afterSwapCalled

```solidity
bool afterSwapCalled
```

### beforeFeeDistCalled

```solidity
bool beforeFeeDistCalled
```

### afterFeeDistCalled

```solidity
bool afterFeeDistCalled
```

### lastPool

```solidity
address lastPool
```

### lastSender

```solidity
address lastSender
```

### lastIsBuy

```solidity
bool lastIsBuy
```

### lastAmountIn

```solidity
uint256 lastAmountIn
```

### lastAmountOut

```solidity
uint256 lastAmountOut
```

### lastFeeAmount

```solidity
uint256 lastFeeAmount
```

### constructor

```solidity
constructor(address _poolRegistry, address _owner, uint8 flags_) public
```

### setFlags

```solidity
function setFlags(uint8 flags_) external
```

### setBeforeSwapReject

```solidity
function setBeforeSwapReject(bool reject) external
```

### setBeforeSwapDelta

```solidity
function setBeforeSwapDelta(int256 delta) external
```

### resetCallTrackers

```solidity
function resetCallTrackers() external
```

### getFlags

```solidity
function getFlags() external view returns (uint8)
```

Returns a bitmap of active hooks for this optical.

_Bit 0: beforeSwap, Bit 1: afterSwap,
     Bit 2: beforeFeeDistribution, Bit 3: afterFeeDistribution
     Pool checks this to skip unused callbacks (gas optimization)._

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| [0] | uint8 |  |

### beforeSwap

```solidity
function beforeSwap(address pool, address sender, bool isBuy, uint256 amountIn) external returns (bool proceed, int256 amountDelta)
```

### afterSwap

```solidity
function afterSwap(address pool, address sender, bool isBuy, uint256 amountIn, uint256 amountOut) external returns (bytes4)
```

### beforeFeeDistribution

```solidity
function beforeFeeDistribution(address pool, uint256 feeAmount) external returns (uint256 adjustedFee)
```

### afterFeeDistribution

```solidity
function afterFeeDistribution(address pool, uint256 feeAmount) external returns (bytes4)
```

## MockRevertingOptical

An optical that reverts on any hook call

### constructor

```solidity
constructor(address _poolRegistry, address _owner, uint8 flags_) public
```

### getFlags

```solidity
function getFlags() external view returns (uint8)
```

Returns a bitmap of active hooks for this optical.

_Bit 0: beforeSwap, Bit 1: afterSwap,
     Bit 2: beforeFeeDistribution, Bit 3: afterFeeDistribution
     Pool checks this to skip unused callbacks (gas optimization)._

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| [0] | uint8 |  |

### beforeSwap

```solidity
function beforeSwap(address, address, bool, uint256) external pure returns (bool, int256)
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
| [0] | bool |  |
| [1] | int256 |  |

### afterSwap

```solidity
function afterSwap(address, address, bool, uint256, uint256) external pure returns (bytes4)
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

