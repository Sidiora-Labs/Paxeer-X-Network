# Solidity API

## IWETH

Interface for Wrapped Native Coin (WETH / WPAX)

_Standard WETH9 surface used by Uniswap, Aave, and every major DeFi protocol.
     On Paxeer Network, the canonical WETH is WPAX at
     0xe5ccf339d1c89c7e6c6768b28507f78b861fc1de (see LIVE_ADDRESSES in tests)._

### deposit

```solidity
function deposit() external payable
```

### withdraw

```solidity
function withdraw(uint256 wad) external
```

### totalSupply

```solidity
function totalSupply() external view returns (uint256)
```

### balanceOf

```solidity
function balanceOf(address owner) external view returns (uint256)
```

### transfer

```solidity
function transfer(address to, uint256 value) external returns (bool)
```

### transferFrom

```solidity
function transferFrom(address from, address to, uint256 value) external returns (bool)
```

### approve

```solidity
function approve(address spender, uint256 value) external returns (bool)
```

### allowance

```solidity
function allowance(address owner, address spender) external view returns (uint256)
```

### Deposit

```solidity
event Deposit(address dst, uint256 wad)
```

### Withdrawal

```solidity
event Withdrawal(address src, uint256 wad)
```

