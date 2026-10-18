# Solidity API

## MockFeeOnTransferERC20

ERC20 variant that siphons a configurable basis-point fee on every
        transfer, used to prove {PECORVault.pullTokens} returns the real
        received amount (spec §7.5 fee-on-transfer safety, S5 regression).

_The fee is burned — the contract never needs a collector. A 0-bps
     setting reduces the mock to a normal ERC20._

### BPS_DENOMINATOR

```solidity
uint256 BPS_DENOMINATOR
```

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

### feeBps

```solidity
uint256 feeBps
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

### FeeUpdated

```solidity
event FeeUpdated(uint256 bps)
```

### constructor

```solidity
constructor(string name_, string symbol_, uint8 decimals_, uint256 feeBps_) public
```

### setFeeBps

```solidity
function setFeeBps(uint256 bps) external
```

### mint

```solidity
function mint(address to, uint256 amount) external
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

### _transferWithFee

```solidity
function _transferWithFee(address from, address to, uint256 amount) internal
```

