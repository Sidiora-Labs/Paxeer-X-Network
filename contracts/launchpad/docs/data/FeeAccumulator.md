# Solidity API

## IOpticalFee

_Minimal interface for optical hooks used by FeeAccumulator_

### beforeFeeDistribution

```solidity
function beforeFeeDistribution(address pool, uint256 feeAmount) external returns (uint256 adjustedFee)
```

### afterFeeDistribution

```solidity
function afterFeeDistribution(address pool, uint256 feeAmount) external returns (bytes4)
```

### getFlags

```solidity
function getFlags() external view returns (uint8)
```

## FeeAccumulator

Tracks accumulated fees per pool and executes fee distribution strategies

_UUPS proxy. Inspired by Aerodrome's PoolFees separation._

### POOL_ROLE

```solidity
bytes32 POOL_ROLE
```

### FEES_ROUTER_ROLE

```solidity
bytes32 FEES_ROUTER_ROLE
```

### FACTORY_ROLE

```solidity
bytes32 FACTORY_ROLE
```

### OPTICAL_CLAIM_ROLE

```solidity
bytes32 OPTICAL_CLAIM_ROLE
```

### OPTICAL_GRANTER_ROLE

```solidity
bytes32 OPTICAL_GRANTER_ROLE
```

### authorizePool

```solidity
function authorizePool(address pool) external
```

Authorize a pool to record fees. Called by Factory on market creation.

### DEAD_ADDRESS

```solidity
address DEAD_ADDRESS
```

### protocolConfig

```solidity
address protocolConfig
```

### treasury

```solidity
address treasury
```

### poolRegistry

```solidity
address poolRegistry
```

### eventEmitter

```solidity
address eventEmitter
```

### usdlAddress

```solidity
address usdlAddress
```

### constructor

```solidity
constructor() public
```

### initialize

```solidity
function initialize(address _protocolConfig, address _treasury, address _poolRegistry, address _eventEmitter, address _usdlAddress, address _admin) external
```

### recordFee

```solidity
function recordFee(address pool, uint256 feeAmount) external
```

Record fees from a swap (pool-only)

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| pool | address | The pool address the fees are from |
| feeAmount | uint256 | Total fee amount in USDL |

### claim

```solidity
function claim(address pool, address recipient) external returns (uint256 amount)
```

Claim accumulated fees for a pool (CLAIM strategy, FeesRouter-only)

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| pool | address | The pool address |
| recipient | address | Address to receive the fees |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| amount | uint256 | The amount of USDL claimed |

### burn

```solidity
function burn(address pool) external returns (uint256 amount)
```

Burn accumulated fees for a pool (BURN strategy, FeesRouter-only)

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| pool | address | The pool address |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| amount | uint256 | The amount of USDL burned |

### triggerAirdrop

```solidity
function triggerAirdrop(address pool) external returns (uint256 totalAmount)
```

Trigger airdrop distribution for a pool (AIRDROP strategy, FeesRouter-only)

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| pool | address | The pool address |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| totalAmount | uint256 | The total amount distributed |

### claimAirdrop

```solidity
function claimAirdrop(address pool, address holder) external returns (uint256 amount)
```

Claim airdrop share for a token holder

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| pool | address | The pool address |
| holder | address | The token holder address |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| amount | uint256 | The amount of USDL claimed |

### sendLpRewards

```solidity
function sendLpRewards(address pool) external returns (uint256 amount)
```

Send accumulated fees to pool as LP rewards (LP_REWARDS strategy, FeesRouter-only)

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| pool | address | The pool address |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| amount | uint256 | The amount of USDL sent |

### getAccumulatedFees

```solidity
function getAccumulatedFees(address pool) external view returns (uint256)
```

Get accumulated fees for a pool

### getProtocolFeesPending

```solidity
function getProtocolFeesPending() external view returns (uint256)
```

Get pending protocol fees waiting to be swept

### getAirdropBalance

```solidity
function getAirdropBalance(address pool) external view returns (uint256)
```

Get airdrop balance for a pool

### getLpRewardsBalance

```solidity
function getLpRewardsBalance(address pool) external view returns (uint256)
```

Get LP rewards balance for a pool

### getAirdropEpoch

```solidity
function getAirdropEpoch(address pool) external view returns (uint256)
```

Get current airdrop epoch for a pool

### hasClaimedAirdrop

```solidity
function hasClaimedAirdrop(address pool, address holder, uint256 epoch) external view returns (bool)
```

Check if holder has claimed for a specific epoch

### _getTokenBalance

```solidity
function _getTokenBalance(address token, address account) internal view returns (uint256)
```

### _getTokenTotalSupply

```solidity
function _getTokenTotalSupply(address token) internal view returns (uint256)
```

### authorizeOptical

```solidity
function authorizeOptical(address optical) external
```

Authorize an optical contract to claim surplus. Callable by OPTICAL_GRANTER_ROLE (e.g. LaunchpadOpticalFactory).

### recordOpticalSurplus

```solidity
function recordOpticalSurplus(address pool, uint256 amount) external
```

Record optical surplus when beforeFeeDistribution returns adjustedFee < feeAmount

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| pool | address | The pool address |
| amount | uint256 | The surplus amount to record |

### claimOpticalSurplus

```solidity
function claimOpticalSurplus(address pool, uint256 amount, address recipient) external
```

Claim optical surplus for a pool (OPTICAL_CLAIM_ROLE only)

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| pool | address | The pool address |
| amount | uint256 | Amount to claim |
| recipient | address | Address to receive the USDL |

### getOpticalSurplus

```solidity
function getOpticalSurplus(address pool) external view returns (uint256)
```

Get optical surplus balance for a pool

### _authorizeUpgrade

```solidity
function _authorizeUpgrade(address) internal
```

