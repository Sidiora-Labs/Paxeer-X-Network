# Solidity API

## ERC721Enumerable

Enumeration extension for ERC721: totalSupply, tokenByIndex, tokenOfOwnerByIndex

### IndexOutOfBounds

```solidity
error IndexOutOfBounds()
```

### supportsInterface

```solidity
function supportsInterface(bytes4 interfaceId) public view virtual returns (bool)
```

ERC165: add ERC721Enumerable interface support

### totalSupply

```solidity
function totalSupply() public view returns (uint256)
```

Returns the total number of tokens

### tokenByIndex

```solidity
function tokenByIndex(uint256 index) external view returns (uint256)
```

Returns the token ID at a given index across all tokens

### tokenOfOwnerByIndex

```solidity
function tokenOfOwnerByIndex(address owner_, uint256 index) external view returns (uint256)
```

Returns the token ID at a given index for an owner

### _mint

```solidity
function _mint(address to, uint256 tokenId) internal virtual
```

### _burn

```solidity
function _burn(uint256 tokenId) internal virtual
```

### _transfer

```solidity
function _transfer(address from, address to, uint256 tokenId) internal virtual
```

