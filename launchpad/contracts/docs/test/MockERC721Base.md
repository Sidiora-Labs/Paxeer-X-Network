# Solidity API

## MockERC721Base

### constructor

```solidity
constructor() public
```

### mint

```solidity
function mint(address to, uint256 tokenId) external
```

### burn

```solidity
function burn(uint256 tokenId) external
```

### exists

```solidity
function exists(uint256 tokenId) external view returns (bool)
```

## MockERC721Receiver

### onERC721Received

```solidity
function onERC721Received(address, address, uint256, bytes) external pure returns (bytes4)
```

## MockBadERC721Receiver

### onERC721Received

```solidity
function onERC721Received(address, address, uint256, bytes) external pure returns (bytes4)
```

