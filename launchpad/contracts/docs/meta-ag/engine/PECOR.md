# Solidity API

## PECOR

UUPS-upgradeable oracle-priced swap engine against PECORVault v2.
        Handles simple swaps, market orders, and native-coin swaps.
        Order management (limit / stop-loss / stop-limit) lives in
        PECOROrders.sol (Task 4.2).

_Spec reference: `docs/architecture/pecor-sidiora-aggregator-spec.md` §7.6
     (FROZEN 2026-04-24). Interface: `contracts/meta-ag/interfaces/IPECOR.sol`.

Inheritance (spec §7.6):
  IPECOR, Initializable, UUPSUpgradeable, AccessControl,
  ReentrancyGuard, Pausable

Roles:
  - DEFAULT_ADMIN_ROLE → Timelock at deploy (invariant S1)
  - FEE_COLLECTOR_ROLE → granted to `feeCollector` via {setFeeCollector}

Storage layout (append-only per S12):
  slot 0:  AccessControl._roles             (mapping)
  slot 1:  priceOracle                      (address)
  slot 2:  vault                            (address)
  slot 3:  transactionTracker               (address)
  slot 4:  weth                             (address)
  slot 5:  swapFeeBps                       (uint256)
  slot 6:  tier1FeeBps                      (uint256)
  slot 7:  tier2FeeBps                      (uint256)
  slot 8:  priceImpactEnabled               (bool)
  slot 9:  priceImpactScalarBps             (uint256)
  slot 10: feeCollector                     (address)
  slot 11: accruedFees                      (mapping)
  slot 12..61: __gap[50]_

### FEE_COLLECTOR_ROLE

```solidity
bytes32 FEE_COLLECTOR_ROLE
```

### PRECISION

```solidity
uint256 PRECISION
```

### BPS_DENOMINATOR

```solidity
uint256 BPS_DENOMINATOR
```

### MAX_FEE_BPS

```solidity
uint256 MAX_FEE_BPS
```

Absolute cap for fee stacking — swapFeeBps + tier1FeeBps + tier2FeeBps
        must never exceed this (spec invariant S11).

### MAX_IMPACT_BPS

```solidity
uint256 MAX_IMPACT_BPS
```

Maximum price impact applied to any single swap.

### TIER1_THRESHOLD

```solidity
uint256 TIER1_THRESHOLD
```

Swap volume tier thresholds (USD, 18-decimals fixed-point).

### TIER2_THRESHOLD

```solidity
uint256 TIER2_THRESHOLD
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

### Expired

```solidity
error Expired()
```

### InsufficientOutput

```solidity
error InsufficientOutput()
```

### ExcessiveInput

```solidity
error ExcessiveInput()
```

### InsufficientLiquidity

```solidity
error InsufficientLiquidity()
```

### NotAStablecoin

```solidity
error NotAStablecoin()
```

### TokenIsStablecoin

```solidity
error TokenIsStablecoin()
```

### UseWethDeposit

```solidity
error UseWethDeposit()
```

### UseWethWithdraw

```solidity
error UseWethWithdraw()
```

### NoFeesToCollect

```solidity
error NoFeesToCollect()
```

### FeeTooHigh

```solidity
error FeeTooHigh()
```

### Tier2BelowTier1

```solidity
error Tier2BelowTier1()
```

### ScalarTooHigh

```solidity
error ScalarTooHigh()
```

### NativeTransferFailed

```solidity
error NativeTransferFailed()
```

### MulticallFailed

```solidity
error MulticallFailed(uint256 index)
```

### priceOracle

```solidity
contract IPriceOracle priceOracle
```

### vault

```solidity
contract IPECORVault vault
```

### transactionTracker

```solidity
contract ITransactionTracker transactionTracker
```

### weth

```solidity
address weth
```

Wrapped native coin. Assigned once in {initialize}.

### swapFeeBps

```solidity
uint256 swapFeeBps
```

Base protocol fee in BPS, applied to every swap.

### tier1FeeBps

```solidity
uint256 tier1FeeBps
```

Additional fee in BPS for volumeUSD >= TIER1_THRESHOLD.

### tier2FeeBps

```solidity
uint256 tier2FeeBps
```

Additional fee in BPS for volumeUSD >= TIER2_THRESHOLD.

### priceImpactEnabled

```solidity
bool priceImpactEnabled
```

Toggle for price-impact deduction on net swap output.

### priceImpactScalarBps

```solidity
uint256 priceImpactScalarBps
```

Scalar used to scale impact by swap-to-reserve ratio.

### feeCollector

```solidity
address feeCollector
```

Destination for {collectFees}. Also holds FEE_COLLECTOR_ROLE.

### accruedFees

```solidity
mapping(address => uint256) accruedFees
```

Per-token accrued fees (tracked here; actual tokens live in
        the vault and are moved via vault.pushTokens at collect time).

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
function initialize(address priceOracle_, address vault_, address weth_, address tracker_, address admin_) external
```

Initialize the UUPS proxy (spec §7.6)

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| priceOracle_ | address |  |
| vault_ | address |  |
| weth_ | address |  |
| tracker_ | address |  |
| admin_ | address |  |

### _authorizeUpgrade

```solidity
function _authorizeUpgrade(address) internal
```

### setPriceOracle

```solidity
function setPriceOracle(address oracle) external
```

### setTransactionTracker

```solidity
function setTransactionTracker(address tracker) external
```

### setSwapFee

```solidity
function setSwapFee(uint256 feeBps) external
```

