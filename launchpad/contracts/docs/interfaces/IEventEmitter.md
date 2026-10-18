# Solidity API

## IEventEmitter

Interface for the central event hub contract

### Unauthorized

```solidity
error Unauthorized()
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

### OpticalExecuted

```solidity
event OpticalExecuted(bytes32 poolId, address optical, string hookName, bytes data, uint256 timestamp, uint256 blockNumber)
```

### PoolStateUpdated

```solidity
event PoolStateUpdated(bytes32 poolId, uint256 virtualReserve, uint256 realReserve, uint256 tokenReserve, uint256 price, uint256 timestamp, uint256 blockNumber)
```

### ConfigUpdated

```solidity
event ConfigUpdated(bytes32 key, uint256 oldValue, uint256 newValue, uint256 timestamp, uint256 blockNumber)
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

### emitOpticalExecuted

```solidity
function emitOpticalExecuted(bytes32 poolId, address optical, string hookName, bytes data) external
```

### emitConfigUpdated

```solidity
function emitConfigUpdated(bytes32 key, uint256 oldValue, uint256 newValue) external
```

### isAuthorizedEmitter

```solidity
function isAuthorizedEmitter(address emitter) external view returns (bool)
```

### setAuthorizedEmitter

```solidity
function setAuthorizedEmitter(address emitter, bool authorized) external
```

### AddressKeyValue

```solidity
struct AddressKeyValue {
  string key;
  address value;
}
```

### AddressArrayKeyValue

```solidity
struct AddressArrayKeyValue {
  string key;
  address[] value;
}
```

### UintKeyValue

```solidity
struct UintKeyValue {
  string key;
  uint256 value;
}
```

### UintArrayKeyValue

```solidity
struct UintArrayKeyValue {
  string key;
  uint256[] value;
}
```

### IntKeyValue

```solidity
struct IntKeyValue {
  string key;
  int256 value;
}
```

### IntArrayKeyValue

```solidity
struct IntArrayKeyValue {
  string key;
  int256[] value;
}
```

### BoolKeyValue

```solidity
struct BoolKeyValue {
  string key;
  bool value;
}
```

### BoolArrayKeyValue

```solidity
struct BoolArrayKeyValue {
  string key;
  bool[] value;
}
```

### Bytes32KeyValue

```solidity
struct Bytes32KeyValue {
  string key;
  bytes32 value;
}
```

### Bytes32ArrayKeyValue

```solidity
struct Bytes32ArrayKeyValue {
  string key;
  bytes32[] value;
}
```

### BytesKeyValue

```solidity
struct BytesKeyValue {
  string key;
  bytes value;
}
```

### BytesArrayKeyValue

```solidity
struct BytesArrayKeyValue {
  string key;
  bytes[] value;
}
```

### StringKeyValue

```solidity
struct StringKeyValue {
  string key;
  string value;
}
```

### StringArrayKeyValue

```solidity
struct StringArrayKeyValue {
  string key;
  string[] value;
}
```

### AddressItems

```solidity
struct AddressItems {
  struct IEventEmitter.AddressKeyValue[] items;
  struct IEventEmitter.AddressArrayKeyValue[] arrayItems;
}
```

### UintItems

```solidity
struct UintItems {
  struct IEventEmitter.UintKeyValue[] items;
  struct IEventEmitter.UintArrayKeyValue[] arrayItems;
}
```

### IntItems

```solidity
struct IntItems {
  struct IEventEmitter.IntKeyValue[] items;
  struct IEventEmitter.IntArrayKeyValue[] arrayItems;
}
```

### BoolItems

```solidity
struct BoolItems {
  struct IEventEmitter.BoolKeyValue[] items;
  struct IEventEmitter.BoolArrayKeyValue[] arrayItems;
}
```

### Bytes32Items

```solidity
struct Bytes32Items {
  struct IEventEmitter.Bytes32KeyValue[] items;
  struct IEventEmitter.Bytes32ArrayKeyValue[] arrayItems;
}
```

### BytesItems

