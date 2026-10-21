# Solidity API

## IRouter

Interface for the user-facing entry point for all protocol interactions

### ZeroAddress

```solidity
error ZeroAddress()
```

### ZeroAmount

```solidity
error ZeroAmount()
```

### DeadlineExpired

```solidity
error DeadlineExpired()
```

### PoolNotFound

```solidity
error PoolNotFound()
```

### InsufficientBalance

```solidity
error InsufficientBalance()
```

### SameToken

```solidity
error SameToken()
```

### PermitParams

EIP-2612 permit parameters for gasless approvals

```solidity
struct PermitParams {
  uint256 value;
  uint256 deadline;
  uint8 v;
  bytes32 r;
  bytes32 s;
}
```

### MarketCreated

```solidity
event MarketCreated(address token, address pool, address creator, uint256 nftId)
```

### Buy

```solidity
event Buy(address pool, address buyer, uint256 usdlIn, uint256 tokensOut)
```

### Sell

```solidity
event Sell(address pool, address seller, uint256 tokensIn, uint256 usdlOut)
```

### MultihopSwap

```solidity
event MultihopSwap(address sender, address tokenIn, address tokenOut, uint256 amountIn, uint256 intermediateUsdl, uint256 amountOut)
```

### createMarket

```solidity
function createMarket(string name, string symbol, uint8 feeStrategy, address optical) external returns (address tokenAddr, address poolAddr, uint256 nftId)
```

Create a new market (token + pool + NFT)

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| name | string | Token name |
| symbol | string | Token symbol |
| feeStrategy | uint8 | Initial fee strategy (0=CLAIM,1=BURN,2=AIRDROP,3=LP_REWARDS) |
| optical | address | Optional optical hook contract (address(0) for none) |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| tokenAddr | address | The deployed token address |
| poolAddr | address | The deployed pool address |
| nftId | uint256 | The minted NFT token ID |

### buy

```solidity
function buy(address pool, uint256 usdlAmountIn, uint256 minTokensOut, uint256 deadline) external returns (uint256 amountOut)
```

Buy tokens with USDL

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| pool | address | The pool address to buy from |
| usdlAmountIn | uint256 | Amount of USDL to spend |
| minTokensOut | uint256 | Minimum tokens to receive (slippage protection) |
| deadline | uint256 | Transaction deadline timestamp |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| amountOut | uint256 | Actual tokens received |

### sell

```solidity
function sell(address pool, uint256 tokenAmountIn, uint256 minUsdlOut, uint256 deadline) external returns (uint256 amountOut)
```

Sell tokens for USDL

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| pool | address | The pool address to sell to |
| tokenAmountIn | uint256 | Amount of tokens to sell |
| minUsdlOut | uint256 | Minimum USDL to receive (slippage protection) |
| deadline | uint256 | Transaction deadline timestamp |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| amountOut | uint256 | Actual USDL received |

### swapTokenForToken

```solidity
function swapTokenForToken(address tokenIn, address tokenOut, uint256 amountIn, uint256 minAmountOut, uint256 deadline) external returns (uint256 amountOut, uint256 intermediateUsdl)
```

Swap Token A → USDL → Token B in a single transaction

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| tokenIn | address | Address of the token to sell |
| tokenOut | address | Address of the token to buy |
| amountIn | uint256 | Amount of tokenIn to sell |
| minAmountOut | uint256 | Minimum tokenOut to receive (end-to-end slippage protection) |
| deadline | uint256 | Transaction deadline timestamp |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| amountOut | uint256 | Actual tokenOut received |
| intermediateUsdl | uint256 | USDL amount received from the sell leg |

### buyWithPermit

```solidity
function buyWithPermit(address pool, uint256 usdlAmountIn, uint256 minTokensOut, uint256 deadline, struct IRouter.PermitParams permit) external returns (uint256 amountOut)
```

Buy tokens with USDL using EIP-2612 permit (no separate approve tx)

### sellWithPermit

```solidity
function sellWithPermit(address pool, uint256 tokenAmountIn, uint256 minUsdlOut, uint256 deadline, struct IRouter.PermitParams permit) external returns (uint256 amountOut)
```

Sell tokens for USDL using EIP-2612 permit (no separate approve tx)

### swapTokenForTokenWithPermit

```solidity
function swapTokenForTokenWithPermit(address tokenIn, address tokenOut, uint256 amountIn, uint256 minAmountOut, uint256 deadline, struct IRouter.PermitParams permit) external returns (uint256 amountOut, uint256 intermediateUsdl)
```

Swap Token A → USDL → Token B using EIP-2612 permit on tokenIn

### createMarketWithPermit

```solidity
function createMarketWithPermit(string name, string symbol, uint8 feeStrategy, address optical, struct IRouter.PermitParams permit) external returns (address tokenAddr, address poolAddr, uint256 nftId)
```

Create a new market using EIP-2612 permit for USDL creation fee

