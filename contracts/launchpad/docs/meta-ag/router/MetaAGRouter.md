# Solidity API

## MetaAGRouter

Polls every registered `IProtocolAdapter` for the best output, executes
        the winning quote, oracle-sanity-checks the result, and supports
        multi-hop routing across heterogeneous adapters (Vault + Sidiora + ...).

_Spec reference: `docs/architecture/pecor-sidiora-aggregator-spec.md` §7.10
     (FROZEN 2026-04-24). Interface:
     `contracts/meta-ag/interfaces/IMetaAGRouter.sol`. Port of
     `dev/PECORRouter.sol` with the following frozen-surface divergences:
       - Ownable to AccessControl (DEFAULT_ADMIN_ROLE held by Timelock, S1).
       - OpenZeppelin SafeERC20.forceApprove replaced with in-house
         `TransferHelper.safeApprove(token, spender, 0)` then `safeApprove
         (token, spender, amount)`: the S9 zero-first reset pattern. Final
         cleanup resets to zero after the adapter call (defense-in-depth).
       - OpenZeppelin Math.mulDiv replaced with `SidioraMath.mulDiv`.
       - Require-string reverts replaced with custom errors registered
         under `ERRORS.router.*` in the test helper.

Inheritance (spec §7.10):
  IMetaAGRouter, Initializable, UUPSUpgradeable, AccessControl,
  ReentrancyGuard, Pausable, Multicall

Roles:
  - DEFAULT_ADMIN_ROLE → Timelock at deploy (invariant S1)

Storage layout (append-only per S12):
  slot 0:  AccessControl._roles          (mapping)
  slot 1:  _adapterList                  (AdapterEntry[])
  slot 2:  _adapterById                  (mapping: adapterId => address)
  slot 3:  _adapterAddresses             (mapping: address => bool dedupe)
  slot 4:  oracleHub                     (address stored as IOracleHub)
  slot 5:  maxOracleSanityDeviation      (uint256)
  slot 6:  oracleSanityEnabled           (bool)
  slot 7..56: __gap[50]

Invariants enforced by this contract:
  - S1  — UUPS `_authorizeUpgrade` gated on DEFAULT_ADMIN_ROLE (Timelock).
  - S3  — `swapMultiHop` re-queries `getQuote` with the actual intermediate
          amount before each hop; slippage guard fires on per-hop output.
  - S4  — `_oracleSanityCheck` skips when either price is unavailable and
          reverts when deviation exceeds `maxOracleSanityDeviation`.
  - S9  — `TransferHelper.safeApprove(token, adapter, 0)` fires before AND
          after every adapter call (no dangling allowances).
  - S12 — `__gap[50]` at the tail._

### BPS_DENOMINATOR

```solidity
uint256 BPS_DENOMINATOR
```

### MAX_ADAPTERS

```solidity
uint256 MAX_ADAPTERS
```

### MAX_HOPS

```solidity
uint256 MAX_HOPS
```

### ZeroAddress

```solidity
error ZeroAddress()
```

### ZeroAmount

```solidity
error ZeroAmount()
```

### SameToken

```solidity
error SameToken()
```

### DeadlineExpired

```solidity
error DeadlineExpired()
```

### InvalidBps

```solidity
error InvalidBps()
```

### MaxAdaptersReached

```solidity
error MaxAdaptersReached()
```

### AdapterAlreadyRegistered

```solidity
error AdapterAlreadyRegistered()
```

### AdapterNotFound

```solidity
error AdapterNotFound()
```

### AdapterInactive

```solidity
error AdapterInactive()
```

### NoAdaptersAvailable

```solidity
error NoAdaptersAvailable()
```

### BestQuoteUnavailable

```solidity
error BestQuoteUnavailable()
```

### QuoteUnavailable

```solidity
error QuoteUnavailable()
```

### SlippageTooHigh

```solidity
error SlippageTooHigh()
```

### MaxHopsExceeded

```solidity
error MaxHopsExceeded()
```

### TooFewHops

```solidity
error TooFewHops()
```

### OracleSanityFailed

```solidity
error OracleSanityFailed()
```

### oracleHub

```solidity
address oracleHub
```

OracleHub consulted by the sanity check (spec §7.10 / S4).

_Stored as `address` to satisfy the frozen interface return type
     (`IMetaAGRouter.oracleHub()` returns `address`). Cast to
     {IOracleHub} inside {_oracleSanityCheck}._

### maxOracleSanityDeviation

```solidity
uint256 maxOracleSanityDeviation
```

Max deviation (BPS) before `_oracleSanityCheck` reverts.

### oracleSanityEnabled

```solidity
bool oracleSanityEnabled
```

Master toggle for the oracle sanity check.

### ensure

```solidity
modifier ensure(uint256 deadline)
```

