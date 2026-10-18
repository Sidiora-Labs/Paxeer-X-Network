# Solidity API

## BuybackBurnOptical

Redirects a configurable portion of fee distributions to buy back tokens and burn them.

_Uses beforeFeeDistribution hook to redirect a percentage of fees.
     The redirected USDL is accumulated and can be used to execute buyback+burn via
     an external trigger. Immutable once deployed.

     Flow: beforeFeeDistribution reduces the fee by buybackBps%, accumulating the
     redirected portion. Anyone can call executeBuybackBurn() to send the accumulated
     USDL to the pool (simulating a buy) and burn the received tokens._

### NothingToExecute

```solidity
error NothingToExecute()
```

### DEAD_ADDRESS

```solidity
address DEAD_ADDRESS
```

Dead address for burning tokens

### buybackBps

```solidity
uint256 buybackBps
```

Percentage of fees redirected to buyback+burn (in bps, e.g., 2000 = 20%)

### accumulatedUsdl

```solidity
mapping(address => uint256) accumulatedUsdl
```

Accumulated USDL per pool for buyback execution

### BuybackAccumulated

```solidity
event BuybackAccumulated(address pool, uint256 amount)
```

### BuybackExecuted

```solidity
event BuybackExecuted(address pool, uint256 usdlUsed)
```

### constructor

```solidity
constructor(address _poolRegistry, address _owner, uint256 _buybackBps) public
```

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| _poolRegistry | address | PoolRegistry address for pool validation |
| _owner | address | Deployer/owner address |
| _buybackBps | uint256 | Percentage of fees to redirect (in bps, max 5000 = 50%) |

### getFlags

```solidity
function getFlags() external pure returns (uint8)
```

### beforeFeeDistribution

```solidity
function beforeFeeDistribution(address pool, uint256 feeAmount) external returns (uint256 adjustedFee)
```

### getAccumulatedUsdl

```solidity
function getAccumulatedUsdl(address pool) external view returns (uint256)
```

Get the accumulated USDL available for buyback for a pool

### markBuybackExecuted

```solidity
function markBuybackExecuted(address pool) external
```

Reset accumulated amount after external buyback execution

_Only owner can trigger this after performing the actual buyback off-chain or via Router_

