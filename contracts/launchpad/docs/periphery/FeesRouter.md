# Solidity API

## FeesRouter

NFT-holder interface for fee management.

_UUPS proxy. Validates NFT ownership, reads strategy, delegates to FeeAccumulator._

### nftContract

```solidity
address nftContract
```

### feeAccumulator

```solidity
address feeAccumulator
```

### poolRegistry

```solidity
address poolRegistry
```

### constructor

```solidity
constructor() public
```

### initialize

```solidity
function initialize(address _nftContract, address _feeAccumulator, address _poolRegistry, address _admin) external
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

### _requireNftOwner

```solidity
function _requireNftOwner(uint256 nftId) internal view
```

### _requireStrategy

```solidity
function _requireStrategy(uint256 nftId, uint8 expectedStrategy) internal view
```

### _authorizeUpgrade

```solidity
function _authorizeUpgrade(address) internal
```

