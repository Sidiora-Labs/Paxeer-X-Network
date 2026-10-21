# Solidity API

## IFeesRouter

Interface for NFT-holder fee management

### NotNftOwner

```solidity
error NotNftOwner()
```

### WrongStrategy

```solidity
error WrongStrategy()
```

### ZeroAddress

```solidity
error ZeroAddress()
```

### FeesClaimed

```solidity
event FeesClaimed(uint256 nftId, address owner, uint256 amount)
```

### FeesBurned

```solidity
event FeesBurned(uint256 nftId, uint256 amount)
```

### AirdropExecuted

```solidity
event AirdropExecuted(uint256 nftId, uint256 amount)
```

### AirdropClaimed

```solidity
event AirdropClaimed(uint256 nftId, address holder, uint256 amount)
```

### LpRewardsExecuted

```solidity
event LpRewardsExecuted(uint256 nftId, uint256 amount)
```

### FeeStrategyChanged

```solidity
event FeeStrategyChanged(uint256 nftId, uint8 oldStrategy, uint8 newStrategy)
```

### setFeeStrategy

```solidity
function setFeeStrategy(uint256 nftId, uint8 newStrategy) external
```

Set fee strategy for a pool NFT (owner-only)

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| nftId | uint256 | The NFT token ID |
| newStrategy | uint8 | New fee strategy (0=CLAIM,1=BURN,2=AIRDROP,3=LP_REWARDS) |

### claimFees

```solidity
function claimFees(uint256 nftId) external returns (uint256 amount)
```

Claim accumulated fees (CLAIM strategy only, NFT owner)

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| nftId | uint256 | The NFT token ID |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| amount | uint256 | USDL amount claimed |

### executeBurn

```solidity
function executeBurn(uint256 nftId) external returns (uint256 amount)
```

Execute burn of accumulated fees (BURN strategy only, NFT owner)

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| nftId | uint256 | The NFT token ID |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| amount | uint256 | USDL amount burned |

### executeAirdrop

```solidity
function executeAirdrop(uint256 nftId) external returns (uint256 amount)
```

Execute airdrop distribution (AIRDROP strategy only, NFT owner)

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| nftId | uint256 | The NFT token ID |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| amount | uint256 | Total USDL distributed |

### claimAirdrop

```solidity
function claimAirdrop(uint256 nftId) external returns (uint256 amount)
```

Claim airdrop share as a token holder

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| nftId | uint256 | The NFT token ID for the pool |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| amount | uint256 | USDL amount claimed |

### executeLpRewards

```solidity
function executeLpRewards(uint256 nftId) external returns (uint256 amount)
```

Execute LP rewards transfer (LP_REWARDS strategy only, NFT owner)

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| nftId | uint256 | The NFT token ID |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| amount | uint256 | USDL amount sent to pool |

