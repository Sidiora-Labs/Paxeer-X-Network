# Solidity API

## ERC20Base

Minimal ERC20 implementation with EIP-2612 permit support

_Includes transfer, approve, transferFrom, permit, DOMAIN_SEPARATOR_

### name

```solidity
string name
```

### symbol

```solidity
string symbol
```

### decimals

```solidity
uint8 decimals
```

### totalSupply

```solidity
uint256 totalSupply
```

### balanceOf

```solidity
mapping(address => uint256) balanceOf
```

### allowance

```solidity
mapping(address => mapping(address => uint256)) allowance
```

### nonces

```solidity
mapping(address => uint256) nonces
```

### DOMAIN_SEPARATOR

```solidity
bytes32 DOMAIN_SEPARATOR
```

### PERMIT_TYPEHASH

```solidity
bytes32 PERMIT_TYPEHASH
```

### InsufficientBalance

```solidity
error InsufficientBalance()
```

### InsufficientAllowance

```solidity
error InsufficientAllowance()
```

### InvalidPermit

```solidity
error InvalidPermit()
```

### PermitExpired

```solidity
error PermitExpired()
```

### ZeroAddress

```solidity
error ZeroAddress()
```

### Transfer

```solidity
event Transfer(address from, address to, uint256 value)
```

### Approval

```solidity
event Approval(address owner, address spender, uint256 value)
```

### constructor

```solidity
constructor(string _name, string _symbol, uint8 _decimals) internal
```

### approve

```solidity
function approve(address spender, uint256 amount) external returns (bool)
```

### transfer

```solidity
function transfer(address to, uint256 amount) external returns (bool)
```

### transferFrom

```solidity
function transferFrom(address from, address to, uint256 amount) external returns (bool)
```

### permit

```solidity
function permit(address owner_, address spender, uint256 value, uint256 deadline, uint8 v, bytes32 r, bytes32 s) external
```

EIP-2612 permit: approve via signature

### _transfer

```solidity
function _transfer(address from, address to, uint256 amount) internal
```

### _approve

```solidity
function _approve(address owner_, address spender, uint256 amount) internal
```

### _mint

```solidity
function _mint(address to, uint256 amount) internal
```

### _burn

```solidity
function _burn(address from, uint256 amount) internal
```

