# Solidity API

## OpticalRegistry

Trust-signaling registry for optical plugins.

_UUPS proxy. Opticals do NOT require registration to function —
     but unregistered opticals show as "unverified" to users/frontends.
     Admin registers/deregisters opticals with metadata._

### ZeroAddress

```solidity
error ZeroAddress()
```

### AlreadyRegistered

```solidity
error AlreadyRegistered()
```

### NotRegistered

```solidity
error NotRegistered()
```

### OpticalMetadata

```solidity
struct OpticalMetadata {
  string name;
  string description;
  uint8 riskLevel;
  string auditor;
  uint256 registeredAt;
}
```

### OpticalRegistered

```solidity
event OpticalRegistered(address optical, string name, uint8 riskLevel, uint256 timestamp)
```

### OpticalDeregistered

```solidity
event OpticalDeregistered(address optical, uint256 timestamp)
```

### OpticalMetadataUpdated

```solidity
event OpticalMetadataUpdated(address optical, uint256 timestamp)
```

### eventEmitter

```solidity
address eventEmitter
```

### constructor

```solidity
constructor() public
```

### initialize

```solidity
function initialize(address _eventEmitter, address _admin) external
```

### registerOptical

```solidity
function registerOptical(address optical, string name, string description, uint8 riskLevel, string auditor) external
```

Register an optical as approved with metadata

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| optical | address | The optical contract address |
| name | string | Human-readable name |
| description | string | Description of the optical's behavior |
| riskLevel | uint8 | Risk level 1-5 (1 = lowest) |
| auditor | string | Auditor name or "unaudited" |

### deregisterOptical

```solidity
function deregisterOptical(address optical) external
```

Deregister an optical (mark as not approved)

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| optical | address | The optical contract address to deregister |

### updateMetadata

```solidity
function updateMetadata(address optical, string name, string description, uint8 riskLevel, string auditor) external
```

Update metadata for a registered optical

### isRegistered

```solidity
function isRegistered(address optical) external view returns (bool)
```

Check if an optical is registered and approved

### getOpticalMetadata

```solidity
function getOpticalMetadata(address optical) external view returns (struct OpticalRegistry.OpticalMetadata)
```

Get metadata for an optical

### getAllOpticals

```solidity
function getAllOpticals(uint256 offset, uint256 limit) external view returns (address[])
```

Get all registered opticals (paginated)

### getOpticalCount

```solidity
function getOpticalCount() external view returns (uint256)
```

Get total count of ever-registered opticals

### _authorizeUpgrade

```solidity
function _authorizeUpgrade(address) internal
```

