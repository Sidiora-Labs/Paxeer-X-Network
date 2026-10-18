# Solidity API

## MetaAGQuoter

Rich quote view over the oracle-priced PECORVault swap path. All
        functions are pure views — never mutates state. Frontends can build
        a full swap UI from a single `batchQuote` round-trip (amounts, fee
        breakdown, liquidity, price freshness).

_Spec reference: `docs/architecture/pecor-sidiora-aggregator-spec.md` §7.11
     (FROZEN 2026-04-24). Interface:
     `contracts/meta-ag/interfaces/IMetaAGQuoter.sol`. Port of
     `dev/PECORQuoter.sol` with the following frozen-surface divergences:
       - Constructor-based replaced with UUPS `initialize(priceOracle, vault,
         weth, pecor, admin)` (spec §7.11).
       - Ownable admin implicitly via AccessControl.DEFAULT_ADMIN_ROLE on
         Timelock (S1). No public admin surface; evolution goes through UUPS.
       - OpenZeppelin Math.mulDiv replaced with `SidioraMath.mulDiv`.

Scope note (spec §7.11):
  MetaAGQuoter quotes ONLY vault-side (oracle-priced) swaps. Cross-adapter
  aggregation (Sidiora AMM + future adapters) lives on
  `MetaAGRouter.getBestQuote() / getAllQuotes()`.

Inheritance (spec §7.11):
  IMetaAGQuoter, Initializable, UUPSUpgradeable, AccessControl

Roles:
  - DEFAULT_ADMIN_ROLE → Timelock at deploy (invariant S1)

Storage layout (append-only per S12):
  slot 0:  AccessControl._roles   (mapping)
  slot 1:  priceOracle            (IPriceOracle)
  slot 2:  vault                  (IPECORVault)
  slot 3:  weth                   (address; semantically immutable after init)
  slot 4:  pecor                  (address; swapFeeBps read via staticcall)
  slot 5..54: __gap[50]_

### BPS_DENOMINATOR

```solidity
uint256 BPS_DENOMINATOR
```

### ZeroAddress

```solidity
error ZeroAddress()
```

### priceOracle

```solidity
contract IPriceOracle priceOracle
```

### vault

```solidity
contract IPECORVault vault
```

### weth

```solidity
address weth
```

Canonical wrapped-native token. Semantically immutable — assigned
        exactly once in {initialize} and never mutated afterwards.

### pecor

```solidity
address pecor
```

PECOR engine address. `swapFeeBps()` is read via staticcall so
        the quoter doesn't depend on IPECOR's impl evolving.

### constructor

```solidity
constructor() public
```

### initialize

```solidity
function initialize(address priceOracle_, address vault_, address weth_, address pecor_, address admin) external
```

Initialize the UUPS proxy (spec §7.11)

#### Parameters

| Name | Type | Description |
| ---- | ---- | ----------- |
| priceOracle_ | address |  |
| vault_ | address |  |
| weth_ | address |  |
| pecor_ | address |  |
| admin | address | Timelock — granted DEFAULT_ADMIN_ROLE |

### _authorizeUpgrade

```solidity
function _authorizeUpgrade(address) internal
```

### quoteExactIn

```solidity
function quoteExactIn(address tokenIn, address tokenOut, uint256 amountIn) external view returns (struct IMetaAGQuoter.QuoteResult)
```

### quoteExactOut

```solidity
function quoteExactOut(address tokenIn, address tokenOut, uint256 amountOut) external view returns (struct IMetaAGQuoter.QuoteResult)
```

### quoteExactInNative

```solidity
function quoteExactInNative(address tokenOut, uint256 nativeAmountIn) external view returns (struct IMetaAGQuoter.QuoteResult)
```

### quoteExactInToNative

```solidity
function quoteExactInToNative(address tokenIn, uint256 amountIn) external view returns (struct IMetaAGQuoter.QuoteResult)
```

### quoteMarketBuy

```solidity
function quoteMarketBuy(address stablecoin, address token, uint256 stablecoinAmount) external view returns (struct IMetaAGQuoter.QuoteResult)
```

### quoteMarketSell

```solidity
function quoteMarketSell(address token, address stablecoin, uint256 tokenAmount) external view returns (struct IMetaAGQuoter.QuoteResult)
```

### batchQuote

```solidity
function batchQuote(struct IMetaAGQuoter.QuoteRequest[] requests) external view returns (struct IMetaAGQuoter.QuoteResult[] results)
```

### getLiquidityInfo

```solidity
function getLiquidityInfo(address token) external view returns (uint256 available, uint256 tokenPrice, bool isStale)
```

### getAllLiquidityInfo

```solidity
function getAllLiquidityInfo() external view returns (address[] tokens, uint256[] reserves, uint256[] prices, bool[] stale)
```

### getTokenPrice

```solidity
function getTokenPrice(address token) external view returns (uint256 price, uint256 timestamp, bool isStale)
```

### getTokenPrices

```solidity
function getTokenPrices(address[] tokens) external view returns (uint256[] prices, uint256[] timestamps, bool[] stale)
```

### getTWAP

```solidity
function getTWAP(address token, uint256 period) external view returns (uint256)
```

### _buildQuote

```solidity
function _buildQuote(address tokenIn, address tokenOut, uint256 amount, bool isExactIn) internal view returns (struct IMetaAGQuoter.QuoteResult result)
```

Builds a full QuoteResult for a vault-side swap.

_Mirrors `PECOR._calculateSwapOutput/_calculateSwapInput` so the
     quoter returns the same amounts the engine would execute. Never
     reverts — unavailable prices return a partial result with
     `sufficientLiquidity=false` and the `priceStale*` flags set._

### _safeGetPrice

```solidity
function _safeGetPrice(address token) internal view returns (uint256 price, uint256 timestamp, bool stale)
```

_Non-reverting price fetch. Returns (0, 0, true) if the oracle
     call reverts (missing token, stale past threshold, paused...)._

### _calculateOutput

```solidity
function _calculateOutput(uint256 amountIn, uint256 priceIn, uint256 priceOut, uint256 decimalsIn, uint256 decimalsOut) internal pure returns (uint256)
```

_Mirrors PECOR's oracle-priced output calculation (decimals-aware)._

### _calculateInput

```solidity
function _calculateInput(uint256 amountOut, uint256 priceIn, uint256 priceOut, uint256 decimalsIn, uint256 decimalsOut) internal pure returns (uint256)
```

_Inverse of `_calculateOutput` — how much input is required to
     realize `amountOut` of tokenOut at the given oracle prices._

### _getFeeBps

```solidity
function _getFeeBps() internal view returns (uint256)
```

_Reads `swapFeeBps()` off the PECOR engine via low-level staticcall.
     Returns 0 if pecor is unset or the call fails (upgrade-safe)._

