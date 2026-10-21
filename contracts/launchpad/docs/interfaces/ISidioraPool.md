# Solidity API

## ISidioraPool

Interface for the core AMM pool contract (beacon proxy instances)

### DeadlineExpired

```solidity
error DeadlineExpired()
```

### SlippageExceeded

```solidity
error SlippageExceeded()
```

### InsufficientLiquidity

```solidity
error InsufficientLiquidity()
```

### InsufficientInput

```solidity
error InsufficientInput()
```

### ZeroAddress

```solidity
error ZeroAddress()
```

### ZeroAmount

```solidity
error ZeroAmount()
```

### PoolInfo

```solidity
struct PoolInfo {
  address tokenAddress;
  address opticalAddress;
  uint256 virtualUsdlReserve;
  uint256 realUsdlBalance;
  uint256 tokenReserve;
  uint256 creationTimestamp;
  uint256 cumulativeVolume;
}
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
function syncReserves() external returns (uint256 usdlBalance, uint256 tokenBalance)
```

Re-read actual token balances (used by LP_REWARDS strategy)

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| usdlBalance | uint256 | Current USDL balance |
| tokenBalance | uint256 | Current token balance |

### getReserves

```solidity
function getReserves() external view returns (uint256 virtualUsdl, uint256 realUsdl, uint256 tokenReserve)
```

Get current reserves

### getEffectiveReserves

```solidity
function getEffectiveReserves() external view returns (uint256 effectiveUsdl, uint256 tokenReserve)
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

### tokenAddress

```solidity
function tokenAddress() external view returns (address)
```

Get the pool's token address

### opticalAddress

```solidity
function opticalAddress() external view returns (address)
```

Get the pool's optical address

### creationTimestamp

```solidity
function creationTimestamp() external view returns (uint256)
```

Get the pool's creation timestamp

### cumulativeVolume

```solidity
function cumulativeVolume() external view returns (uint256)
```

Get cumulative volume

### getPriceSnapshots

```solidity
function getPriceSnapshots() external view returns (uint256[8])
```

Get price snapshots for volatility calculation

