# Solidity API

## IFeeAccumulator

Interface for fee tracking and distribution across pools

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

### NoFeesAccumulated

```solidity
error NoFeesAccumulated()
```

### WrongStrategy

```solidity
error WrongStrategy()
```

### AlreadyClaimed

```solidity
error AlreadyClaimed()
```

### AirdropNotTriggered

```solidity
error AirdropNotTriggered()
```

### InsufficientSurplus

```solidity
error InsufficientSurplus()
```

### FeeRecorded

```solidity
event FeeRecorded(address pool, uint256 feeAmount, uint256 protocolCut, uint256 poolCut)
```

### FeesClaimed

```solidity
event FeesClaimed(address pool, address recipient, uint256 amount)
```

### FeesBurned

```solidity
event FeesBurned(address pool, uint256 amount)
```

### AirdropTriggered

```solidity
event AirdropTriggered(address pool, uint256 totalAmount, uint256 epoch)
```

### AirdropClaimed

```solidity
event AirdropClaimed(address pool, address holder, uint256 amount, uint256 epoch)
```

### LpRewardsSent

```solidity
event LpRewardsSent(address pool, uint256 amount)
```

### ProtocolFeeSwept

```solidity
event ProtocolFeeSwept(uint256 amount)
```

### OpticalSurplusRecorded

```solidity
event OpticalSurplusRecorded(address pool, uint256 amount)
```

### OpticalSurplusClaimed

```solidity
event OpticalSurplusClaimed(address pool, address recipient, uint256 amount)
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

### authorizePool

```solidity
function authorizePool(address pool) external
```

Authorize a pool to record fees (Factory-only via FACTORY_ROLE)

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| pool | address | The pool address to authorize |

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

### authorizeOptical

```solidity
function authorizeOptical(address optical) external
```

Authorize an optical to claim surplus (OPTICAL_GRANTER_ROLE only)

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| optical | address | The optical contract address to authorize |

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

