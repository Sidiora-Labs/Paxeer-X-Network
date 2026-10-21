# Solidity API

## IERC20Minimal

Zero-dep minimal ERC20 surface used by the Sidiora Meta-AG stack.

_Introduced in Phase 3 (Task 3.1) as the first internal consumer of ERC20
     view/transfer semantics beyond oracle reads. Later phases (engine,
     adapters, router) reuse this interface verbatim to keep the meta-ag
     layer free of external dependencies.

     This file is scoped to `contracts/meta-ag/` and does not expand the
     Phase 1 frozen public surface — it is an internal utility interface._

### balanceOf

```solidity
function balanceOf(address account) external view returns (uint256)
```

### totalSupply

```solidity
function totalSupply() external view returns (uint256)
```

### transfer

```solidity
function transfer(address to, uint256 amount) external returns (bool)
```

### transferFrom

```solidity
function transferFrom(address from, address to, uint256 amount) external returns (bool)
```

### approve

```solidity
function approve(address spender, uint256 amount) external returns (bool)
```

### allowance

```solidity
function allowance(address owner, address spender) external view returns (uint256)
```

### decimals

```solidity
function decimals() external view returns (uint8)
```

