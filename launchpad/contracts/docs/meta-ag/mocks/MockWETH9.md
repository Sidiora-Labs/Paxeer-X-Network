# Solidity API

## MockWETH9

Minimal WETH-compatible mock that mirrors the WPAX9 live contract
        on Paxeer Network. Used by {PECORVault} tests to exercise
        `depositNative` (wrap) and `withdrawNative` (unwrap) paths and to
        assert the {PECORVault.receive} WethOnly guard.

_Keeps the WETH9 ABI surface minimal enough to test the vault without
     pulling in an external WETH9 source._

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

### InsufficientBalance

```solidity
error InsufficientBalance()
```

### InsufficientAllowance

```solidity
error InsufficientAllowance()
```

### totalSupply

```solidity
function totalSupply() external view returns (uint256)
```

### balanceOf

```solidity
function balanceOf(address owner) external view returns (uint256)
```

### allowance

```solidity
function allowance(address owner, address spender) external view returns (uint256)
```

### deposit

```solidity
function deposit() external payable
```

### withdraw

```solidity
function withdraw(uint256 wad) external
```

### approve

```solidity
function approve(address spender, uint256 value) external returns (bool)
```

### transfer

```solidity
function transfer(address to, uint256 value) external returns (bool)
```

### transferFrom

```solidity
function transferFrom(address from, address to, uint256 value) external returns (bool)
```

### _transfer

```solidity
function _transfer(address from, address to, uint256 value) internal returns (bool)
```

### receive

```solidity
receive() external payable
```

_Accept native to let tests simulate arbitrary funding paths._