_S11: swapFeeBps + tier1FeeBps + tier2FeeBps ≤ MAX_FEE_BPS._

### setTieredFees

```solidity
function setTieredFees(uint256 tier1FeeBps_, uint256 tier2FeeBps_) external
```

_S11: swapFeeBps + tier1FeeBps + tier2FeeBps ≤ MAX_FEE_BPS.
     Also enforces tier2FeeBps ≥ tier1FeeBps (monotonic escalation)._

### setPriceImpact

```solidity
function setPriceImpact(bool enabled, uint256 scalarBps) external
```

### setFeeCollector

```solidity
function setFeeCollector(address collector) external
```

_Rotates FEE_COLLECTOR_ROLE from the previous collector to the new one._

### pause

```solidity
function pause() external
```

### unpause

```solidity
function unpause() external
```

### collectFees

```solidity
function collectFees(address token) external
```

_Moves accrued fees out of the vault into {feeCollector}.
     The vault treats PECOR as an OPERATOR_ROLE holder, so this
     succeeds after vault.setOperator(PECOR, true)._

### swapExactIn

```solidity
function swapExactIn(address tokenIn, address tokenOut, uint256 amountIn, uint256 amountOutMin, uint256 deadline) external returns (uint256 amountOut)
```

### swapExactOut

```solidity
function swapExactOut(address tokenIn, address tokenOut, uint256 amountOut, uint256 amountInMax, uint256 deadline) external returns (uint256 amountIn)
```

### marketBuy

```solidity
function marketBuy(address stablecoin, address token, uint256 stablecoinAmount, uint256 minTokenAmount, uint256 deadline) external returns (uint256 tokenAmount)
```

### marketSell

```solidity
function marketSell(address token, address stablecoin, uint256 tokenAmount, uint256 minStablecoinAmount, uint256 deadline) external returns (uint256 stablecoinAmount)
```

### swapExactInNative

```solidity
function swapExactInNative(address tokenOut, uint256 amountOutMin, uint256 deadline) external payable returns (uint256 amountOut)
```

### swapExactInToNative

```solidity
function swapExactInToNative(address tokenIn, uint256 amountIn, uint256 amountOutMin, uint256 deadline) external returns (uint256 amountOut)
```

### getQuoteExactIn

```solidity
function getQuoteExactIn(address tokenIn, address tokenOut, uint256 amountIn) external view returns (uint256 amountOut)
```

### getQuoteExactOut

```solidity
function getQuoteExactOut(address tokenIn, address tokenOut, uint256 amountOut) external view returns (uint256 amountIn)
```

### getDetailedQuote

```solidity
function getDetailedQuote(address tokenIn, address tokenOut, uint256 amountIn) external view returns (uint256 grossOut, uint256 netOut, uint256 priceImpactBps, uint256 feeBps, uint256 feeAmount)
```

Full oracle-priced quote with price impact and fee breakdown.

#### Return Values

| Name | Type | Description |
| ---- | ---- | ----------- |
| grossOut | uint256 | Raw amountOut before fee / impact. |
| netOut | uint256 | amountOut user actually receives. |
| priceImpactBps | uint256 | Price impact applied (BPS). |
| feeBps | uint256 | Effective fee rate after tiered stacking (BPS). |
| feeAmount | uint256 | Fee deducted from grossOut. |

### multicall

```solidity
function multicall(bytes[] data) external returns (bytes[] results)
```

Batch multiple calls atomically via delegatecall (spec §7.6).

_Bubbles up the first failing call's revert data verbatim so that
     custom errors reach the outer caller. Matches the ERC-1967 proxy
     hardening landed in Phase 3 commit 18._

### receive

```solidity
receive() external payable
```

Accept native transfers only from WETH unwraps initiated by PECOR.

### _calculateSwapOutput

```solidity
function _calculateSwapOutput(address tokenIn, address tokenOut, uint256 amountIn, uint256 priceIn, uint256 priceOut) internal view returns (uint256)
```

_Compute gross output respecting token decimals and oracle prices._

### _calculateSwapInput

```solidity
function _calculateSwapInput(address tokenIn, address tokenOut, uint256 amountOut, uint256 priceIn, uint256 priceOut) internal view returns (uint256)
```

_Compute required input for a target output, rounding up._

### _applyFeeAndImpact

```solidity
function _applyFeeAndImpact(address token, uint256 grossAmount, uint256 volumeUSD, uint256 reserveUSD) internal returns (uint256 netAmount)
```

_Apply tiered fee + price impact, credit accrued fees._

### _calcNetOutput

```solidity
function _calcNetOutput(uint256 grossAmount, uint256 volumeUSD, uint256 reserveUSD) internal view returns (uint256 netAmount)
```

_Pure view form of {_applyFeeAndImpact} — no state or events._

### _getEffectiveFeeBps

```solidity
function _getEffectiveFeeBps(uint256 volumeUSD) internal view returns (uint256)
```

_Tiered fee resolution per spec §7.6 / invariant S11._

### _recordTrade

```solidity
function _recordTrade(address user, address tokenIn, address tokenOut, uint256 amountIn, uint256 amountOut, uint256 volumeUSD) internal
```

_Gated call to the transaction tracker; silently skipped if unset.
     When set, the tracker enforces EMITTER_ROLE on its side (S10)._

### _recordMarketTrade

```solidity
function _recordMarketTrade(address user, address stablecoin, address token, uint256 stablecoinAmount, uint256 tokenAmount, bool isBuy, uint256 executionPrice) internal
```

_Gated market-order record; silently skipped if tracker unset._

