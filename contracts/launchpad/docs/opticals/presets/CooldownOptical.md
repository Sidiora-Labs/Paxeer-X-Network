# Solidity API

## CooldownOptical

Enforces minimum time between trades per wallet (anti-bot).

_Uses beforeSwap hook. Tracks last trade timestamp per pool per wallet.
     Configurable cooldown period. Independent per wallet.
     Immutable once deployed._

### CooldownActive

```solidity
error CooldownActive()
```

### cooldownSeconds

```solidity
uint256 cooldownSeconds
```

Minimum seconds between trades for any given wallet

### lastTradeTime

```solidity
mapping(address => mapping(address => uint256)) lastTradeTime
```

Last trade timestamp per pool per wallet: pool => wallet => timestamp

### constructor

```solidity
constructor(address _poolRegistry, address _owner, uint256 _cooldownSeconds) public
```

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| _poolRegistry | address | PoolRegistry address for pool validation |
| _owner | address | Deployer/owner address |
| _cooldownSeconds | uint256 | Minimum seconds between trades per wallet |

### getFlags

```solidity
function getFlags() external pure returns (uint8)
```

### beforeSwap

```solidity
function beforeSwap(address pool, address sender, bool, uint256) external returns (bool proceed, int256 amountDelta)
```

