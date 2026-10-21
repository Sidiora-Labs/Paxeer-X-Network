# Solidity API

## PoolAddress

Computes deterministic pool addresses from BeaconProxy CREATE2 parameters

_Used by Factory, Router, and Quoter to predict pool addresses before deployment_

### computeAddress

```solidity
function computeAddress(address factory, address beacon, address token, bytes creationCode) internal pure returns (address pool)
```

Computes the CREATE2 address for a pool BeaconProxy

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| factory | address | The factory contract address (deployer) |
| beacon | address | The PoolBeacon address |
| token | address | The SidioraERC20 token address (used in salt) |
| creationCode | bytes | The BeaconProxy creation code |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| pool | address | The deterministic pool address |

### computeSalt

```solidity
function computeSalt(address token) internal pure returns (bytes32)
```

Computes the salt used for pool CREATE2 deployment

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| token | address | The token address |

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| [0] | bytes32 | salt The salt bytes32 |

