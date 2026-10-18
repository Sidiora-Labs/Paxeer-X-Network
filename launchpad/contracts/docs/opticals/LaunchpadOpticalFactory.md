# Solidity API

## LaunchpadOpticalFactory

Self-service factory for deploying LaunchpadOptical instances.
        Any creator can deploy their own vesting + capital-raise optical for their pool.

_UUPS proxy. Deploys immutable LaunchpadOptical contracts and auto-grants
     OPTICAL_CLAIM_ROLE on FeeAccumulator so the new optical can claim surplus._

### ZeroAddress

```solidity
error ZeroAddress()
```

### ArrayTooLong

```solidity
error ArrayTooLong()
```

### LaunchpadOpticalCreated

```solidity
event LaunchpadOpticalCreated(address optical, address creator, address teamClaimAddress, uint256 cliffDuration, uint256 vestingDuration, uint256 capitalRaiseBps, uint256 capitalRaiseDuration)
```

### poolRegistry

```solidity
address poolRegistry
```

PoolRegistry address (passed to each LaunchpadOptical)

### feeAccumulator

```solidity
address feeAccumulator
```

FeeAccumulator address (passed to each LaunchpadOptical + role granting)

### opticalRegistry

```solidity
address opticalRegistry
```

OpticalRegistry for optional auto-registration

### constructor

```solidity
constructor() public
```

### initialize

```solidity
function initialize(address _poolRegistry, address _feeAccumulator, address _opticalRegistry, address _admin) external
```

### createLaunchpadOptical

```solidity
function createLaunchpadOptical(address[] teamWallets, uint256 cliffDuration, uint256 vestingDuration, uint256 capitalRaiseBps, uint256 capitalRaiseDuration, address teamClaimAddress) external returns (address optical)
```

Deploy a new LaunchpadOptical for a project

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| teamWallets | address[] | Additional team wallets to vest (creator is auto-vested) |
| cliffDuration | uint256 | Hard lock period in seconds |
| vestingDuration | uint256 | Linear unlock period after cliff in seconds |
| capitalRaiseBps | uint256 | Fee percentage diverted to team (max 1000 = 10%) |
| capitalRaiseDuration | uint256 | How long the capital raise fee is active in seconds |
| teamClaimAddress | address | Where accumulated USDL is claimed to |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| optical | address | The address of the newly deployed LaunchpadOptical |

### getOpticalsByCreator

```solidity
function getOpticalsByCreator(address creator) external view returns (address[])
```

Get all LaunchpadOpticals deployed by a specific creator

### getDeployedCount

```solidity
function getDeployedCount() external view returns (uint256)
```

Get total number of deployed LaunchpadOpticals

### getAllOpticals

```solidity
function getAllOpticals(uint256 offset, uint256 limit) external view returns (address[])
```

Get paginated list of all deployed opticals

### _authorizeUpgrade

```solidity
function _authorizeUpgrade(address) internal
```

