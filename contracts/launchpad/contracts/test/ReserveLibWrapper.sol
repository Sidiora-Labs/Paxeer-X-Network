// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

import "../libraries/ReserveLib.sol";

/// @notice Wrapper to expose ReserveLib library functions for testing
contract ReserveLibWrapper {
    function getEffectiveReserves(uint256 virtualUsdl, uint256 realUsdl) external pure returns (uint256) {
        return ReserveLib.getEffectiveReserves(virtualUsdl, realUsdl);
    }

    function getAmountOut(uint256 reserveIn, uint256 reserveOut, uint256 amountIn) external pure returns (uint256) {
        return ReserveLib.getAmountOut(reserveIn, reserveOut, amountIn);
    }

    function getAmountIn(uint256 reserveIn, uint256 reserveOut, uint256 amountOut) external pure returns (uint256) {
        return ReserveLib.getAmountIn(reserveIn, reserveOut, amountOut);
    }

    function getPrice(uint256 effectiveUsdl, uint256 tokenReserve) external pure returns (uint256) {
        return ReserveLib.getPrice(effectiveUsdl, tokenReserve);
    }

    function getMarketCap(uint256 effectiveUsdl, uint256 tokenReserve, uint256 totalSupply) external pure returns (uint256) {
        return ReserveLib.getMarketCap(effectiveUsdl, tokenReserve, totalSupply);
    }
}
