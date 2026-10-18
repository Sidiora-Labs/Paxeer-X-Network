# Solidity API

## TaxOptical

Applies additional configurable buy/sell tax via beforeSwap amountDelta.

_Uses beforeSwap hook. Tax is taken by reducing the effective amountIn
     (negative amountDelta). The taxed portion remains in the pool, effectively
     benefiting liquidity. Configurable separate buy/sell rates, capped at 10%.
     Immutable once deployed — rates are set at construction._

### TaxTooHigh

```solidity
error TaxTooHigh()
```

### MAX_TAX_BPS

```solidity
uint256 MAX_TAX_BPS
```

Maximum allowed tax in basis points (1000 = 10%)

### buyTaxBps

```solidity
uint256 buyTaxBps
```

Buy tax in basis points (e.g., 100 = 1%)

### sellTaxBps

```solidity
uint256 sellTaxBps
```

Sell tax in basis points (e.g., 100 = 1%)

### constructor

```solidity
constructor(address _poolRegistry, address _owner, uint256 _buyTaxBps, uint256 _sellTaxBps) public
```

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| _poolRegistry | address | PoolRegistry address for pool validation |
| _owner | address | Deployer/owner address |
| _buyTaxBps | uint256 | Buy tax in basis points (max 1000 = 10%) |
| _sellTaxBps | uint256 | Sell tax in basis points (max 1000 = 10%) |

### getFlags

```solidity
function getFlags() external pure returns (uint8)
```

### beforeSwap

```solidity
function beforeSwap(address, address, bool isBuy, uint256 amountIn) external view returns (bool proceed, int256 amountDelta)
```