```solidity
struct BytesItems {
  struct IEventEmitter.BytesKeyValue[] items;
  struct IEventEmitter.BytesArrayKeyValue[] arrayItems;
}
```

### StringItems

```solidity
struct StringItems {
  struct IEventEmitter.StringKeyValue[] items;
  struct IEventEmitter.StringArrayKeyValue[] arrayItems;
}
```

### EventData

```solidity
struct EventData {
  struct IEventEmitter.AddressItems addressItems;
  struct IEventEmitter.UintItems uintItems;
  struct IEventEmitter.IntItems intItems;
  struct IEventEmitter.BoolItems boolItems;
  struct IEventEmitter.Bytes32Items bytes32Items;
  struct IEventEmitter.BytesItems bytesItems;
  struct IEventEmitter.StringItems stringItems;
}
```

### EventLog

```solidity
event EventLog(address msgSender, bytes32 eventNameHash, string eventName, struct IEventEmitter.EventData eventData, uint256 timestamp, uint256 blockNumber)
```

Generic schemaless event with no extra topics beyond eventNameHash.

_`msgSender` records the actual caller (real provenance).
     `eventNameHash = keccak256(bytes(eventName))` — indexed so off-chain
     consumers filter on topic1 without decoding the string body._

### EventLog1

```solidity
event EventLog1(address msgSender, bytes32 eventNameHash, bytes32 topic1, string eventName, struct IEventEmitter.EventData eventData, uint256 timestamp, uint256 blockNumber)
```

Generic schemaless event with one extra topic (eg. user / pool / token).

### EventLog2

```solidity
event EventLog2(address msgSender, bytes32 eventNameHash, bytes32 topic1, bytes32 topic2, string eventName, struct IEventEmitter.EventData eventData, uint256 timestamp, uint256 blockNumber)
```

Generic schemaless event with two extra topics (eg. user + token, pool + nft).

### emitEventLog

```solidity
function emitEventLog(string eventName, struct IEventEmitter.EventData data) external
```

### emitEventLog1

```solidity
function emitEventLog1(string eventName, bytes32 topic1, struct IEventEmitter.EventData data) external
```

### emitEventLog2

```solidity
function emitEventLog2(string eventName, bytes32 topic1, bytes32 topic2, struct IEventEmitter.EventData data) external
```

### PecorSwap

```solidity
event PecorSwap(address user, address tokenIn, address tokenOut, uint256 amountIn, uint256 amountOut, uint256 priceIn, uint256 priceOut, uint256 volumeUSD, uint256 feeBps, uint256 feeAmount, uint256 impactBps, uint8 swapKind, uint256 timestamp, uint256 blockNumber)
```

### PecorOrderCreated

```solidity
event PecorOrderCreated(uint256 orderId, address user, uint8 orderKind, uint8 orderType, address tokenIn, address tokenOut, uint256 amount, uint256 targetPrice, uint256 stopPrice, uint256 limitPrice, uint256 timestamp, uint256 blockNumber)
```

### PecorOrderLifecycle

```solidity
event PecorOrderLifecycle(uint256 orderId, address user, uint8 orderKind, uint8 phase, uint256 price, bytes payload, uint256 timestamp, uint256 blockNumber)
```

### BestRouteSwap

```solidity
event BestRouteSwap(address user, address tokenIn, address tokenOut, bytes32 routeId, uint256 amountIn, uint256 amountOut, address[] hops, uint256 protocolFeeBps, uint256 timestamp, uint256 blockNumber)
```

### emitPecorSwap

```solidity
function emitPecorSwap(address user, address tokenIn, address tokenOut, uint256 amountIn, uint256 amountOut, uint256 priceIn, uint256 priceOut, uint256 volumeUSD, uint256 feeBps, uint256 feeAmount, uint256 impactBps, uint8 swapKind) external
```

### emitPecorOrderCreated

```solidity
function emitPecorOrderCreated(uint256 orderId, address user, uint8 orderKind, uint8 orderType, address tokenIn, address tokenOut, uint256 amount, uint256 targetPrice, uint256 stopPrice, uint256 limitPrice) external
```

