# Solidity API

## IProtocolConfig

Interface for the global protocol configuration contract

### FeeOutOfRange

```solidity
error FeeOutOfRange()
```

### ZeroAddress

```solidity
error ZeroAddress()
```

### ConfigUpdated

```solidity
event ConfigUpdated(bytes32 key, uint256 oldValue, uint256 newValue)
```

### usdlAddress

```solidity
function usdlAddress() external view returns (address)
```

### virtualUsdlDefault

```solidity
function virtualUsdlDefault() external view returns (uint256)
```

### virtualTokenDefault

```solidity
function virtualTokenDefault() external view returns (uint256)
```

### minFeeBps

```solidity
function minFeeBps() external view returns (uint256)
```

### maxFeeBps

```solidity
function maxFeeBps() external view returns (uint256)
```

### baseFeeBps

```solidity
function baseFeeBps() external view returns (uint256)
```

### protocolFeeBps

```solidity
function protocolFeeBps() external view returns (uint256)
```

### feeDecayRate

```solidity
function feeDecayRate() external view returns (uint256)
```

### volatilityWeight

```solidity
function volatilityWeight() external view returns (uint256)
```

### concentrationWeight

```solidity
function concentrationWeight() external view returns (uint256)
```

### creationFee

```solidity
function creationFee() external view returns (uint256)
```

### setBaseFeeBps

```solidity
function setBaseFeeBps(uint256 newBaseFeeBps) external
```

### setProtocolFeeBps

```solidity
function setProtocolFeeBps(uint256 newProtocolFeeBps) external
```

### setCreationFee

```solidity
function setCreationFee(uint256 newCreationFee) external
```

### setFeeWeights

```solidity
function setFeeWeights(uint256 newDecayRate, uint256 newVolWeight, uint256 newConcWeight) external
```

### setVirtualDefaults

```solidity
function setVirtualDefaults(uint256 newVirtualUsdl, uint256 newVirtualToken) external
```

