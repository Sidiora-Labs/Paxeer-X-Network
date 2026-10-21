# Solidity API

## MockEventEmitter

Simplified mock EventEmitter for testing contracts that emit events through it

### lastConfigKey

```solidity
bytes32 lastConfigKey
```

### lastConfigOldValue

```solidity
uint256 lastConfigOldValue
```

### lastConfigNewValue

```solidity
uint256 lastConfigNewValue
```

### configUpdateCount

```solidity
uint256 configUpdateCount
```

### authorizedEmitters

```solidity
mapping(address => bool) authorizedEmitters
```

### ConfigUpdated

```solidity
event ConfigUpdated(bytes32 key, uint256 oldValue, uint256 newValue, uint256 timestamp, uint256 blockNumber)
```

### MarketCreated

```solidity
event MarketCreated(bytes32 poolId, address token, address creator, address pool, address optical, uint256 timestamp, uint256 blockNumber)
```

### Swap

```solidity
event Swap(bytes32 poolId, address sender, bool isBuy, uint256 amountIn, uint256 amountOut, uint256 fee, uint256 price, uint256 timestamp, uint256 blockNumber)
```

### FeeRecorded

```solidity
event FeeRecorded(bytes32 poolId, uint256 feeAmount, uint256 protocolCut, uint256 poolCut, uint256 timestamp, uint256 blockNumber)
```

### FeeDistributed

```solidity
event FeeDistributed(bytes32 poolId, uint256 nftId, uint8 strategy, uint256 amount, address recipient, uint256 timestamp, uint256 blockNumber)
```

### FeeStrategyChanged

```solidity
event FeeStrategyChanged(bytes32 poolId, uint256 nftId, uint8 oldStrategy, uint8 newStrategy, uint256 timestamp, uint256 blockNumber)
```

### PoolStateUpdated

```solidity
event PoolStateUpdated(bytes32 poolId, uint256 virtualReserve, uint256 realReserve, uint256 tokenReserve, uint256 price, uint256 timestamp, uint256 blockNumber)
```

### setAuthorizedEmitter

```solidity
function setAuthorizedEmitter(address emitter, bool authorized) external
```

### isAuthorizedEmitter

```solidity
function isAuthorizedEmitter(address emitter) external view returns (bool)
```

### emitConfigUpdated

```solidity
function emitConfigUpdated(bytes32 key, uint256 oldValue, uint256 newValue) external
```

### emitMarketCreated

```solidity
function emitMarketCreated(bytes32 poolId, address token, address creator, address pool, address optical) external
```

### emitSwap

```solidity
function emitSwap(bytes32 poolId, address sender, bool isBuy, uint256 amountIn, uint256 amountOut, uint256 fee, uint256 price) external
```

### emitFeeRecorded

```solidity
function emitFeeRecorded(bytes32 poolId, uint256 feeAmount, uint256 protocolCut, uint256 poolCut) external
```

### emitFeeDistributed

```solidity
function emitFeeDistributed(bytes32 poolId, uint256 nftId, uint8 strategy, uint256 amount, address recipient) external
```

### emitFeeStrategyChanged

```solidity
function emitFeeStrategyChanged(bytes32 poolId, uint256 nftId, uint8 oldStrategy, uint8 newStrategy) external
```

### emitPoolStateUpdated

```solidity
function emitPoolStateUpdated(bytes32 poolId, uint256 virtualReserve, uint256 realReserve, uint256 tokenReserve, uint256 price) external
```