### emitPecorOrderLifecycle

```solidity
function emitPecorOrderLifecycle(uint256 orderId, address user, uint8 orderKind, uint8 phase, uint256 price, bytes payload) external
```

### emitBestRouteSwap

```solidity
function emitBestRouteSwap(address user, address tokenIn, address tokenOut, bytes32 routeId, uint256 amountIn, uint256 amountOut, address[] hops, uint256 protocolFeeBps) external
```

### PriceUpdated

```solidity
event PriceUpdated(address token, uint256 roundId, address relayer, uint256 price, uint256 confidence, bytes32 sourceId, uint256 timestamp, uint256 blockNumber)
```

### CircuitBreakerTriggered

```solidity
event CircuitBreakerTriggered(address token, bytes32 sourceId, uint256 reportedPrice, uint256 referencePrice, uint256 deviationBps, uint256 timestamp, uint256 blockNumber)
```

### OracleAdapterLifecycle

```solidity
event OracleAdapterLifecycle(bytes32 sourceId, address adapter, uint8 phase, uint256 priority, uint256 timestamp, uint256 blockNumber)
```

### emitPriceUpdated

```solidity
function emitPriceUpdated(address token, uint256 roundId, address relayer, uint256 price, uint256 confidence, bytes32 sourceId) external
```

### emitCircuitBreaker

```solidity
function emitCircuitBreaker(address token, bytes32 sourceId, uint256 reportedPrice, uint256 referencePrice, uint256 deviationBps) external
```

### emitOracleAdapterLifecycle

```solidity
function emitOracleAdapterLifecycle(bytes32 sourceId, address adapter, uint8 phase, uint256 priority) external
```

### VaultFlow

```solidity
event VaultFlow(uint8 flowType, address token, address party, uint256 amount, uint256 newReserve, uint256 timestamp, uint256 blockNumber)
```

### emitVaultFlow

```solidity
function emitVaultFlow(uint8 flowType, address token, address party, uint256 amount, uint256 newReserve) external
```

### Governance

```solidity
event Governance(uint8 action, bytes32 id, address actor, bytes payload, uint256 timestamp, uint256 blockNumber)
```

### emitGovernance

```solidity
function emitGovernance(uint8 action, bytes32 id, address actor, bytes payload) external
```

### TreasuryFlow

```solidity
event TreasuryFlow(uint8 direction, address token, address party, uint256 amount, uint256 timestamp, uint256 blockNumber)
```

### emitTreasuryFlow

```solidity
function emitTreasuryFlow(uint8 direction, address token, address party, uint256 amount) external
```

### OpticalLifecycle

```solidity
event OpticalLifecycle(uint8 action, address optical, address pool, bytes32 name, bytes payload, uint256 timestamp, uint256 blockNumber)
```

### emitOpticalLifecycle

```solidity
function emitOpticalLifecycle(uint8 action, address optical, address pool, bytes32 name, bytes payload) external
```

### TokenTransfer

```solidity
event TokenTransfer(address token, address from, address to, uint256 value, uint256 timestamp, uint256 blockNumber)
```

### NftTransfer

```solidity
event NftTransfer(address nft, address from, address to, uint256 tokenId, uint256 timestamp, uint256 blockNumber)
```

### AssetApproval

```solidity
event AssetApproval(address asset, address owner, address spender, uint256 valueOrTokenId, bool isNft, uint256 timestamp, uint256 blockNumber)
```

### emitTokenTransfer

```solidity
function emitTokenTransfer(address token, address from, address to, uint256 value) external
```

### emitNftTransfer

```solidity
function emitNftTransfer(address nft, address from, address to, uint256 tokenId) external
```

### emitAssetApproval

```solidity
function emitAssetApproval(address asset, address owner, address spender, uint256 valueOrTokenId, bool isNft) external
```

### RoleChange

```solidity
event RoleChange(uint8 action, bytes32 role, address account, address sender, bytes32 previousAdminRole, uint256 timestamp, uint256 blockNumber)
```

### ContractUpgraded

