# Solidity API

## IOpticalRegistryMinimal

Minimal interface for the optional optical registry auth path.

### isApproved

```solidity
function isApproved(address optical) external view returns (bool)
```

## IMetaAGRouterMinimal

Minimal interface for the optional Meta-AG router auth path.
        Adapter authorization is keyed by `bytes32 adapterId` rather than
        address; v2 keeps the lookup as `isAdapterActive` for parity with
        the existing MetaAGRouter surface, expecting the router to have
        been registered via `setAuthorizedEmitter` directly.

### isAdapterActive

```solidity
function isAdapterActive(bytes32 adapterId) external view returns (bool)
```

## EventEmitter

Universal protocol event hub. Single source of indexer truth across
        Sidiora Launchpad, Meta-AG/PECOR, opticals, oracle, treasury,
        governance, and base-layer lifecycle (roles, upgrades, pause).

_UUPS-upgradeable. Storage layout is **append-only** vs v1.
        v1 functions and events are preserved verbatim — same selectors,
        same topic0 — for backward compatibility with existing call sites
        in `core/SidioraFactory`, `core/SidioraPool`, `protocol/ProtocolConfig`.

        v2 additions (per `contracts/data/EVENT-EMITTER-V2-PLAN.md`):
          - Generic schemaless `EventLog{,1,2}` with indexed eventNameHash.
          - ~30 typed fast-path emitters spanning every protocol domain.
          - `EVENT_EMITTER_ROLE` separate from `DEFAULT_ADMIN_ROLE`.
          - `VERSION()` accessor returning `"2.0.0"`.
          - 5-path authorization mesh:
              1) static `_authorizedEmitters` mapping
              2) dynamic `IPoolRegistry.isRegisteredPool`        (Launchpad pools)
              3) dynamic `IOpticalRegistry.isApproved`           (opticals)
              4) `sidioraFactory == msg.sender`                  (factory itself)
              5) `_registeredTokens[msg.sender]`                 (pool tokens)

        Reference: GMX V2 EventEmitter pattern, Synthetix V3 EventLog schema._

### EMITTER_ADMIN_ROLE

```solidity
bytes32 EMITTER_ADMIN_ROLE
```

v1 — retained for ABI compatibility. Not used as a gate in v2.

### EVENT_EMITTER_ROLE

```solidity
bytes32 EVENT_EMITTER_ROLE
```

v2 — distinct from DEFAULT_ADMIN_ROLE. Holds permission to
        authorize emitters and wire registries. Does NOT grant
        upgrade authority (DEFAULT_ADMIN_ROLE retains that).

### poolRegistry

```solidity
address poolRegistry
```

_slot 2._

### opticalRegistry

```solidity
address opticalRegistry
```

_slot 3._

### metaAGRouter

```solidity
address metaAGRouter
```

_slot 4._

### sidioraFactory

```solidity
address sidioraFactory
```

_slot 5._

### constructor

```solidity
constructor() public
```

### initialize

```solidity
function initialize(address _admin) external
```

v1 initializer — preserved for fresh-deploy compatibility.
        New deploys SHOULD call this once, then `reinitializeV2`.

### reinitializeV2

```solidity
function reinitializeV2(address adminWithEmitterRole) external
```

v1 → v2 storage migration. Idempotent.

_Callable only by DEFAULT_ADMIN_ROLE. Safe to call once on the
     existing deployed proxy `0x6679aF411d534de222C32ed0AF94C3BD67090672`
     after the impl swap._

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| adminWithEmitterRole | address | Address granted EVENT_EMITTER_ROLE.        Pass `address(0)` to skip role grant (admin can do it later        via `grantRole(EVENT_EMITTER_ROLE, ...)`). |

### _authorizeUpgrade

```solidity
function _authorizeUpgrade(address) internal
```

### onlyAuthorized

```solidity
modifier onlyAuthorized()
```

### isAuthorizedEmitter

```solidity
function isAuthorizedEmitter(address emitter) external view returns (bool)
```

Public auth-check for off-chain probes / dependent contracts.

### isRegisteredToken

```solidity
function isRegisteredToken(address token) external view returns (bool)
```

### _isAuthorized

```solidity
function _isAuthorized(address sender) internal view returns (bool)
```

### setPoolRegistry

```solidity
function setPoolRegistry(address _poolRegistry) external
```

v1 — wire the Launchpad pool registry. Retained.

### setAuthorizedEmitter

```solidity
function setAuthorizedEmitter(address emitter, bool authorized) external
```

v1 — preserved for compatibility. v2 prefers EVENT_EMITTER_ROLE
        (see `setAuthorizedEmitterByRole` below) but DEFAULT_ADMIN_ROLE
        remains a valid caller.

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
function setTokenRegistry(address) external
```

Compatibility shim — the interface declares `setTokenRegistry`
        to allow a future external token registry. v2 uses the local
        `_registeredTokens` mapping, so this currently no-ops the
        external pointer; future versions may delegate.

### registerToken

```solidity
function registerToken(address token, address) external
```

Register a pool-token deployed by the SidioraFactory so it
        can mirror Transfer / Approval through this emitter.

_Callable by the wired factory itself (auto-registration on
     market creation) or by EVENT_EMITTER_ROLE / DEFAULT_ADMIN_ROLE._

### deregisterToken

```solidity
function deregisterToken(address token) external
```

### VERSION

```solidity
function VERSION() external pure returns (string)
```

Semantic version string of the deployed implementation.

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

### emitVaultFlow

```solidity
function emitVaultFlow(uint8 flowType, address token, address party, uint256 amount, uint256 newReserve) external
```

### emitGovernance

```solidity
function emitGovernance(uint8 action, bytes32 id, address actor, bytes payload) external
```

### emitTreasuryFlow

```solidity
function emitTreasuryFlow(uint8 direction, address token, address party, uint256 amount) external
```

### emitOpticalLifecycle

```solidity
function emitOpticalLifecycle(uint8 action, address optical, address pool, bytes32 name, bytes payload) external
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

