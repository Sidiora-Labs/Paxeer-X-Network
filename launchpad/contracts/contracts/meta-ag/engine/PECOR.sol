// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.27;

import {IPECOR} from "../interfaces/IPECOR.sol";
import {IPECORVault} from "../interfaces/IPECORVault.sol";
import {IPriceOracle} from "../interfaces/IPriceOracle.sol";
import {ITransactionTracker} from "../interfaces/ITransactionTracker.sol";
import {IWETH} from "../interfaces/IWETH.sol";
import {IERC20Minimal} from "../interfaces/IERC20Minimal.sol";
import {Initializable} from "../../base/Initializable.sol";
import {UUPSUpgradeable} from "../../base/UUPSUpgradeable.sol";
import {AccessControl} from "../../base/AccessControl.sol";
import {ReentrancyGuard} from "../../base/ReentrancyGuard.sol";
import {Pausable} from "../../base/Pausable.sol";
import {TransferHelper} from "../../libraries/TransferHelper.sol";
import {PECORMath} from "./PECORMath.sol";
import {SidioraMath} from "../../libraries/SidioraMath.sol";

/// @title PECOR — Paxeer Eqos Central Order Router (Swap Engine)
/// @notice UUPS-upgradeable oracle-priced swap engine against PECORVault v2.
///         Handles simple swaps, market orders, and native-coin swaps.
///         Order management (limit / stop-loss / stop-limit) lives in
///         PECOROrders.sol (Task 4.2).
/// @dev Spec reference: `docs/architecture/pecor-sidiora-aggregator-spec.md` §7.6
///      (FROZEN 2026-04-24). Interface: `contracts/meta-ag/interfaces/IPECOR.sol`.
///
/// Inheritance (spec §7.6):
///   IPECOR, Initializable, UUPSUpgradeable, AccessControl,
///   ReentrancyGuard, Pausable
///
/// Roles:
///   - DEFAULT_ADMIN_ROLE → Timelock at deploy (invariant S1)
///   - FEE_COLLECTOR_ROLE → granted to `feeCollector` via {setFeeCollector}
///
/// Storage layout (append-only per S12):
///   slot 0:  AccessControl._roles             (mapping)
///   slot 1:  priceOracle                      (address)
///   slot 2:  vault                            (address)
///   slot 3:  transactionTracker               (address)
///   slot 4:  weth                             (address)
///   slot 5:  swapFeeBps                       (uint256)
///   slot 6:  tier1FeeBps                      (uint256)
///   slot 7:  tier2FeeBps                      (uint256)
///   slot 8:  priceImpactEnabled               (bool)
///   slot 9:  priceImpactScalarBps             (uint256)
///   slot 10: feeCollector                     (address)
///   slot 11: accruedFees                      (mapping)
///   slot 12..61: __gap[50]
contract PECOR is
    IPECOR,
    Initializable,
    UUPSUpgradeable,
    AccessControl,
    ReentrancyGuard,
    Pausable
{

    bytes32 public constant FEE_COLLECTOR_ROLE = keccak256("FEE_COLLECTOR_ROLE");

    uint256 public constant PRECISION = 1e18;
    uint256 public constant BPS_DENOMINATOR = 10_000;

    /// @notice Absolute cap for fee stacking — swapFeeBps + tier1FeeBps + tier2FeeBps
    ///         must never exceed this (spec invariant S11).
    uint256 public constant MAX_FEE_BPS = 200;

    /// @notice Maximum price impact applied to any single swap.
    uint256 public constant MAX_IMPACT_BPS = 500;

    /// @notice Swap volume tier thresholds (USD, 18-decimals fixed-point).
    uint256 public constant TIER1_THRESHOLD = 10_000e18;
    uint256 public constant TIER2_THRESHOLD = 100_000e18;

    error ZeroAddress();
    error ZeroAmount();
    error SameToken();
    error Expired();
    error InsufficientOutput();
    error ExcessiveInput();
    error InsufficientLiquidity();
    error NotAStablecoin();
    error TokenIsStablecoin();
    error UseWethDeposit();
    error UseWethWithdraw();
    error NoFeesToCollect();
    error FeeTooHigh();
    error Tier2BelowTier1();
    error ScalarTooHigh();
    error NativeTransferFailed();
    error MulticallFailed(uint256 index);
    error UnsupportedInputTransfer();
    error LiabilityMigrationRequired();
    error MigrationAlreadyComplete();
    event TrackerCallFailed(address indexed tracker, bytes4 indexed selector);

    IPriceOracle public priceOracle;
    IPECORVault public vault;
    ITransactionTracker public transactionTracker;

    /// @notice Wrapped native coin. Assigned once in {initialize}.
    address public weth;

    /// @notice Base protocol fee in BPS, applied to every swap.
    uint256 public swapFeeBps;
    /// @notice Additional fee in BPS for volumeUSD >= TIER1_THRESHOLD.
    uint256 public tier1FeeBps;
    /// @notice Additional fee in BPS for volumeUSD >= TIER2_THRESHOLD.
    uint256 public tier2FeeBps;

    /// @notice Toggle for price-impact deduction on net swap output.
    bool public priceImpactEnabled;
    /// @notice Scalar used to scale impact by swap-to-reserve ratio.
    uint256 public priceImpactScalarBps;

    /// @notice Destination for {collectFees}. Also holds FEE_COLLECTOR_ROLE.
    address public feeCollector;

    /// @notice Per-token accrued fees (tracked here; actual tokens live in
    ///         the vault and are moved via vault.pushTokens at collect time).
    mapping(address => uint256) public accruedFees;

    /// @dev Reserved storage for future upgrades (S12: append-only, 50 * 32 bytes).
    bool public feesInitialized;
    uint256 public feeMigrationCursor;
    uint256[48] private __gap;

    modifier liabilitiesInitialized() {
        if (!feesInitialized) revert LiabilityMigrationRequired();
        _;
    }

    modifier ensure(uint256 deadline) {
        if (deadline != 0 && block.timestamp > deadline) revert Expired();
        _;
    }

    /// @custom:oz-upgrades-unsafe-allow constructor
    constructor() {
        _disableInitializers();
    }

    /// @inheritdoc IPECOR
    function initialize(
        address priceOracle_,
        address vault_,
        address weth_,
        address tracker_,
        address admin_
    ) external override initializer {
        if (priceOracle_ == address(0)) revert ZeroAddress();
        if (vault_ == address(0)) revert ZeroAddress();
        if (weth_ == address(0)) revert ZeroAddress();
        if (admin_ == address(0)) revert ZeroAddress();

        _initReentrancyGuard();
        priceOracle = IPriceOracle(priceOracle_);
        vault = IPECORVault(vault_);
        weth = weth_;
        transactionTracker = ITransactionTracker(tracker_);

        feesInitialized = true;
        tier1FeeBps = 20;
        tier2FeeBps = 50;
        priceImpactScalarBps = 100;
        priceImpactEnabled = true;

        _grantRole(DEFAULT_ADMIN_ROLE, admin_);
    }

    function _authorizeUpgrade(address) internal override onlyRole(DEFAULT_ADMIN_ROLE) {}

    /// @inheritdoc IPECOR
    function setPriceOracle(address oracle) external override onlyRole(DEFAULT_ADMIN_ROLE) {
        if (oracle == address(0)) revert ZeroAddress();
        priceOracle = IPriceOracle(oracle);
        emit PriceOracleUpdated(oracle);
    }

    /// @inheritdoc IPECOR
    function setTransactionTracker(address tracker) external override onlyRole(DEFAULT_ADMIN_ROLE) {
        transactionTracker = ITransactionTracker(tracker);
        emit TransactionTrackerUpdated(tracker);
    }

    /// @inheritdoc IPECOR
    /// @dev S11: swapFeeBps + tier1FeeBps + tier2FeeBps ≤ MAX_FEE_BPS.
    function setSwapFee(uint256 feeBps) external override onlyRole(DEFAULT_ADMIN_ROLE) {
        if (feeBps + tier1FeeBps + tier2FeeBps > MAX_FEE_BPS) revert FeeTooHigh();
        swapFeeBps = feeBps;
        emit SwapFeeUpdated(feeBps);
    }

    /// @inheritdoc IPECOR
    /// @dev S11: swapFeeBps + tier1FeeBps + tier2FeeBps ≤ MAX_FEE_BPS.
    ///      Also enforces tier2FeeBps ≥ tier1FeeBps (monotonic escalation).
    function setTieredFees(
        uint256 tier1FeeBps_,
        uint256 tier2FeeBps_
    ) external override onlyRole(DEFAULT_ADMIN_ROLE) {
        if (tier2FeeBps_ < tier1FeeBps_) revert Tier2BelowTier1();
        if (swapFeeBps + tier1FeeBps_ + tier2FeeBps_ > MAX_FEE_BPS) revert FeeTooHigh();
        tier1FeeBps = tier1FeeBps_;
        tier2FeeBps = tier2FeeBps_;
        emit TieredFeesUpdated(tier1FeeBps_, tier2FeeBps_);
    }

    /// @inheritdoc IPECOR
    function setPriceImpact(
        bool enabled,
        uint256 scalarBps
    ) external override onlyRole(DEFAULT_ADMIN_ROLE) {
        if (scalarBps > MAX_IMPACT_BPS) revert ScalarTooHigh();
        priceImpactEnabled = enabled;
        priceImpactScalarBps = scalarBps;
        emit PriceImpactConfigUpdated(enabled, scalarBps);
    }

    /// @inheritdoc IPECOR
    /// @dev Rotates FEE_COLLECTOR_ROLE from the previous collector to the new one.
    function setFeeCollector(address collector) external override onlyRole(DEFAULT_ADMIN_ROLE) {
        if (collector == address(0)) revert ZeroAddress();
        address previous = feeCollector;
        if (previous != address(0)) {
            _revokeRole(FEE_COLLECTOR_ROLE, previous);
        }
        feeCollector = collector;
        _grantRole(FEE_COLLECTOR_ROLE, collector);
        emit FeeCollectorUpdated(collector);
    }

    /// @inheritdoc IPECOR
    function pause() external override {
        if (!hasRole(DEFAULT_ADMIN_ROLE, msg.sender)) _checkRole(PAUSER_ROLE, msg.sender);
        _pause();
    }

    /// @inheritdoc IPECOR
    function unpause() external override onlyRole(DEFAULT_ADMIN_ROLE) {
        _unpause();
    }

    /// @inheritdoc IPECOR
    /// @dev Moves accrued fees out of the vault into {feeCollector}.
    ///      The vault treats PECOR as an OPERATOR_ROLE holder, so this
    ///      succeeds after vault.setOperator(PECOR, true).
    function collectFees(address token) external override onlyRole(FEE_COLLECTOR_ROLE) nonReentrant liabilitiesInitialized {
        uint256 amount = accruedFees[token];
        if (amount == 0) revert NoFeesToCollect();
        accruedFees[token] = 0;
        vault.releaseTokens(token, amount);
        vault.pushTokens(token, feeCollector, amount);
        emit FeesCollected(token, feeCollector, amount);
    }

    /// @inheritdoc IPECOR
    function swapExactIn(
        address tokenIn,
        address tokenOut,
        uint256 amountIn,
        uint256 amountOutMin,
        uint256 deadline
    ) external override nonReentrant liabilitiesInitialized whenNotPaused ensure(deadline) returns (uint256 amountOut) {
        if (tokenIn == tokenOut) revert SameToken();
        if (amountIn == 0) revert ZeroAmount();

        amountIn = vault.pullTokens(tokenIn, msg.sender, amountIn);

        uint256 priceIn = priceOracle.getPrice(tokenIn);
        uint256 priceOut = priceOracle.getPrice(tokenOut);

        uint256 grossOut = _calculateSwapOutput(tokenIn, tokenOut, amountIn, priceIn, priceOut);
        uint256 volumeUSD = SidioraMath.mulDiv(amountIn, priceIn, 10 ** vault.getTokenDecimals(tokenIn));
        uint256 reserveUSD = SidioraMath.mulDiv(
            vault.getAvailableReserves(tokenOut),
            priceOut,
            10 ** vault.getTokenDecimals(tokenOut)
        );

        amountOut = _applyFeeAndImpact(tokenOut, grossOut, volumeUSD, reserveUSD);

        if (amountOut < amountOutMin) revert InsufficientOutput();
        if (!vault.hasLiquidity(tokenOut, amountOut)) revert InsufficientLiquidity();

        vault.pushTokens(tokenOut, msg.sender, amountOut);

        emit SimpleSwap(msg.sender, tokenIn, tokenOut, amountIn, amountOut, priceOut);
        _recordTrade(msg.sender, tokenIn, tokenOut, amountIn, amountOut, volumeUSD);
    }

    /// @inheritdoc IPECOR
    function swapExactOut(
        address tokenIn,
        address tokenOut,
        uint256 amountOut,
        uint256 amountInMax,
        uint256 deadline
    ) external override nonReentrant liabilitiesInitialized whenNotPaused ensure(deadline) returns (uint256 amountIn) {
        if (tokenIn == tokenOut) revert SameToken();
        if (amountOut == 0) revert ZeroAmount();
        if (!vault.hasLiquidity(tokenOut, amountOut)) revert InsufficientLiquidity();

        uint256 priceIn = priceOracle.getPrice(tokenIn);
        uint256 priceOut = priceOracle.getPrice(tokenOut);
        amountIn = _quoteExactOut(tokenIn, tokenOut, amountOut, priceIn, priceOut);
        if (amountIn > amountInMax) revert ExcessiveInput();
        uint256 actualIn = vault.pullTokens(tokenIn, msg.sender, amountIn);
        if (actualIn != amountIn) revert UnsupportedInputTransfer();
        uint256 volumeUSD = SidioraMath.mulDiv(amountIn, priceIn, 10 ** vault.getTokenDecimals(tokenIn));
        uint256 grossOut = _calculateSwapOutput(tokenIn, tokenOut, amountIn, priceIn, priceOut);
        // Every retained output unit is an output-token liability, including
        // fee/impact and conservative input/output integer rounding.
        uint256 retained = grossOut - amountOut;
        if (retained != 0) {
            vault.reserveTokens(tokenOut, retained);
            accruedFees[tokenOut] += retained;
        }
        if (!vault.hasLiquidity(tokenOut, amountOut)) revert InsufficientLiquidity();
        vault.pushTokens(tokenOut, msg.sender, amountOut);

        emit SimpleSwap(msg.sender, tokenIn, tokenOut, amountIn, amountOut, priceOut);
        _recordTrade(msg.sender, tokenIn, tokenOut, amountIn, amountOut, volumeUSD);
    }

    /// @inheritdoc IPECOR
    function marketBuy(
        address stablecoin,
        address token,
        uint256 stablecoinAmount,
        uint256 minTokenAmount,
        uint256 deadline
    ) external override nonReentrant liabilitiesInitialized whenNotPaused ensure(deadline) returns (uint256 tokenAmount) {
        if (!vault.isStablecoin(stablecoin)) revert NotAStablecoin();
        if (vault.isStablecoin(token)) revert TokenIsStablecoin();
        if (stablecoinAmount == 0) revert ZeroAmount();

        stablecoinAmount = vault.pullTokens(stablecoin, msg.sender, stablecoinAmount);

        uint256 tokenPrice = priceOracle.getPrice(token);
        uint256 stablePrice = priceOracle.getPrice(stablecoin);

        uint256 grossOut = _calculateSwapOutput(stablecoin, token, stablecoinAmount, stablePrice, tokenPrice);
        uint256 volumeUSD = SidioraMath.mulDiv(
            stablecoinAmount,
            stablePrice,
            10 ** vault.getTokenDecimals(stablecoin)
        );
        uint256 reserveUSD = SidioraMath.mulDiv(
            vault.getAvailableReserves(token),
            tokenPrice,
            10 ** vault.getTokenDecimals(token)
        );

        tokenAmount = _applyFeeAndImpact(token, grossOut, volumeUSD, reserveUSD);

        if (tokenAmount < minTokenAmount) revert InsufficientOutput();
        if (!vault.hasLiquidity(token, tokenAmount)) revert InsufficientLiquidity();

        vault.pushTokens(token, msg.sender, tokenAmount);

        emit MarketOrderExecuted(msg.sender, stablecoin, token, stablecoinAmount, tokenAmount, true);
        _recordTrade(msg.sender, stablecoin, token, stablecoinAmount, tokenAmount, volumeUSD);
        _recordMarketTrade(msg.sender, stablecoin, token, stablecoinAmount, tokenAmount, true, tokenPrice);
    }

    /// @inheritdoc IPECOR
    function marketSell(
        address token,
        address stablecoin,
        uint256 tokenAmount,
        uint256 minStablecoinAmount,
        uint256 deadline
    ) external override nonReentrant liabilitiesInitialized whenNotPaused ensure(deadline) returns (uint256 stablecoinAmount) {
        if (!vault.isStablecoin(stablecoin)) revert NotAStablecoin();
        if (vault.isStablecoin(token)) revert TokenIsStablecoin();
        if (tokenAmount == 0) revert ZeroAmount();

        tokenAmount = vault.pullTokens(token, msg.sender, tokenAmount);

        uint256 tokenPrice = priceOracle.getPrice(token);
        uint256 stablePrice = priceOracle.getPrice(stablecoin);

        uint256 grossOut = _calculateSwapOutput(token, stablecoin, tokenAmount, tokenPrice, stablePrice);
        uint256 volumeUSD = SidioraMath.mulDiv(
            tokenAmount,
            tokenPrice,
            10 ** vault.getTokenDecimals(token)
        );
        uint256 reserveUSD = SidioraMath.mulDiv(
            vault.getAvailableReserves(stablecoin),
            stablePrice,
            10 ** vault.getTokenDecimals(stablecoin)
        );

        stablecoinAmount = _applyFeeAndImpact(stablecoin, grossOut, volumeUSD, reserveUSD);

        if (stablecoinAmount < minStablecoinAmount) revert InsufficientOutput();
        if (!vault.hasLiquidity(stablecoin, stablecoinAmount)) revert InsufficientLiquidity();

        vault.pushTokens(stablecoin, msg.sender, stablecoinAmount);

        emit MarketOrderExecuted(msg.sender, stablecoin, token, stablecoinAmount, tokenAmount, false);
        _recordTrade(msg.sender, token, stablecoin, tokenAmount, stablecoinAmount, volumeUSD);
        _recordMarketTrade(msg.sender, stablecoin, token, stablecoinAmount, tokenAmount, false, tokenPrice);
    }

    /// @inheritdoc IPECOR
    function swapExactInNative(
        address tokenOut,
        uint256 amountOutMin,
        uint256 deadline
    )
        external
        payable
        override
        nonReentrant
        liabilitiesInitialized
        whenNotPaused
        ensure(deadline)
        returns (uint256 amountOut)
    {
        if (msg.value == 0) revert ZeroAmount();
        address wethAddr = weth;
        if (tokenOut == wethAddr) revert UseWethDeposit();

        IWETH(wethAddr).deposit{value: msg.value}();
        TransferHelper.safeApprove(wethAddr, address(vault), msg.value);

        uint256 actualIn = vault.pullTokens(wethAddr, address(this), msg.value);
        if (actualIn != msg.value) revert UnsupportedInputTransfer();

        uint256 priceIn = priceOracle.getPrice(wethAddr);
        uint256 priceOut = priceOracle.getPrice(tokenOut);

        uint256 grossOut = _calculateSwapOutput(wethAddr, tokenOut, msg.value, priceIn, priceOut);
        uint256 volumeUSD = SidioraMath.mulDiv(
            msg.value,
            priceIn,
            10 ** vault.getTokenDecimals(wethAddr)
        );
        uint256 reserveUSD = SidioraMath.mulDiv(
            vault.getAvailableReserves(tokenOut),
            priceOut,
            10 ** vault.getTokenDecimals(tokenOut)
        );

        amountOut = _applyFeeAndImpact(tokenOut, grossOut, volumeUSD, reserveUSD);

        if (amountOut < amountOutMin) revert InsufficientOutput();
        if (!vault.hasLiquidity(tokenOut, amountOut)) revert InsufficientLiquidity();

        vault.pushTokens(tokenOut, msg.sender, amountOut);

        emit NativeSwap(msg.sender, tokenOut, msg.value, amountOut, true);
        _recordTrade(msg.sender, wethAddr, tokenOut, msg.value, amountOut, volumeUSD);
    }

    /// @inheritdoc IPECOR
    function swapExactInToNative(
        address tokenIn,
        uint256 amountIn,
        uint256 amountOutMin,
        uint256 deadline
    )
        external
        override
        nonReentrant
        liabilitiesInitialized
        whenNotPaused
        ensure(deadline)
        returns (uint256 amountOut)
    {
        if (amountIn == 0) revert ZeroAmount();
        address wethAddr = weth;
        if (tokenIn == wethAddr) revert UseWethWithdraw();

        amountIn = vault.pullTokens(tokenIn, msg.sender, amountIn);

        uint256 priceIn = priceOracle.getPrice(tokenIn);
        uint256 priceOut = priceOracle.getPrice(wethAddr);

        uint256 grossOut = _calculateSwapOutput(tokenIn, wethAddr, amountIn, priceIn, priceOut);
        uint256 volumeUSD = SidioraMath.mulDiv(
            amountIn,
            priceIn,
            10 ** vault.getTokenDecimals(tokenIn)
        );
        uint256 reserveUSD = SidioraMath.mulDiv(
            vault.getAvailableReserves(wethAddr),
            priceOut,
            10 ** vault.getTokenDecimals(wethAddr)
        );

        amountOut = _applyFeeAndImpact(wethAddr, grossOut, volumeUSD, reserveUSD);

        if (amountOut < amountOutMin) revert InsufficientOutput();
        if (!vault.hasLiquidity(wethAddr, amountOut)) revert InsufficientLiquidity();

        vault.withdrawNative(amountOut, msg.sender);

        emit NativeSwap(msg.sender, tokenIn, amountOut, amountIn, false);
        _recordTrade(msg.sender, tokenIn, wethAddr, amountIn, amountOut, volumeUSD);
    }

    /// @inheritdoc IPECOR
    function getQuoteExactIn(
        address tokenIn,
        address tokenOut,
        uint256 amountIn
    ) external view override returns (uint256 amountOut) {
        uint256 priceIn = priceOracle.getPrice(tokenIn);
        uint256 priceOut = priceOracle.getPrice(tokenOut);
        uint256 grossOut = _calculateSwapOutput(tokenIn, tokenOut, amountIn, priceIn, priceOut);
        uint256 volumeUSD = SidioraMath.mulDiv(
            amountIn,
            priceIn,
            10 ** vault.getTokenDecimals(tokenIn)
        );
        uint256 reserveUSD = SidioraMath.mulDiv(
            vault.getAvailableReserves(tokenOut),
            priceOut,
            10 ** vault.getTokenDecimals(tokenOut)
        );
        return _calcNetOutput(grossOut, volumeUSD, reserveUSD);
    }

    /// @inheritdoc IPECOR
    function getQuoteExactOut(
        address tokenIn,
        address tokenOut,
        uint256 amountOut
    ) external view override returns (uint256 amountIn) {
        uint256 priceIn = priceOracle.getPrice(tokenIn);
        uint256 priceOut = priceOracle.getPrice(tokenOut);
        if (tokenIn == tokenOut) revert SameToken();
        if (amountOut == 0) revert ZeroAmount();
        return _quoteExactOut(tokenIn, tokenOut, amountOut, priceIn, priceOut);
    }

    /// @inheritdoc IPECOR
    function getDetailedQuote(
        address tokenIn,
        address tokenOut,
        uint256 amountIn
    )
        external
        view
        override
        returns (
            uint256 grossOut,
            uint256 netOut,
            uint256 priceImpactBps,
            uint256 feeBps,
            uint256 feeAmount
        )
    {
        uint256 priceIn = priceOracle.getPrice(tokenIn);
        uint256 priceOut = priceOracle.getPrice(tokenOut);
        grossOut = _calculateSwapOutput(tokenIn, tokenOut, amountIn, priceIn, priceOut);
        uint256 volumeUSD = SidioraMath.mulDiv(
            amountIn,
            priceIn,
            10 ** vault.getTokenDecimals(tokenIn)
        );
        uint256 reserveUSD = SidioraMath.mulDiv(
            vault.getAvailableReserves(tokenOut),
            priceOut,
            10 ** vault.getTokenDecimals(tokenOut)
        );

        feeBps = _getEffectiveFeeBps(volumeUSD);
        feeAmount = SidioraMath.mulDiv(grossOut, feeBps, BPS_DENOMINATOR);

        if (priceImpactEnabled && reserveUSD > 0) {
            priceImpactBps = SidioraMath.mulDiv(volumeUSD, priceImpactScalarBps, reserveUSD);
            if (priceImpactBps > MAX_IMPACT_BPS) priceImpactBps = MAX_IMPACT_BPS;
        }

        uint256 afterFee = grossOut - feeAmount;
        uint256 impactDeduction = priceImpactBps == 0
            ? 0
            : SidioraMath.mulDiv(afterFee, priceImpactBps, BPS_DENOMINATOR);
        netOut = afterFee - impactDeduction;
    }

    /// @inheritdoc IPECOR
    /// @dev Bubbles up the first failing call's revert data verbatim so that
    ///      custom errors reach the outer caller. Matches the ERC-1967 proxy
    ///      hardening landed in Phase 3 commit 18.
    function multicall(bytes[] calldata data) external override returns (bytes[] memory results) {
        uint256 n = data.length;
        results = new bytes[](n);
        for (uint256 i = 0; i < n; ++i) {
            (bool success, bytes memory result) = address(this).delegatecall(data[i]);
            if (!success) {
                if (result.length > 0) {
                    // solhint-disable-next-line no-inline-assembly
                    assembly {
                        revert(add(result, 32), mload(result))
                    }
                }
                revert MulticallFailed(i);
            }
            results[i] = result;
        }
    }

    /// @notice Accept native transfers only from WETH unwraps initiated by PECOR.
    receive() external payable {
        if (msg.sender != weth) revert NativeTransferFailed();
    }

    /// @dev Compute gross output respecting token decimals and oracle prices.
    function _calculateSwapOutput(
        address tokenIn,
        address tokenOut,
        uint256 amountIn,
        uint256 priceIn,
        uint256 priceOut
    ) internal view returns (uint256) {
        return PECORMath.convert(amountIn, priceIn, priceOut, vault.getTokenDecimals(tokenIn), vault.getTokenDecimals(tokenOut), false);
    }

    /// @dev Compute required input for a target output, rounding up.
    function _calculateSwapInput(
        address tokenIn,
        address tokenOut,
        uint256 amountOut,
        uint256 priceIn,
        uint256 priceOut
    ) internal view returns (uint256) {
        return PECORMath.convert(amountOut, priceOut, priceIn, vault.getTokenDecimals(tokenOut), vault.getTokenDecimals(tokenIn), true);
    }

    struct InverseContext {
        address tokenIn;
        address tokenOut;
        uint256 priceIn;
        uint256 priceOut;
        uint256 unit;
        uint256 reserveUSD;
    }

    /// @dev Invert both integer-rounded deductions exactly. Discrete impact
    /// steps and fee-tier jumps can make net output non-monotone, so a global
    /// binary search is unsafe. Within each fee tier the assumed impact only
    /// increases toward a valid candidate; all smaller candidates have already
    /// failed a lower-bound deduction. Impact is bounded to 0..500 BPS.
    function _quoteExactOut(address tokenIn, address tokenOut, uint256 target, uint256 priceIn, uint256 priceOut)
        internal view returns (uint256)
    {
        InverseContext memory q = InverseContext(tokenIn, tokenOut, priceIn, priceOut,
            10 ** vault.getTokenDecimals(tokenIn),
            SidioraMath.mulDiv(vault.getAvailableReserves(tokenOut), priceOut, 10 ** vault.getTokenDecimals(tokenOut)));
        uint256 first = _ceilMulDiv(TIER1_THRESHOLD, q.unit, priceIn);
        uint256 second = _ceilMulDiv(TIER2_THRESHOLD, q.unit, priceIn);
        uint256 lower;
        for (uint256 tier; tier < 3; ++tier) {
            uint256 upper = tier == 0 ? first - 1 : tier == 1 ? second - 1 : type(uint256).max;
            uint256 fee = swapFeeBps + (tier >= 1 ? tier1FeeBps : 0) + (tier == 2 ? tier2FeeBps : 0);
            (bool found, uint256 candidate) = _inverseTier(q, target, fee, lower, upper);
            if (found) return candidate;
            lower = tier == 0 ? first : second;
        }
        revert InsufficientLiquidity();
    }

    function _inverseTier(InverseContext memory q, uint256 target, uint256 fee, uint256 lower, uint256 upper)
        private view returns (bool found, uint256 candidate)
    {
        uint256 assumedImpact;
        for (uint256 step; step <= MAX_IMPACT_BPS; ++step) {
            uint256 afterFee = SidioraMath.mulDiv(target - 1, BPS_DENOMINATOR, BPS_DENOMINATOR - assumedImpact) + 1;
            uint256 gross = SidioraMath.mulDiv(afterFee - 1, BPS_DENOMINATOR, BPS_DENOMINATOR - fee) + 1;
            candidate = _calculateSwapInput(q.tokenIn, q.tokenOut, gross, q.priceIn, q.priceOut);
            if (candidate < lower) candidate = lower;
            if (candidate > upper) return (false, 0);
            uint256 volumeUSD = SidioraMath.mulDiv(candidate, q.priceIn, q.unit);
            uint256 impact;
            if (priceImpactEnabled && q.reserveUSD != 0) {
                impact = SidioraMath.mulDiv(volumeUSD, priceImpactScalarBps, q.reserveUSD);
                if (impact > MAX_IMPACT_BPS) impact = MAX_IMPACT_BPS;
            }
            if (impact <= assumedImpact) return (true, candidate);
            assumedImpact = impact;
        }
    }

    function _ceilMulDiv(uint256 a, uint256 b, uint256 d) private pure returns (uint256 q) {
        q = SidioraMath.mulDiv(a, b, d);
        if (mulmod(a, b, d) != 0) ++q;
    }

    /// @dev Apply tiered fee + price impact, credit accrued fees.
    function _applyFeeAndImpact(
        address token,
        uint256 grossAmount,
        uint256 volumeUSD,
        uint256 reserveUSD
    ) internal returns (uint256 netAmount) {
        uint256 effectiveFeeBps = _getEffectiveFeeBps(volumeUSD);
        uint256 feeAmount = SidioraMath.mulDiv(grossAmount, effectiveFeeBps, BPS_DENOMINATOR);
        netAmount = grossAmount - feeAmount;

        if (feeAmount > 0) {
            accruedFees[token] += feeAmount;
            emit TieredFeeApplied(msg.sender, token, volumeUSD, effectiveFeeBps, feeAmount);
        }

        if (priceImpactEnabled && reserveUSD > 0) {
            uint256 impactBps = SidioraMath.mulDiv(volumeUSD, priceImpactScalarBps, reserveUSD);
            if (impactBps > MAX_IMPACT_BPS) impactBps = MAX_IMPACT_BPS;
            if (impactBps > 0) {
                uint256 impactDeduction = SidioraMath.mulDiv(netAmount, impactBps, BPS_DENOMINATOR);
                netAmount -= impactDeduction;
                accruedFees[token] += impactDeduction;
                emit PriceImpactApplied(msg.sender, token, impactBps, impactDeduction);
            }
        }
        uint256 retained = grossAmount - netAmount;
        if (retained != 0) vault.reserveTokens(token, retained);
    }

    /// @dev Pure view form of {_applyFeeAndImpact} — no state or events.
    function _calcNetOutput(
        uint256 grossAmount,
        uint256 volumeUSD,
        uint256 reserveUSD
    ) internal view returns (uint256 netAmount) {
        uint256 effectiveFeeBps = _getEffectiveFeeBps(volumeUSD);
        uint256 feeAmount = SidioraMath.mulDiv(grossAmount, effectiveFeeBps, BPS_DENOMINATOR);
        netAmount = grossAmount - feeAmount;

        if (priceImpactEnabled && reserveUSD > 0) {
            uint256 impactBps = SidioraMath.mulDiv(volumeUSD, priceImpactScalarBps, reserveUSD);
            if (impactBps > MAX_IMPACT_BPS) impactBps = MAX_IMPACT_BPS;
            if (impactBps > 0) {
                netAmount -= SidioraMath.mulDiv(netAmount, impactBps, BPS_DENOMINATOR);
            }
        }
    }

    /// @dev Tiered fee resolution per spec §7.6 / invariant S11.
    function _getEffectiveFeeBps(uint256 volumeUSD) internal view returns (uint256) {
        if (volumeUSD >= TIER2_THRESHOLD) return swapFeeBps + tier1FeeBps + tier2FeeBps;
        if (volumeUSD >= TIER1_THRESHOLD) return swapFeeBps + tier1FeeBps;
        return swapFeeBps;
    }

    /// @dev Gated call to the transaction tracker; silently skipped if unset.
    ///      When set, the tracker enforces EMITTER_ROLE on its side (S10).
    function _recordTrade(
        address user,
        address tokenIn,
        address tokenOut,
        uint256 amountIn,
        uint256 amountOut,
        uint256 volumeUSD
    ) internal {
        if (address(transactionTracker) == address(0)) return;
        _track(abi.encodeCall(ITransactionTracker.recordTrade, (user, tokenIn, tokenOut, amountIn, amountOut, volumeUSD)));
    }

    /// @dev Gated market-order record; silently skipped if tracker unset.
    function _recordMarketTrade(
        address user,
        address stablecoin,
        address token,
        uint256 stablecoinAmount,
        uint256 tokenAmount,
        bool isBuy,
        uint256 executionPrice
    ) internal {
        if (address(transactionTracker) == address(0)) return;
        _track(abi.encodeCall(ITransactionTracker.recordMarketTrade,
            (user, stablecoin, token, stablecoinAmount, tokenAmount, isBuy, executionPrice)));
    }
    function _track(bytes memory payload) internal {
        address tracker = address(transactionTracker);
        bool success;
        assembly ("memory-safe") { success := call(500000, tracker, 0, add(payload, 32), mload(payload), 0, 0) }
        if (!success || tracker.code.length == 0) emit TrackerCallFailed(tracker, bytes4(payload));
    }

    function liabilitiesReady() external view returns (bool) { return feesInitialized; }

    /// @notice Existing proxies must pause and reserve old fees before trading.
    function migrateFeeReservations(uint256 maxCount) external onlyRole(DEFAULT_ADMIN_ROLE) whenPaused nonReentrant {
        if (feesInitialized) revert MigrationAlreadyComplete();
        if (maxCount == 0 || maxCount > 100) revert ZeroAmount();
        address[] memory tokens = vault.getRegisteredTokens();
        uint256 end = feeMigrationCursor + (maxCount < tokens.length - feeMigrationCursor ? maxCount : tokens.length - feeMigrationCursor);
        for (; feeMigrationCursor < end; ++feeMigrationCursor) {
            address token = tokens[feeMigrationCursor];
            uint256 amount = accruedFees[token];
            if (amount != 0) vault.reserveTokens(token, amount);
        }
        if (end == tokens.length) feesInitialized = true;
    }

}

