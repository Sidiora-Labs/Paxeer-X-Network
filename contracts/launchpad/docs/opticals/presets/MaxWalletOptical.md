# Solidity API

## MaxWalletOptical

Enforces maximum token holding percentage per wallet after buy swaps.

_Uses afterSwap hook. Immutable once deployed.
     Pool address is exempt (it holds the liquidity).
     Only checks on buys — sells naturally reduce holdings._

### MaxWalletExceeded

```solidity
error MaxWalletExceeded()
```

### maxWalletBps

```solidity
uint256 maxWalletBps
```

Maximum wallet holding in basis points of total supply (e.g., 200 = 2%)

### exempt

```solidity
mapping(address => bool) exempt
```

Addresses exempt from max wallet check

### constructor

```solidity
constructor(address _poolRegistry, address _owner, uint256 _maxWalletBps) public
```

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| _poolRegistry | address | PoolRegistry address for pool validation |
| _owner | address | Deployer/owner address |
| _maxWalletBps | uint256 | Maximum holding in bps of total supply |

### setExempt

```solidity
function setExempt(address account, bool isExempt) external
```

Set an address as exempt from max wallet check

_Only owner can set exemptions (e.g., pool address, treasury)_

### getFlags

```solidity
function getFlags() external pure returns (uint8)
```

### afterSwap

```solidity
function afterSwap(address pool, address sender, bool isBuy, uint256, uint256) external returns (bytes4)
```

