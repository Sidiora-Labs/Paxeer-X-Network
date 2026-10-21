# Solidity API

## SidioraNFT

ERC721 representing fee rights for a pool. One NFT per pool.

_UUPS proxy singleton. Factory mints via MINTER_ROLE._

### MINTER_ROLE

```solidity
bytes32 MINTER_ROLE
```

### STRATEGY_SETTER_ROLE

```solidity
bytes32 STRATEGY_SETTER_ROLE
```

### nextTokenId

```solidity
uint256 nextTokenId
```

Get current token ID counter

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
function initialize(string _name, string _symbol, address _eventEmitter, address _admin) external
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

### supportsInterface

```solidity
function supportsInterface(bytes4 interfaceId) public view virtual returns (bool)
```

ERC165 override

### _authorizeUpgrade

```solidity
function _authorizeUpgrade(address) internal
```

