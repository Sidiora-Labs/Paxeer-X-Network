# Solidity API

## MockRevertingERC20

ERC20 that always reverts on transfer/transferFrom/approve
Used to test TransferHelper's error handling

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

### shouldRevert

```solidity
bool shouldRevert
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

### setShouldRevert

```solidity
function setShouldRevert(bool _shouldRevert) external
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

