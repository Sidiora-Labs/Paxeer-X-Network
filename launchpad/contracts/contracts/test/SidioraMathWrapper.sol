// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

import "../libraries/SidioraMath.sol";

/// @notice Wrapper to expose SidioraMath library functions for testing
contract SidioraMathWrapper {
    using SidioraMath for uint256;

    function sqrt(uint256 x) external pure returns (uint256) {
        return SidioraMath.sqrt(x);
    }

    function mulDiv(uint256 a, uint256 b, uint256 denominator) external pure returns (uint256) {
        return SidioraMath.mulDiv(a, b, denominator);
    }

    function mulDivRoundingUp(uint256 a, uint256 b, uint256 denominator) external pure returns (uint256) {
        return SidioraMath.mulDivRoundingUp(a, b, denominator);
    }

    function min(uint256 a, uint256 b) external pure returns (uint256) {
        return SidioraMath.min(a, b);
    }

    function max(uint256 a, uint256 b) external pure returns (uint256) {
        return SidioraMath.max(a, b);
    }

    function abs(int256 x) external pure returns (uint256) {
        return SidioraMath.abs(x);
    }

    function safeCastToUint128(uint256 x) external pure returns (uint128) {
        return SidioraMath.safeCastToUint128(x);
    }

    function safeCastToInt256(uint256 x) external pure returns (int256) {
        return SidioraMath.safeCastToInt256(x);
    }
}