### constructor

```solidity
constructor() public
```

### initialize

```solidity
function initialize(address oracleHub_, uint256 maxSanityDeviation, address admin) external
```

Initialize the UUPS proxy (spec §7.10)

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| oracleHub_ | address |  |
| maxSanityDeviation | uint256 | Max deviation (BPS) before sanity revert (spec Q5 = 500) |
| admin | address | Timelock — granted DEFAULT_ADMIN_ROLE |

### _authorizeUpgrade

```solidity
function _authorizeUpgrade(address) internal
```

### pause

```solidity
function pause() external
```

### unpause

```solidity
function unpause() external
```

### setOracleHub

```solidity
function setOracleHub(address hub) external
```

### setOracleSanityDeviation

```solidity
function setOracleSanityDeviation(uint256 bps) external
```

### setOracleSanityEnabled

```solidity
function setOracleSanityEnabled(bool enabled) external
```

### registerAdapter

```solidity
function registerAdapter(address adapter) external
```

Register a new adapter (reads adapter.adapterId() for collision check).
        Caller must have DEFAULT_ADMIN_ROLE (Timelock).

### deactivateAdapter

```solidity
function deactivateAdapter(bytes32 adapterId_) external
```

### activateAdapter

```solidity
function activateAdapter(bytes32 adapterId_) external
```

### getBestQuote

```solidity
function getBestQuote(address tokenIn, address tokenOut, uint256 amountIn) public view returns (struct IMetaAGRouter.BestQuote best)
```

Best quote across all active adapters (no side effects).

### getAllQuotes

```solidity
function getAllQuotes(address tokenIn, address tokenOut, uint256 amountIn) external view returns (struct IProtocolAdapter.QuoteResult[] quotes, bytes32[] adapterIds, string[] names)
```

Every adapter's quote for a pair. Parallel arrays.

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| quotes | struct IProtocolAdapter.QuoteResult[] | Array of IProtocolAdapter.QuoteResult |
| adapterIds | bytes32[] | Array of adapter IDs |
| names | string[] | Array of adapter names |

### swapBestRoute

```solidity
function swapBestRoute(address tokenIn, address tokenOut, uint256 amountIn, uint256 amountOutMin, uint256 deadline) external returns (uint256 amountOut)
```

Swap using the best available adapter (highest amountOut).

### swapViaAdapter

```solidity
function swapViaAdapter(bytes32 adapterId_, address tokenIn, address tokenOut, uint256 amountIn, uint256 amountOutMin, uint256 deadline) external returns (uint256 amountOut)
```

Swap via a specific adapter (user-specified, bypasses best-route selection).

### swapMultiHop

```solidity
function swapMultiHop(struct IMetaAGRouter.HopParams[] hops, uint256 amountIn, uint256 amountOutMin, uint256 deadline) external returns (uint256 amountOut)
```

Execute a multi-hop swap across multiple adapters. Re-queries each hop.

### getAdapters

```solidity
function getAdapters() external view returns (struct IMetaAGRouter.AdapterEntry[])
```

### getAdapter

```solidity
function getAdapter(bytes32 adapterId_) external view returns (struct IMetaAGRouter.AdapterEntry)
```

### adapterCount

```solidity
function adapterCount() external view returns (uint256)
```

### isAdapterActive

```solidity
function isAdapterActive(bytes32 adapterId_) external view returns (bool)
```

### _executeAdapterSwap

```solidity
function _executeAdapterSwap(address adapter, address tokenIn, address tokenOut, uint256 amountIn, uint256 amountOutMin, address payer, address recipient, uint256 deadline, bytes adapterData) internal returns (uint256 amountOut)
```

Runs the S9 approval dance around a single `executeSwap` leg.

_Transfers `amountIn` of `tokenIn` from `payer` into this router,
     approves the adapter with zero-first reset, executes, then resets
     approval back to zero. Slippage is enforced against `amountOutMin`._

### _oracleSanityCheck

```solidity
function _oracleSanityCheck(address tokenIn, address tokenOut, uint256 amountIn, uint256 amountOut) internal view
```

Reverts if the realized `amountOut` deviates from oracle-implied
        expectation by more than `maxOracleSanityDeviation` bps.

_Port of `dev/PECORRouter._oracleSanityCheck` with the dev's intent
     preserved: ignores decimals for the rough sanity check (expected
     value is approximate and only used to bound catastrophic deviations).
     Skips silently when either oracle price is unavailable._

### _findAdapterIndex

```solidity
function _findAdapterIndex(bytes32 adapterId_) internal view returns (uint256)
```

_Finds the index of an adapter by ID. Reverts `AdapterNotFound` if
     no entry matches — cheaper than a separate existence map because
     the list is capped at `MAX_ADAPTERS = 20`._

