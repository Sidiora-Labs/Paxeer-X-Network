# Solidity API

## MockERC20

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
constructor(string _name, string _symbol, uint8 _decimals) public
```

### mint

```solidity
function mint(address to, uint256 amount) external
```

### burn

```solidity
function burn(address from, uint256 amount) external
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

