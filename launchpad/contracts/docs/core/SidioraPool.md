# Solidity API

## IOpticalMinimal

_Minimal interface for optical hooks used by pool_

### beforeSwap

```solidity
function beforeSwap(address pool, address sender, bool isBuy, uint256 amountIn) external returns (bool proceed, int256 amountDelta)
```

### afterSwap

```solidity
function afterSwap(address pool, address sender, bool isBuy, uint256 amountIn, uint256 amountOut) external returns (bytes4)
```

### getFlags

```solidity
function getFlags() external view returns (uint8)
```

## SidioraPool

Core AMM engine. Constant product with virtual reserves and dynamic fees.

_Beacon proxy instances. Deliberately focused on AMM math + state transitions only.

     FEE MODEL: Fees are always paid in the INPUT token.
       BUY  (USDL → Token): fee in USDL → sent to FeeAccumulator for strategy distribution
       SELL (Token → USDL): fee in Token → stays in pool, deepens token-side liquidity

     VIRTUAL RESERVE FLOOR: virtualUsdlReserve (10,000 USDL) is pricing-only.
       realUsdlBalance can never go below 0. The pool cannot pay out virtual USDL.
       On sells, amountOut is bounded by realUsdlBalance (defense-in-depth)._

### VirtualFloorBreached

```solidity
error VirtualFloorBreached()
```

### tokenAddress

```solidity
address tokenAddress
```

Get the pool's token address

### usdlAddress

```solidity
address usdlAddress
```

### opticalAddress

```solidity
address opticalAddress
```

Get the pool's optical address

### feeAccumulator

```solidity
address feeAccumulator
```

### eventEmitter

```solidity
address eventEmitter
```

### protocolConfig

```solidity
address protocolConfig
```

### guardian

```solidity
address guardian
```

### virtualUsdlReserve

```solidity
uint256 virtualUsdlReserve
```

### realUsdlBalance

```solidity
uint256 realUsdlBalance
```

### tokenReserve

```solidity
uint256 tokenReserve
```

### creationTimestamp

```solidity
uint256 creationTimestamp
```

Get the pool's creation timestamp

### cumulativeVolume

```solidity
uint256 cumulativeVolume
```

Get cumulative volume

### priceSnapshots

```solidity
uint256[8] priceSnapshots
```

### snapshotIndex

```solidity
uint256 snapshotIndex
```

### snapshotCount

```solidity
uint256 snapshotCount
```

### accumulatedUsdlFees

```solidity
uint256 accumulatedUsdlFees
```

### accumulatedTokenFees

```solidity
uint256 accumulatedTokenFees
```

### constructor

```solidity
constructor() public
```

### initialize

```solidity
function initialize(address _tokenAddress, address _usdlAddress, address _opticalAddress, address _feeAccumulator, address _eventEmitter, address _protocolConfig, address _guardian, uint256 _virtualUsdlReserve, uint256 _tokenReserve) external
```

Initialize pool state (called once by factory via beacon proxy)

### swap

```solidity
function swap(uint256 amountIn, uint256 minAmountOut, bool isBuy, address recipient, uint256 deadline) external returns (uint256 amountOut)
```

Execute a swap

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| amountIn | uint256 | Amount of input token |
| minAmountOut | uint256 | Minimum output (slippage protection) |
| isBuy | bool | True = USDL→Token, False = Token→USDL |
| recipient | address | Address to receive output tokens |
| deadline | uint256 | Transaction deadline timestamp |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| amountOut | uint256 | Actual output amount |

### syncReserves

```solidity
function syncReserves() external returns (uint256 usdlBal, uint256 tokenBal)
```

Re-read actual token balances (used by LP_REWARDS strategy)

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| usdlBal | uint256 |  |
| tokenBal | uint256 |  |

### pause

```solidity
function pause() external
```

### unpause

```solidity
function unpause() external
```

### paused

```solidity
function paused() external view returns (bool)
```

### getReserves

```solidity
function getReserves() external view returns (uint256 virtualUsdl, uint256 realUsdl, uint256 tokenRes)
```

Get current reserves

### getEffectiveReserves

```solidity
function getEffectiveReserves() external view returns (uint256 effectiveUsdl, uint256 tokenRes)
```

Get effective reserves (virtual + real USDL, token)

### getPrice

```solidity
function getPrice() external view returns (uint256)
```

Get current token price in USDL (Q128 fixed-point)

### getPoolInfo

```solidity
function getPoolInfo() external view returns (struct ISidioraPool.PoolInfo)
```

Get full pool info struct

### getPriceSnapshots

```solidity
function getPriceSnapshots() external view returns (uint256[8])
```

Get price snapshots for volatility calculation

### _calculateFee

```solidity
function _calculateFee() internal view returns (uint256)
```

### _currentPrice

```solidity
function _currentPrice() internal view returns (uint256)
```

### _updatePriceSnapshot

```solidity
function _updatePriceSnapshot() internal
```

### _getBalance

```solidity
function _getBalance(address token) internal view returns (uint256)
```