/*
 * ══════════════════════════════════════════════════════════════════════════════════
 *
 *              Paxlabs HyperPax-OS-Protocol LICENSE (HyperPax-OS-Protocol)
 *                 Copyright © 2026 Paxlabs Inc. All rights reserved.
 *           
 *
 * ══════════════════════════════════════════════════════════════════════════════════
 *
 *   HyperPax-OS-Protocol License — Summary (non-binding): You may read, use, deploy, 
 *   and integrate HyperPax-OS-Protocol. If you Modify and distribute/deploy 
 *   the Modified version, you must release your changes under this same license.
 *   NO Commercial License is required until you cross a Commercial Trigger 
 *   (e.g., Charged Fees > US$100,000 in any rolling 12-month period or in any 
 *   single calendar month, or Liquidity Under Control > US$10,000,000).
 *   This summary is for convenience only.
 *
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *   1) DEFINITION
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *
 *   1.1  "Licensed Work" means the HyperPax-OS-Protocol stack as released in this repository,
 *        including: (a) the core HyperPax-OS-Protocol execution engine, instruction/handler
 *        interfaces, example instruction libraries published by Paxlabs, SDK stubs, schemas,
 *        configuration, tests, tooling, bytecode/ABIs, and deployment scripts; (b) all
 *        documentation and technical specifications published by Paxlabs; and (c) all updates,
 *        patches, and new versions of the foregoing that Paxlabs publishes under this license.
 *
 *   1.2  "Charged Fees" means all monetary or in-kind value (fiat, crypto, tokens, credits,
 *        rebates, or other consideration) that You or Your Affiliates directly or indirectly
 *        receive or accrue in connection with operating, offering, or providing access to any
 *        product or service that is powered by, routed through, or materially enabled by the
 *        Licensed Work, including without limitation: (a) swap/trade/execution fees, positive-
 *        slippage capture, spreads, mark-ups, retained priority/tips; (b) maker/taker rebates,
 *        order-flow payments, routing/referral/affiliate fees, MEV/builder/bundle payments and
 *        other extractable-value shares; (c) subscription, seat, usage, API, or platform fees
 *        attributable to the Licensed Work; (d) performance/incentive/carried-interest fees,
 *        revenue shares, or similar participation; and (e) token grants, rewards, airdrops,
 *        distributions, or rebates received by or for You or Your Affiliates in consideration
 *        of or tied to such operations. Charged Fees are measured on a gross-receipts basis at
 *        fair-market value in USD when received or accrued, include amounts received by
 *        agents/designees or wallets You control, and must be reasonably allocated for bundles.
 *        Anti-avoidance: relabeling, splitting, routing through Affiliates/Related Parties,
 *        offsetting, or deferring does not exclude amounts; Affiliates/common control are
 *        aggregated; the Control or Benefit Principle applies.
 *
 *   1.3  "Commercial License" means a separate written agreement between Paxlabs and You
 *        (and/or Your Affiliates), that grants You the right to engage in Commercial Use of
 *        the Licensed Work subject to negotiated terms, conditions, and fees.
 *
 *   1.4  "Control or Benefit Principle." Triggers and obligations apply where you or your
 *        Affiliates control the relevant activity or benefit economically from it (directly
 *        or through agents/DAOs under your direction).
 *
 *   1.5  "Rolling Year" means any period of twelve (12) consecutive months measured on a
 *        rolling basis.
 *
 *   1.6  "Liquidity Under Control (LUC)" means the aggregate fair-market USD value of real,
 *        non-synthetic, non-levered, withdrawable assets that Your (or Your Affiliates'/
 *        agents') products, services, or code can instruct or cause to be moved or committed
 *        via the Licensed Work (e.g., wallet balances under automated control, committed
 *        liquidity, or programmatic authorization) at the time assessed.
 *
 *   1.7  "Modify" (and "Modified Work") means to change, fork, translate, extend, or create a
 *        derivative work of the Licensed Work, including: (a) altering source or bytecode;
 *        (b) creating plug-ins/modules/instruction programs that run in the same program/
 *        runtime or EVM address space (e.g., static/dynamic linking, delegatecall/proxy
 *        patterns); or (c) bundling the Licensed Work and additions as a single product.
 *
 *   1.8  "You" (and "Your") means the individual or legal entity exercising rights under this
 *        license, and its Affiliates. "Affiliates" are entities controlling, controlled by, or
 *        under common control with a party, directly or indirectly.
 *
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *   2) GRANT
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *
 *   2.1  Source and Object Use. Subject to Sections 3–11, Paxlabs grants You a worldwide,
 *        non-exclusive license to use, copy, distribute unmodified source/object forms of the
 *        Licensed Work.
 *
 *   2.2  Pure Caller Use (Integration-Only / Non-Modifying). Pure Caller Use means building or
 *        operating products or services that interact with the Licensed Work solely by forming
 *        calldata, submitting transactions, or reading state through published ABIs, APIs, or
 *        RPC endpoints, without distributing any Modified Work. Pure Caller Use is permitted
 *        under this License and does not, by itself, trigger any payment obligations or
 *        Commercial License. However, if in connection with Pure Caller Use You or Your
 *        Affiliates (a) charge or retain any fees, spreads, rebates, incentives, or other
 *        consideration; (b) meet any Trigger in Section 5.2; such use constitutes Commercial
 *        Use and requires obtaining a Commercial License from Paxlabs. Even where Pure Caller
 *        Use is not met, see §5.3 for the current enforcement waiver applicable to Volume
 *        Activities.
 *
 *   2.3  Audit/Research Safe Harbor. Security auditors and researchers may compile, test, and
 *        report on the Licensed Work in the course of good-faith security research.
 *
 *   2.4  For any distribution, public display, public performance, publication, reporting,
 *        disclosure, or other public communication of any portion of the Licensed Work or any
 *        analysis, results, or outputs derived from the Licensed Work, You must preserve all
 *        existing copyright, license, and attribution notices included in the Licensed Work
 *        and must include a reasonable attribution identifying the source as
 *        "HyperPaxeer — © Paxlabs Inc 2026" (or any successor notice included in the
 *        Licensed Work).
 *
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *   3) COPYLEFT FOR MODIFICATIONS & EXTENSIONS
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *
 *   3.1  If you Modify or distribute any portion of the Licensed Work, you must:
 *        A. Publish under this same license (LicenseRef-Paxlabs-HyperPax-OS-Protocol license),
 *           at no charge, complete corresponding source of any portions of Your work that
 *           modify, extend, incorporate, or otherwise rely on the Licensed Work;
 *        B. Preserve existing copyright, license, and third-party notices;
 *        C. Add prominent attribution: "Powered by HyperPax-OS-Protocol — © Paxlabs Inc 2026"
 *           in repository README and UI where applicable;
 *        D. Clearly mark changes and date of change;
 *        E. Provide build and deployment instructions sufficient for reproducibility.
 *
 *   3.2  This copyleft covers all forms of Modification, combination, or use of the Licensed
 *        Work in or with other code, products, or systems.
 *
 *   3.3  The obligations in §§3.1 A-B apply only to components that are derivative of the
 *        Licensed Work. Independent code that simply calls, interfaces with, or is distributed
 *        alongside the Licensed Work is not subject to this requirement.
 *
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *   4) NON-COMMERCIAL FREE USE
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *
 *   Non-commercial use (including experimentation, prototyping, hackathons, research, community
 *   pilots) is free of charge, subject to Section 3 for any Modifications; and provided that
 *   such use does not constitute or involve any activity described in Section 5.
 *
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *   5) COMMERCIAL TRIGGER
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *
 *   5.1  Commercial Use: Any Commercial Use of the Licensed Work requires a Commercial License
 *        from Paxlabs, unless otherwise expressly stated. Commercial Use means any use of the
 *        Licensed Work that provides, enables, or is integrated into any product, service,
 *        system, workflow, or operation from which You or Your Affiliates derive, or reasonably
 *        expect to derive, monetary or in-kind commercial value, directly or indirectly,
 *        including through Charged Fees or other consideration.
 *
 *   5.2  Without limiting §5.1, You (and Your Affiliates) must obtain a Commercial License from
 *        Paxlabs if any of the following occur:
 *        A. Aggregated Fees Trigger: Your aggregated Charged Fees attributable to usage of the
 *           Licensed Work exceed USD 100,000 in any Rolling Year.
 *        B. LUC Trigger: Your LUC exceeds USD 10,000,000 at any time.
 *        C. Operator/Liquidity Provider Direct-Use. You (or Your Affiliate) operate instruction
 *           programs or services (e.g., deploying, offering, or running products or services
 *           powered by, routed through, or materially enabled by the Licensed Work) or acting
 *           as a Liquidity Provider that directly exercise the Licensed Work (bypassing Paxeer
 *           Network/Paxlabs or other permitted/licensed interfaces) to capture fees or value
 *           and, in doing so, satisfy Triggers A or B above.
 *        You must aggregate commonly-controlled/Affiliated entities; no disaggregation, white-
 *        labeling, or similar structuring to avoid a Trigger is permitted. The Control or
 *        Benefit Principle applies.
 *
 *   5.3  Notwithstanding §§ 5.1 - 5.2, Paxlabs presently waives enforcement of the Commercial
 *        Triggers for parties whose activities consist primarily of routing order flow,
 *        aggregation, arbitrage, or market-making through the Licensed Work ("Volume
 *        Activities"), including where such parties (i) trade with their own or third-party
 *        capital and/or (ii) charge or retain fees, spreads, rebates, or other compensation.
 *        This waiver is not a license, creates no reliance rights, and is revocable by Paxlabs
 *        at any time in its sole discretion, including with respect to existing users, by
 *        (a) public notice in the project repository or (b) direct notice. Upon notice of
 *        revocation, you must within ten (10) days cease the Volume Activities or obtain a
 *        Commercial License; continued use thereafter constitutes unauthorized Commercial Use.
 *        This waiver does not excuse past breaches unrelated to this subsection.
 *
 *   5.4  Crossing any Trigger or any other Commercial Use requires you to contact Paxlabs
 *        within 15 days at license@Paxlabs.com to execute Commercial License. Commercial
 *        License terms are confidential and may change.
 *
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *   6) AUDIT
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *
 *   Once per year, Paxlabs may request an independent revenue/LUC audit (under NDA) at
 *   Paxlabs's expense; if under-reporting exceeds 5%, You reimburse reasonable audit costs in
 *   addition to other remedies. Paxlabs may also request an additional attestation "for cause"
 *   (objective indications of a Trigger). You must reasonably cooperate with any such
 *   attestation, including providing accurate records, logs, and other information reasonably
 *   necessary.
 *
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *   7) ADDITIONAL INTELLECTUAL PROPERTY TERMS
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *
 *   7.1  Patents. Paxlabs grants a limited, non-exclusive, worldwide license under Paxlabs's
 *        patent claims that read on the Licensed Work solely to the extent necessary to
 *        exercise the rights expressly granted to You under this License. Nothing in this
 *        License grants You any right to patent, claim, or seek protection for (a) the
 *        Licensed Work (whether modified or unmodified), (b) any Modification of the Licensed
 *        Work, or (c) any work or system that incorporates, combines with, or depends on the
 *        Licensed Work. This patent license terminates if you (or your Affiliates) stop using
 *        the Licensed Work or assert any patent claim against Paxlabs or compliant users of
 *        the Licensed Work. No implied patent license is granted beyond this clause.
 *
 *   7.2  Trademarks & Branding. Trademarks. No rights are granted to use any Paxlabs/
 *        HyperPax-OS-Protocol/Paxeer Network names, logos, or trademarks, or any "Powered by
 *        HyperPaxeer" or similar designation, except solely to make truthful statements of
 *        compatibility or integration. Any use must comply with Paxlabs's brand guidelines and
 *        may require separate written permission.
 *
 *   7.3  Except as expressly granted, no other rights (by implication, estoppel, or otherwise)
 *        are granted, copyrights, patents, trade secrets, trademarks, or other IP.
 *
 *   7.4  You must not suggest Paxlabs endorses or certifies Your product absent a written
 *        agreement.
 *
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *   8) WARRANTY & LIABILITY
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *
 *   THE LICENSED WORK IS PROVIDED BY PAXLABS "AS IS" AND "AS AVAILABLE," WITHOUT WARRANTIES OF
 *   ANY KIND, EXPRESS OR IMPLIED, INCLUDING MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE,
 *   NON-INFRINGEMENT, AND THAT OPERATION WILL BE UNINTERRUPTED OR ERROR-FREE. TO THE MAXIMUM
 *   EXTENT PERMITTED BY LAW, PAXLABS, ITS AFFILIATES AND CONTRIBUTORS ARE NOT LIABLE FOR
 *   INDIRECT, INCIDENTAL, SPECIAL, CONSEQUENTIAL, EXEMPLARY, OR PUNITIVE DAMAGES, OR LOST
 *   PROFITS/REVENUE/GOODWILL, ARISING FROM OR RELATED TO THIS LICENSE OR THE LICENSED WORK,
 *   EVEN IF ADVISED OF THE POSSIBILITY.
 *
 *   Nothing in this Section limits Paxlabs's ability to seek injunctive relief without bond in
 *   addition to other remedies or to enforce Your obligations under this License.
 *
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *   9) TERMINATION
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *
 *   Material breach, including any breach of §§ 2-9, not cured within 15 days of notice
 *   terminates this License. Prior compliant distributions survive. Sections 2.4, 3, 8, 10,
 *   11.3 survive termination.
 *
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *   10) GOVERNING LAW; VENUE; INJUNCTIONS
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *
 *   This License is governed by the laws of the State of New York, excluding conflict rules.
 *   The parties submit to the exclusive jurisdiction and venue of the state and federal courts
 *   located in New York County, New York (SDNY). Each party consents to injunctive relief
 *   (including specific performance) for actual or threatened breach.
 *
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *   11) NOTICES; ASSIGNMENT; ENTIRE AGREEMENT
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *
 *   11.1 Notices. Legal or any other notices to Paxlabs: legal@Paxlabs.com (with subject
 *        "HyperPaxeer Notice").
 *
 *   11.2 Third-Party Components. Portions of the Licensed Work may incorporate, bundle, or
 *        reference third-party components governed by their own licenses. You must comply with
 *        those third-party terms; nothing in this License limits rights granted by those
 *        licenses. Preserve all third-party copyright and license notices. A list of such
 *        components and licenses is provided in THIRD_PARTY_NOTICES (and/or in file headers)
 *        and may be updated from time to time.
 *
 *   11.3 Assignment. You may not assign this License (by law or otherwise) without Paxlabs's
 *        prior written consent; any unauthorized assignment is void. Paxlabs may assign freely.
 *
 *   11.4 Entire Agreement. This License is the entire agreement for the Licensed Work and
 *        supersedes prior understandings. If any provision is unenforceable, it will be
 *        modified to the minimum extent necessary to be enforceable; the remainder stays in
 *        effect. No waiver is effective unless in writing.
 *
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *   12) VERSIONING
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *
 *   Paxlabs may publish new or updated versions of this License from time to time. Each
 *   release of the Licensed Work is governed by the license version identified in the
 *   repository for that release. Paxlabs may also re-release the Licensed Work, or any
 *   portion of it, under different license terms in future releases.
 *
 * ═══════════════════════════════════════════════════════════════════════════════════════════════
 *   END OF LICENSE — Contact: license@Paxlabs.com  |  legal@Paxlabs.com
 * ═══════════════════════════════════════════════════════════════════════════════════════════════
 */