```solidity
event ContractUpgraded(address proxy, address newImplementation, uint8 kind, uint256 timestamp, uint256 blockNumber)
```

### PauseToggle

```solidity
event PauseToggle(address pausedContract, bool paused, uint256 timestamp, uint256 blockNumber)
```

### emitRoleChange

```solidity
function emitRoleChange(uint8 action, bytes32 role, address account, address sender, bytes32 previousAdminRole) external
```

### emitUpgraded

```solidity
function emitUpgraded(address proxy, address newImplementation, uint8 kind) external
```

### emitPauseToggle

```solidity
function emitPauseToggle(address pausedContract, bool paused) external
```

### FeeFlow

```solidity
event FeeFlow(uint8 kind, address pool, address party, uint256 amount, uint256 protocolCut, uint256 poolCut, uint256 epoch, uint256 timestamp, uint256 blockNumber)
```

Mirror of FeeAccumulator's full event surface, collapsed
        into a single typed channel for indexer simplicity.

### RouterTrade

```solidity
event RouterTrade(uint8 kind, address pool, address sender, address tokenIn, address tokenOut, uint256 amountIn, uint256 amountOut, uint256 intermediateUsdl, uint256 timestamp, uint256 blockNumber)
```

### NftMint

```solidity
event NftMint(uint256 tokenId, address creator, address pool, uint8 strategy, uint256 timestamp, uint256 blockNumber)
```

### PoolRegistered

```solidity
event PoolRegistered(address pool, address token, address creator, address optical, uint256 nftId, uint256 timestamp, uint256 blockNumber)
```

### TokenDeployed

```solidity
event TokenDeployed(address token, address pool, address creator, bytes32 salt, string name, string symbol, uint8 decimals, uint256 totalSupply, uint256 timestamp, uint256 blockNumber)
```

### emitFeeFlow

```solidity
function emitFeeFlow(uint8 kind, address pool, address party, uint256 amount, uint256 protocolCut, uint256 poolCut, uint256 epoch) external
```

### emitRouterTrade

```solidity
function emitRouterTrade(uint8 kind, address pool, address sender, address tokenIn, address tokenOut, uint256 amountIn, uint256 amountOut, uint256 intermediateUsdl) external
```

### emitNftMint

```solidity
function emitNftMint(uint256 tokenId, address creator, address pool, uint8 strategy) external
```

### emitPoolRegistered

```solidity
function emitPoolRegistered(address pool, address token, address creator, address optical, uint256 nftId) external
```

### emitTokenDeployed

```solidity
function emitTokenDeployed(address token, address pool, address creator, bytes32 salt, string name, string symbol, uint8 decimals, uint256 totalSupply) external
```

### VERSION

```solidity
function VERSION() external pure returns (string)
```

Semantic version string of the deployed implementation.

### EVENT_EMITTER_ROLE

```solidity
function EVENT_EMITTER_ROLE() external view returns (bytes32)
```

The role that can authorize emitters and wire registries.
        Distinct from DEFAULT_ADMIN_ROLE (which retains upgrade authority).

### setOpticalRegistry

```solidity
function setOpticalRegistry(address registry) external
```

Wire dynamic auth registries (one-time admin call after v2 upgrade).

### setMetaAGRouter

```solidity
function setMetaAGRouter(address router) external
```

### setSidioraFactory

```solidity
function setSidioraFactory(address factory) external
```

### setTokenRegistry

```solidity
function setTokenRegistry(address registry) external
```

### registerToken

```solidity
function registerToken(address token, address pool) external
```

Per-token registration for the dynamic ERC20 auto-auth path.
        Allows pool-token Transfer mirrors without per-token admin tx.

### deregisterToken

```solidity
function deregisterToken(address token) external
```

### isRegisteredToken

```solidity
function isRegisteredToken(address token) external view returns (bool)
```

### reinitializeV2

```solidity
function reinitializeV2(address adminWithEmitterRole) external
```

One-time storage migration entrypoint for v1 → v2 proxy upgrade.
        Idempotent — safe to call once.

