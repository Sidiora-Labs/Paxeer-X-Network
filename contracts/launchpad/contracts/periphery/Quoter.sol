// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

import "../base/Initializable.sol";
import "../base/UUPSUpgradeable.sol";
import "../base/AccessControl.sol";
import "../interfaces/IQuoter.sol";
import "../interfaces/ISidioraPool.sol";
import "../interfaces/IProtocolConfig.sol";
import "../interfaces/IPoolRegistry.sol";
import "../libraries/ReserveLib.sol";
import "../libraries/FeeLib.sol";
import "../libraries/SidioraMath.sol";

/// @title Quoter
/// @notice Read-only quote and data access. No state changes. Gas-free via staticcall.
/// @dev UUPS proxy. All functions are view — no state mutations.
contract Quoter is IQuoter, Initializable, UUPSUpgradeable, AccessControl {
    error ZeroAddress();
    error UnexecutableQuote();
    error OpticalQuoteUnsupported();

    address public poolRegistry;
    address public protocolConfig;

    /// @custom:oz-upgrades-unsafe-allow constructor
    constructor() {
        _disableInitializers();
    }

    function initialize(
        address _poolRegistry,
        address _protocolConfig,
        address _admin
    ) external initializer {
        if (_admin == address(0)) revert ZeroAddress();
        if (_poolRegistry == address(0)) revert ZeroAddress();

        poolRegistry = _poolRegistry;
        protocolConfig = _protocolConfig;

        _grantRole(DEFAULT_ADMIN_ROLE, _admin);
    }

    /// @inheritdoc IQuoter
    function quoteExactInput(
        address pool,
        uint256 amountIn,
        bool isBuy
    ) external view override returns (QuoteResult memory result) {
        _validatePool(pool);
        (uint256 virtualUsdl, uint256 realUsdl, uint256 tokenRes) = ISidioraPool(pool).getReserves();
        uint256 effectiveUsdl = virtualUsdl + realUsdl;

        // Calculate fee
        uint256 feeBps = _calculateFee(pool);
        result.feeAmount = SidioraMath.mulDiv(amountIn, feeBps, 10000);
        uint256 amountInAfterFee = amountIn - result.feeAmount;

        // Calculate output
        if (isBuy) {
            result.amountOut = ReserveLib.getAmountOut(effectiveUsdl, tokenRes, amountInAfterFee);
        } else {
            result.amountOut = ReserveLib.getAmountOut(tokenRes, effectiveUsdl, amountInAfterFee);
            if (result.amountOut > realUsdl) revert UnexecutableQuote();
        }

        if (result.amountOut == 0) revert UnexecutableQuote();

        // Calculate price impact in bps
        // Price impact = |newPrice - oldPrice| / oldPrice * 10000
        if (tokenRes > 0 && effectiveUsdl > 0 && ReserveLib.getPrice(effectiveUsdl, tokenRes) > 0) {
            uint256 priceBefore = ReserveLib.getPrice(effectiveUsdl, tokenRes);
            uint256 newEffectiveUsdl;
            uint256 newTokenRes;
            if (isBuy) {
                newEffectiveUsdl = effectiveUsdl + amountInAfterFee;
                newTokenRes = tokenRes - result.amountOut;
            } else {
                newEffectiveUsdl = effectiveUsdl - result.amountOut;
                newTokenRes = tokenRes + amountIn;
            }
            if (newTokenRes > 0) {
                uint256 priceAfter = ReserveLib.getPrice(newEffectiveUsdl, newTokenRes);
                if (priceAfter > priceBefore) {
                    result.priceImpactBps = SidioraMath.mulDiv(priceAfter - priceBefore, 10000, priceBefore);
                } else {
                    result.priceImpactBps = SidioraMath.mulDiv(priceBefore - priceAfter, 10000, priceBefore);
                }
            }
        }
    }

    /// @inheritdoc IQuoter
    function getPoolPrice(address pool) external view override returns (uint256) {
        return ISidioraPool(pool).getPrice();
    }

    /// @inheritdoc IQuoter
    function getPoolStats(address pool) external view override returns (PoolStats memory stats) {
        (stats.virtualUsdl, stats.realUsdl, stats.tokenReserve) = ISidioraPool(pool).getReserves();
        stats.cumulativeVolume = ISidioraPool(pool).cumulativeVolume();
        stats.currentFeeBps = _calculateFee(pool);
        stats.poolAge = block.timestamp - ISidioraPool(pool).creationTimestamp();
        stats.price = ISidioraPool(pool).getPrice();

        // Market cap = price * totalSupply / 1e18
        uint256 effectiveUsdl = stats.virtualUsdl + stats.realUsdl;
        if (stats.tokenReserve > 0) {
            // Get total supply from token
            address tokenAddr = ISidioraPool(pool).tokenAddress();
            uint256 totalSupply = _getTokenTotalSupply(tokenAddr);
            stats.marketCap = ReserveLib.getMarketCap(effectiveUsdl, stats.tokenReserve, totalSupply);
        }
    }

    /// @inheritdoc IQuoter
    function getMarketCap(address pool) external view override returns (uint256) {
        (uint256 virtualUsdl, uint256 realUsdl, uint256 tokenRes) = ISidioraPool(pool).getReserves();
        uint256 effectiveUsdl = virtualUsdl + realUsdl;
        address tokenAddr = ISidioraPool(pool).tokenAddress();
        uint256 totalSupply = _getTokenTotalSupply(tokenAddr);
        return ReserveLib.getMarketCap(effectiveUsdl, tokenRes, totalSupply);
    }

    /// @inheritdoc IQuoter
    function getAllPools(uint256 offset, uint256 limit) external view override returns (address[] memory) {
        return IPoolRegistry(poolRegistry).getAllPools(offset, limit);
    }

    /// @inheritdoc IQuoter
    function getPoolsByCreator(address creator) external view override returns (address[] memory) {
        return IPoolRegistry(poolRegistry).getPoolsByCreator(creator);
    }

    /// @inheritdoc IQuoter
    function quoteMultihop(
        address tokenIn,
        address tokenOut,
        uint256 amountIn
    ) external view override returns (MultihopQuoteResult memory result) {
        if (tokenIn == address(0) || tokenOut == address(0)) revert ZeroAddress();
        if (tokenIn == tokenOut) revert ZeroAddress(); // reuse error for simplicity

        // Resolve pools
        result.poolA = IPoolRegistry(poolRegistry).getPoolByToken(tokenIn);
        result.poolB = IPoolRegistry(poolRegistry).getPoolByToken(tokenOut);
        if (result.poolA == address(0) || result.poolB == address(0)) revert ZeroAddress();

        _validatePool(result.poolA);
        _validatePool(result.poolB);

        // ── LEG 1: Sell tokenIn → USDL on poolA ──
        (
            uint256 virtualUsdlA,
            uint256 realUsdlA,
            uint256 tokenResA
        ) = ISidioraPool(result.poolA).getReserves();
        uint256 effectiveUsdlA = virtualUsdlA + realUsdlA;

        uint256 feeBpsA = _calculateFee(result.poolA);
        result.sellFeeAmount = SidioraMath.mulDiv(amountIn, feeBpsA, 10000);
        uint256 amountInAfterFeeA = amountIn - result.sellFeeAmount;

        // Sell: tokenReserve is reserveIn, effectiveUsdl is reserveOut
        result.intermediateUsdl = ReserveLib.getAmountOut(tokenResA, effectiveUsdlA, amountInAfterFeeA);

        if (result.intermediateUsdl == 0 || result.intermediateUsdl > realUsdlA) revert UnexecutableQuote();

        // Sell price impact
        if (tokenResA > 0 && effectiveUsdlA > 0 && ReserveLib.getPrice(effectiveUsdlA, tokenResA) > 0) {
            uint256 priceBeforeA = ReserveLib.getPrice(effectiveUsdlA, tokenResA);
            uint256 newEffUsdlA = effectiveUsdlA - result.intermediateUsdl;
            uint256 newTokenResA = tokenResA + amountIn;
            if (newTokenResA > 0 && newEffUsdlA > 0) {
                uint256 priceAfterA = ReserveLib.getPrice(newEffUsdlA, newTokenResA);
                result.sellPriceImpactBps = priceBeforeA > priceAfterA
                    ? SidioraMath.mulDiv(priceBeforeA - priceAfterA, 10000, priceBeforeA)
                    : SidioraMath.mulDiv(priceAfterA - priceBeforeA, 10000, priceBeforeA);
            }
        }

        // ── LEG 2: Buy tokenOut with USDL on poolB ──
        (
            uint256 virtualUsdlB,
            uint256 realUsdlB,
            uint256 tokenResB
        ) = ISidioraPool(result.poolB).getReserves();
        uint256 effectiveUsdlB = virtualUsdlB + realUsdlB;

        uint256 feeBpsB = _calculateFee(result.poolB);
        result.buyFeeAmount = SidioraMath.mulDiv(result.intermediateUsdl, feeBpsB, 10000);
        uint256 amountInAfterFeeB = result.intermediateUsdl - result.buyFeeAmount;

        // Buy: effectiveUsdl is reserveIn, tokenReserve is reserveOut
        result.amountOut = ReserveLib.getAmountOut(effectiveUsdlB, tokenResB, amountInAfterFeeB);

        if (result.amountOut == 0) revert UnexecutableQuote();

        // Buy price impact
        if (tokenResB > 0 && effectiveUsdlB > 0 && ReserveLib.getPrice(effectiveUsdlB, tokenResB) > 0) {
            uint256 priceBeforeB = ReserveLib.getPrice(effectiveUsdlB, tokenResB);
            uint256 newEffUsdlB = effectiveUsdlB + amountInAfterFeeB;
            uint256 newTokenResB = tokenResB - result.amountOut;
            if (newTokenResB > 0) {
                uint256 priceAfterB = ReserveLib.getPrice(newEffUsdlB, newTokenResB);
                result.buyPriceImpactBps = priceAfterB > priceBeforeB
                    ? SidioraMath.mulDiv(priceAfterB - priceBeforeB, 10000, priceBeforeB)
                    : SidioraMath.mulDiv(priceBeforeB - priceAfterB, 10000, priceBeforeB);
            }
        }

        // Combined price impact (additive approximation)
        result.combinedPriceImpactBps = result.sellPriceImpactBps + result.buyPriceImpactBps;
    }

    // --- Internal ---

    function _calculateFee(address pool) internal view returns (uint256) {
        return ISidioraPool(pool).currentFeeBps();
    }

    /// @dev Hooks can reject or adjust trades based on trader-specific state. A generic
    ///      view quote cannot promise execution; fail explicitly rather than overquote.
    function _validatePool(address pool) internal view {
        if (!IPoolRegistry(poolRegistry).isRegisteredPool(pool)) revert UnexecutableQuote();
        if (ISidioraPool(pool).opticalAddress() != address(0)) revert OpticalQuoteUnsupported();
        (bool ok, bytes memory data) = pool.staticcall(abi.encodeWithSignature("paused()"));
        if (!ok || data.length != 32 || abi.decode(data, (bool))) revert UnexecutableQuote();
    }

    function _getTokenTotalSupply(address token) internal view returns (uint256) {
        (bool success, bytes memory data) = token.staticcall(
            abi.encodeWithSignature("totalSupply()")
        );
        if (!success || data.length != 32 || token.code.length == 0) revert UnexecutableQuote();
        return abi.decode(data, (uint256));
    }

    function _authorizeUpgrade(address) internal override onlyRole(DEFAULT_ADMIN_ROLE) {}
}
