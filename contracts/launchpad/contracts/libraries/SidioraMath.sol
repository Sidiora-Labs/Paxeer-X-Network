// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

/// @title SidioraMath
/// @notice Core math primitives: sqrt, mulDiv, min, max, abs, safe casting
/// @dev All functions are pure/internal. No external dependencies.
library SidioraMath {
    error Overflow();
    error DivisionByZero();

    /// @notice Calculates floor(sqrt(x)) using the Babylonian method
    /// @param x The value to take the square root of
    /// @return z The floor of the square root
    function sqrt(uint256 x) internal pure returns (uint256 z) {
        if (x == 0) return 0;
        if (x <= 3) return 1;

        z = x;
        uint256 y = (x >> 1) + 1;
        while (y < z) {
            z = y;
            y = (x / y + y) >> 1;
        }
    }

    /// @notice Calculates (a * b) / denominator with 512-bit intermediate precision
    /// @dev Reverts on denominator == 0 or result overflow
    /// @param a Multiplicand
    /// @param b Multiplier
    /// @param denominator Divisor
    /// @return result The 256-bit result
    function mulDiv(
        uint256 a,
        uint256 b,
        uint256 denominator
    ) internal pure returns (uint256 result) {
        if (denominator == 0) revert DivisionByZero();

        uint256 prod0;
        uint256 prod1;
        assembly {
            let mm := mulmod(a, b, not(0))
            prod0 := mul(a, b)
            prod1 := sub(sub(mm, prod0), lt(mm, prod0))
        }

        if (prod1 == 0) {
            return prod0 / denominator;
        }

        if (prod1 >= denominator) revert Overflow();

        unchecked {
            uint256 remainder;
            assembly {
                remainder := mulmod(a, b, denominator)
                prod1 := sub(prod1, gt(remainder, prod0))
                prod0 := sub(prod0, remainder)
            }

            uint256 twos = denominator & (0 - denominator);
            assembly {
                denominator := div(denominator, twos)
                prod0 := div(prod0, twos)
                twos := add(div(sub(0, twos), twos), 1)
            }
            prod0 |= prod1 * twos;

            uint256 inverse = (3 * denominator) ^ 2;
            inverse *= 2 - denominator * inverse;
            inverse *= 2 - denominator * inverse;
            inverse *= 2 - denominator * inverse;
            inverse *= 2 - denominator * inverse;
            inverse *= 2 - denominator * inverse;
            inverse *= 2 - denominator * inverse;

            result = prod0 * inverse;
        }
    }

    /// @notice Calculates (a * b) / denominator, rounded up
    function mulDivRoundingUp(
        uint256 a,
        uint256 b,
        uint256 denominator
    ) internal pure returns (uint256 result) {
        result = mulDiv(a, b, denominator);
        if (mulmod(a, b, denominator) > 0) {
            if (result == type(uint256).max) revert Overflow();
            result++;
        }
    }

    /// @notice Returns the smaller of two values
    function min(uint256 a, uint256 b) internal pure returns (uint256) {
        return a < b ? a : b;
    }

    /// @notice Returns the larger of two values
    function max(uint256 a, uint256 b) internal pure returns (uint256) {
        return a > b ? a : b;
    }

    /// @notice Returns the absolute value of a signed integer
    function abs(int256 x) internal pure returns (uint256) {
        return x >= 0 ? uint256(x) : uint256(-(x + 1)) + 1;
    }

    /// @notice Safely casts a uint256 to uint128
    function safeCastToUint128(uint256 x) internal pure returns (uint128) {
        if (x > type(uint128).max) revert Overflow();
        return uint128(x);
    }

    /// @notice Safely casts a uint256 to int256
    function safeCastToInt256(uint256 x) internal pure returns (int256) {
        if (x > uint256(type(int256).max)) revert Overflow();
        return int256(x);
    }
}
