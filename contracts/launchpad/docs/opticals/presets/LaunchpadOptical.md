# Solidity API

## LaunchpadOptical

Preset optical for serious projects: cliff + linear vesting for team wallets,
        plus a time-limited capital raise fee that diverts a portion of swap fees to the team as USDL.

_Immutable once deployed. One instance per pool (constructor params are pool-specific).

     HOOKS USED:
       - beforeSwap:             Enforce cliff lock + linear vesting on team sells
       - afterSwap:              Track cumulative tokens sold per vested wallet (for vesting math)
       - beforeFeeDistribution:  Divert capitalRaiseBps of pool fees to optical surplus for team

     CAPITAL RAISE FLOW:
       1. beforeFeeDistribution reduces the pool fee by capitalRaiseBps%
       2. FeeAccumulator records the difference as opticalSurplus
       3. Team calls claimCapital() → LaunchpadOptical calls FeeAccumulator.claimOpticalSurplus()
       4. USDL is transferred to teamClaimAddress_

### CliffActive

```solidity
error CliffActive()
```

### VestingExceeded

```solidity
error VestingExceeded()
```

### CapitalRaiseTooHigh

```solidity
error CapitalRaiseTooHigh()
```

### DurationTooLong

```solidity
error DurationTooLong()
```

### NoTeamWallets

```solidity
error NoTeamWallets()
```

### TooManyTeamWallets

```solidity
error TooManyTeamWallets()
```

### NothingToClaim

```solidity
error NothingToClaim()
```

### NotTeam

```solidity
error NotTeam()
```

### PoolStartTimeRecorded

```solidity
event PoolStartTimeRecorded(address pool, uint256 startTime)
```

### SellBlockedCliff

```solidity
event SellBlockedCliff(address pool, address sender, uint256 amountIn)
```

### SellBlockedVesting

```solidity
event SellBlockedVesting(address pool, address sender, uint256 amountIn, uint256 maxSellable)
```

### CapitalRaiseAccumulated

```solidity
event CapitalRaiseAccumulated(address pool, uint256 amount, uint256 totalRaisedAmount)
```

### CapitalRaiseClaimed

```solidity
event CapitalRaiseClaimed(address pool, uint256 amount, address recipient)
```

### MAX_CAPITAL_RAISE_BPS

```solidity
uint256 MAX_CAPITAL_RAISE_BPS
```

### MAX_CLIFF_DURATION

```solidity
uint256 MAX_CLIFF_DURATION
```

### MAX_VESTING_DURATION

```solidity
uint256 MAX_VESTING_DURATION
```

### MAX_RAISE_DURATION

```solidity
uint256 MAX_RAISE_DURATION
```

### MAX_TEAM_WALLETS

```solidity
uint256 MAX_TEAM_WALLETS
```

### creator

```solidity
address creator
```

Pool creator wallet (auto-vested)

### teamClaimAddress

```solidity
address teamClaimAddress
```

Address where accumulated USDL capital is claimed to

### feeAccumulator

```solidity
address feeAccumulator
```

FeeAccumulator address for claiming optical surplus

### cliffDuration

```solidity
uint256 cliffDuration
```

Hard lock period in seconds before any vesting begins

### vestingDuration

```solidity
uint256 vestingDuration
```

Linear unlock period in seconds after the cliff

### capitalRaiseBps

```solidity
uint256 capitalRaiseBps
```

Fee percentage diverted to team (in bps, max 1000 = 10%)

### capitalRaiseDuration

```solidity
uint256 capitalRaiseDuration
```

How long the capital raise fee is active (in seconds)

### teamWalletCount

```solidity
uint256 teamWalletCount
```

Number of team wallets (excluding creator)

### isVested

```solidity
mapping(address => bool) isVested
```

Whether an address is subject to vesting restrictions

### poolStartTime

```solidity
mapping(address => uint256) poolStartTime
```

Per-pool creation timestamp (recorded on first interaction)

### tokensSold

```solidity
mapping(address => mapping(address => uint256)) tokensSold
```

Cumulative tokens sold per vested wallet per pool (for vesting math)

_pool → wallet → amount sold_

### accumulatedUsdl

```solidity
mapping(address => uint256) accumulatedUsdl
```

USDL accumulated per pool for team capital raise (accounting only)

### totalRaised

```solidity
mapping(address => uint256) totalRaised
```

Lifetime total USDL raised per pool

### constructor

```solidity
constructor(address _poolRegistry, address _owner, address _creator, address[] _teamWallets, uint256 _cliffDuration, uint256 _vestingDuration, uint256 _capitalRaiseBps, uint256 _capitalRaiseDuration, address _teamClaimAddress, address _feeAccumulator) public
```

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| _poolRegistry | address | PoolRegistry address for pool validation |
| _owner | address | Deployer/owner address |
| _creator | address | Pool creator wallet (auto-vested) |
| _teamWallets | address[] | Additional team wallets to vest |
| _cliffDuration | uint256 | Hard lock period in seconds |
| _vestingDuration | uint256 | Linear unlock period after cliff in seconds |
| _capitalRaiseBps | uint256 | Fee percentage diverted to team (max 1000 = 10%) |
| _capitalRaiseDuration | uint256 | How long the capital raise fee is active in seconds |
| _teamClaimAddress | address | Where accumulated USDL is claimed to |
| _feeAccumulator | address | FeeAccumulator contract address |

### getFlags

```solidity
function getFlags() external pure returns (uint8)
```

### beforeSwap

```solidity
function beforeSwap(address pool, address sender, bool isBuy, uint256 amountIn) external returns (bool proceed, int256 amountDelta)
```

### afterSwap

```solidity
function afterSwap(address pool, address sender, bool isBuy, uint256 amountIn, uint256) external returns (bytes4)
```

### beforeFeeDistribution

```solidity
function beforeFeeDistribution(address pool, uint256 feeAmount) external returns (uint256 adjustedFee)
```

### claimCapital

```solidity
function claimCapital(address pool) external
```

Claim accumulated USDL capital for a pool

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| pool | address | The pool address to claim capital from |

### getVestingInfo

```solidity
function getVestingInfo(address pool, address wallet) external view returns (bool vested, uint256 cliffEnd, uint256 vestEnd, uint256 vestedAmount, uint256 sold, uint256 maxSellableNow)
```

Get vesting info for a wallet in a specific pool

### getCapitalRaiseInfo

```solidity
function getCapitalRaiseInfo(address pool) external view returns (uint256 accumulated, uint256 totalRaisedAmount, uint256 raiseEndTime, bool isActive)
```

Get capital raise info for a pool

### isTeamWallet

```solidity
function isTeamWallet(address wallet) external view returns (bool)
```

Check if an address is a team wallet (vested)

### _getTokenBalance

```solidity
function _getTokenBalance(address pool, address wallet) internal view returns (uint256)
```

_Read token balance of a wallet via the pool's token address_

