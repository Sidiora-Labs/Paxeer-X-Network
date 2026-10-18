# Solidity API

## ProtocolConfig

Single source of truth for all global protocol parameters

_UUPS proxy. Only admin can update. Emits config changes via EventEmitter._

### usdlAddress

```solidity
address usdlAddress
```

### virtualUsdlDefault

```solidity
uint256 virtualUsdlDefault
```

### virtualTokenDefault

```solidity
uint256 virtualTokenDefault
```

### minFeeBps

```solidity
uint256 minFeeBps
```

### maxFeeBps

```solidity
uint256 maxFeeBps
```

### baseFeeBps

```solidity
uint256 baseFeeBps
```

### protocolFeeBps

```solidity
uint256 protocolFeeBps
```

### feeDecayRate

```solidity
uint256 feeDecayRate
```

### volatilityWeight

```solidity
uint256 volatilityWeight
```

### concentrationWeight

```solidity
uint256 concentrationWeight
```

### creationFee

```solidity
uint256 creationFee
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
function initialize(address _usdlAddress, address _eventEmitter, address _admin) external
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

### _authorizeUpgrade

```solidity
function _authorizeUpgrade(address) internal
```

