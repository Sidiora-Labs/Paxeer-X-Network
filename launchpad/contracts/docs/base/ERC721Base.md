# Solidity API

## ERC721Base

Minimal ERC721 implementation with ERC165 introspection

_Supports transfer, safeTransfer, approve, setApprovalForAll, supportsInterface_

### name

```solidity
string name
```

### symbol

```solidity
string symbol
```

### TokenNotFound

```solidity
error TokenNotFound()
```

### NotTokenOwner

```solidity
error NotTokenOwner()
```

### NotApproved

```solidity
error NotApproved()
```

### ZeroAddress

```solidity
error ZeroAddress()
```

### AlreadyMinted

```solidity
error AlreadyMinted()
```

### NonERC721Receiver

```solidity
error NonERC721Receiver()
```

### Transfer

```solidity
event Transfer(address from, address to, uint256 tokenId)
```

### Approval

```solidity
event Approval(address owner, address approved, uint256 tokenId)
```

### ApprovalForAll

```solidity
event ApprovalForAll(address owner, address operator, bool approved)
```

### constructor

```solidity
constructor(string _name, string _symbol) internal
```

### supportsInterface

```solidity
function supportsInterface(bytes4 interfaceId) public view virtual returns (bool)
```

ERC165: Returns true for ERC721 and ERC165 interface IDs

### balanceOf

```solidity
function balanceOf(address owner_) public view returns (uint256)
```

### ownerOf

```solidity
function ownerOf(uint256 tokenId) public view returns (address owner_)
```

### approve

```solidity
function approve(address to, uint256 tokenId) external
```

### getApproved

```solidity
function getApproved(uint256 tokenId) public view returns (address)
```

### setApprovalForAll

```solidity
function setApprovalForAll(address operator, bool approved) external
```

### isApprovedForAll

```solidity
function isApprovedForAll(address owner_, address operator) public view returns (bool)
```

### transferFrom

```solidity
function transferFrom(address from, address to, uint256 tokenId) public virtual
```

### safeTransferFrom

```solidity
function safeTransferFrom(address from, address to, uint256 tokenId) external
```

### safeTransferFrom

```solidity
function safeTransferFrom(address from, address to, uint256 tokenId, bytes data) public
```

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

### _isApprovedOrOwner

```solidity
function _isApprovedOrOwner(address spender, uint256 tokenId) internal view returns (bool)
```

### _exists

```solidity
function _exists(uint256 tokenId) internal view returns (bool)
```

## IERC721Receiver

### onERC721Received

```solidity
function onERC721Received(address operator, address from, uint256 tokenId, bytes data) external returns (bytes4)
```

