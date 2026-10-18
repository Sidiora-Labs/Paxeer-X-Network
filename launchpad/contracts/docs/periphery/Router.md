# Solidity API

## Router

User-facing entry point for ALL protocol interactions.

_UUPS proxy. Handles validation, token transfers, and delegates to core contracts.
     Inherits Multicall for batching (e.g., create + buy in one tx).
     Supports EIP-2612 permit for gasless approvals and multihop Token→USDL→Token swaps._

### factory

```solidity
address factory
```

### poolRegistry

```solidity
address poolRegistry
```

### protocolConfig

```solidity
address protocolConfig
```

### usdlAddress

```solidity
address usdlAddress
```

### constructor

```solidity
constructor() public
```

### initialize

```solidity
function initialize(address _factory, address _poolRegistry, address _protocolConfig, address _usdlAddress, address _admin) external
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

### _createMarket

```solidity
function _createMarket(string name, string symbol, uint8 feeStrategy, address optical) internal returns (address tokenAddr, address poolAddr, uint256 nftId)
```

### _buy

```solidity
function _buy(address pool, uint256 usdlAmountIn, uint256 minTokensOut, uint256 deadline) internal returns (uint256 amountOut)
```

### _sell

```solidity
function _sell(address pool, uint256 tokenAmountIn, uint256 minUsdlOut, uint256 deadline) internal returns (uint256 amountOut)
```

### _swapTokenForToken

```solidity
function _swapTokenForToken(address tokenIn, address tokenOut, uint256 amountIn, uint256 minAmountOut, uint256 deadline) internal returns (uint256 amountOut, uint256 intermediateUsdl)
```

_Executes Token A → USDL → Token B atomically.
     Leg 1: Sell tokenIn on poolA, Router receives USDL.
     Leg 2: Buy tokenOut on poolB with intermediate USDL, user receives tokenOut._

### _isRegisteredPool

```solidity
function _isRegisteredPool(address pool) internal view returns (bool)
```

### _executePermit

```solidity
function _executePermit(address token, address owner_, address spender, struct IRouter.PermitParams permit) internal
```

_Calls EIP-2612 permit on a token. Fails silently if permit reverts
     (e.g., nonce already used, token doesn't support permit) to avoid
     griefing via front-run permit._

### _authorizeUpgrade

```solidity
function _authorizeUpgrade(address) internal
```

