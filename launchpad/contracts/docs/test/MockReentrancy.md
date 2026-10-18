# Solidity API

## MockReentrancyGuard

### counter

```solidity
uint256 counter
```

### protectedIncrement

```solidity
function protectedIncrement() external
```

### unprotectedIncrement

```solidity
function unprotectedIncrement() external
```

### reentrantCall

```solidity
function reentrantCall() external
```

### crossFunctionReentrantCall

```solidity
function crossFunctionReentrantCall() external
```

## ReentrancyAttacker

### target

```solidity
contract MockReentrancyGuard target
```

### attackCount

```solidity
uint256 attackCount
```

### constructor

```solidity
constructor(address _target) public
```

### attack

```solidity
function attack() external
```

### receive

```solidity
receive() external payable
```

