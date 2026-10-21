# Solidity API

## ISidioraFactory

Interface for the market creation orchestrator

### ZeroAddress

```solidity
error ZeroAddress()
```

### InsufficientCreationFee

```solidity
error InsufficientCreationFee()
```

### DuplicateToken

```solidity
error DuplicateToken()
```

### MarketCreated

```solidity
event MarketCreated(address token, address pool, address creator, uint256 nftId, address optical)
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
| feeStrategy | uint8 | Initial fee strategy for the pool NFT (0=CLAIM,1=BURN,2=AIRDROP,3=LP_REWARDS) |
| optical | address | Optional optical hook contract (address(0) for none) |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| tokenAddr | address | The deployed token address |
| poolAddr | address | The deployed pool address |
| nftId | uint256 | The minted NFT token ID |

### createMarketFor

```solidity
function createMarketFor(address creator, string name, string symbol, uint8 feeStrategy, address optical) external returns (address tokenAddr, address poolAddr, uint256 nftId)
```

Create a new market on behalf of a creator (Router-only via ROUTER_ROLE)

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| creator | address | The actual creator address (receives NFT, becomes guardian) |
| name | string | Token name |
| symbol | string | Token symbol |
| feeStrategy | uint8 | Initial fee strategy for the pool NFT |
| optical | address | Optional optical hook contract (address(0) for none) |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| tokenAddr | address | The deployed token address |
| poolAddr | address | The deployed pool address |
| nftId | uint256 | The minted NFT token ID |

### getNonce

```solidity
function getNonce(address creator) external view returns (uint256)
```

Get the nonce for a creator (used for CREATE2 determinism)

### poolBeacon

```solidity
function poolBeacon() external view returns (address)
```

Get the pool beacon address

### nftContract

```solidity
function nftContract() external view returns (address)
```

Get the NFT contract address

### poolRegistry

```solidity
function poolRegistry() external view returns (address)
```

Get the pool registry address

### eventEmitter

```solidity
function eventEmitter() external view returns (address)
```

Get the event emitter address

### protocolConfig

```solidity
function protocolConfig() external view returns (address)
```

Get the protocol config address

### treasury

```solidity
function treasury() external view returns (address)
```

Get the treasury address

