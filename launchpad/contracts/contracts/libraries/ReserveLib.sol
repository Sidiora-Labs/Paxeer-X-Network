// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

import "./SidioraMath.sol";

/// @title ReserveLib
/// @notice Constant-product AMM math with virtual reserves
/// @dev All functions are pure. Used by SidioraPool and Quoter.
///
/// Effective USDL Reserve = virtualUsdlReserve + realUsdlBalance
/// k = effectiveUsdlReserve × tokenReserve
///
/// BUY (USDL → Token):
///   amountOut = (tokenReserve × amountIn) / (effectiveUsdl + amountIn)
///
/// SELL (Token → USDL):
///   amountOut = (effectiveUsdl × amountIn) / (tokenReserve + amountIn)
library ReserveLib {
    error InsufficientInput();
    error InsufficientLiquidity();

    /// @notice Calculates effective USDL reserve (virtual + real)
    function getEffectiveReserves(
        uint256 virtualUsdl,
        uint256 realUsdl
    ) internal pure returns (uint256) {
        return virtualUsdl + realUsdl;
    }

    /// @notice Given an input amount and reserves, returns the output amount
    /// @dev Uses constant product formula: amountOut = (reserveOut * amountIn) / (reserveIn + amountIn)
    /// @param reserveIn Input reserve (effectiveUsdl for buys, tokenReserve for sells)
    /// @param reserveOut Output reserve (tokenReserve for buys, effectiveUsdl for sells)
    /// @param amountIn Input amount (after fee deduction)
    /// @return amountOut Output amount
    function getAmountOut(
        uint256 reserveIn,
        uint256 reserveOut,
        uint256 amountIn
    ) internal pure returns (uint256 amountOut) {
        if (amountIn == 0) revert InsufficientInput();
        if (reserveIn == 0 || reserveOut == 0) revert InsufficientLiquidity();

        // amountOut = (reserveOut * amountIn) / (reserveIn + amountIn)
        uint256 denominator = reserveIn + amountIn;
        amountOut = SidioraMath.mulDiv(reserveOut, amountIn, denominator);
    }

    /// @notice Given a desired output amount and reserves, returns the required input
    /// @dev Inverse of getAmountOut: amountIn = (reserveIn * amountOut) / (reserveOut - amountOut) + 1
    /// @param reserveIn Input reserve
    /// @param reserveOut Output reserve
    /// @param amountOut Desired output amount
    /// @return amountIn Required input amount
    function getAmountIn(
        uint256 reserveIn,
        uint256 reserveOut,
        uint256 amountOut
    ) internal pure returns (uint256 amountIn) {
        if (amountOut == 0) revert InsufficientInput();
        if (reserveIn == 0 || reserveOut == 0) revert InsufficientLiquidity();
        if (amountOut >= reserveOut) revert InsufficientLiquidity();

        // amountIn = (reserveIn * amountOut) / (reserveOut - amountOut) + 1
        uint256 denominator = reserveOut - amountOut;
        amountIn = SidioraMath.mulDiv(reserveIn, amountOut, denominator) + 1;
    }

    /// @notice Returns the current token price in USDL (18 decimal fixed-point)
    /// @dev price = effectiveUsdl * 1e18 / tokenReserve
    /// @param effectiveUsdl Virtual + real USDL reserve
    /// @param tokenReserve Token reserve
    /// @return price Token price in USDL with 18 decimals
    function getPrice(
        uint256 effectiveUsdl,
        uint256 tokenReserve
    ) internal pure returns (uint256 price) {
        if (tokenReserve == 0) revert InsufficientLiquidity();
        price = SidioraMath.mulDiv(effectiveUsdl, 1e18, tokenReserve);
    }

    /// @notice Returns market cap in USDL
    /// @dev marketCap = price * totalSupply / 1e18
    /// @param effectiveUsdl Virtual + real USDL reserve
    /// @param tokenReserve Token reserve in pool
    /// @param totalSupply Total token supply (including tokens outside pool)
    /// @return marketCap Market capitalization in USDL
    function getMarketCap(
        uint256 effectiveUsdl,
        uint256 tokenReserve,
        uint256 totalSupply
    ) internal pure returns (uint256 marketCap) {
        uint256 price = getPrice(effectiveUsdl, tokenReserve);
        marketCap = SidioraMath.mulDiv(price, totalSupply, 1e18);
    }
}
