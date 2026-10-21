# Solidity API

## ITreasury

Interface for the protocol treasury contract

### InsufficientBalance

```solidity
error InsufficientBalance()
```

### Unauthorized

```solidity
error Unauthorized()
```

### ZeroAddress

```solidity
error ZeroAddress()
```

### ZeroAmount

```solidity
error ZeroAmount()
```

### Deposited

```solidity
event Deposited(address token, address from, uint256 amount)
```

### Withdrawn

```solidity
event Withdrawn(address token, address to, uint256 amount)
```

### deposit

```solidity
function deposit(address token, uint256 amount) external
```

### withdraw

```solidity
function withdraw(address token, address to, uint256 amount) external
```

### getBalance

```solidity
function getBalance(address token) external view returns (uint256)
```

