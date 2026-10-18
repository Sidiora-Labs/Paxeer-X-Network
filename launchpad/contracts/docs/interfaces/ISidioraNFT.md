# Solidity API

## ISidioraNFT

Interface for the ERC721 contract representing fee rights per pool

### InvalidStrategy

```solidity
error InvalidStrategy()
```

### FeeStrategyChanged

```solidity
event FeeStrategyChanged(uint256 tokenId, uint8 oldStrategy, uint8 newStrategy)
```

### PoolNFTMinted

```solidity
event PoolNFTMinted(uint256 tokenId, address creator, address pool)
```

### mint

```solidity
function mint(address to, address pool, uint8 strategy) external returns (uint256 tokenId)
```

Mint a new pool NFT (factory-only via MINTER_ROLE)

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| to | address | The NFT recipient (pool creator) |
| pool | address | The pool address this NFT represents |
| strategy | uint8 | Initial fee strategy |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| tokenId | uint256 | The minted token ID |

### getFeeStrategy

```solidity
function getFeeStrategy(uint256 tokenId) external view returns (uint8)
```

Get the fee strategy for a token

### setFeeStrategy

```solidity
function setFeeStrategy(uint256 tokenId, uint8 newStrategy) external
```

Set the fee strategy for a token (caller must be owner or approved)

### getPoolAddress

```solidity
function getPoolAddress(uint256 tokenId) external view returns (address)
```

Get the pool address associated with an NFT

### nextTokenId

```solidity
function nextTokenId() external view returns (uint256)
```

Get current token ID counter

