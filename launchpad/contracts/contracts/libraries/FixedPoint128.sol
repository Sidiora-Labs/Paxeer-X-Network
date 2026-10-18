// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

import "./SidioraMath.sol";

/// @title FixedPoint128
/// @notice Q128.128 fixed-point arithmetic for precise price calculations
/// @dev Uses SidioraMath.mulDiv for overflow-safe operations
library FixedPoint128 {
    uint256 internal constant Q128 = 1 << 128;

    /// @notice Multiplies a value by a Q128.128 fixed-point number
    /// @param x The multiplicand
    /// @param y The Q128.128 multiplier
    /// @return The result (not in Q128 format — regular uint256)
    function mulQ128(uint256 x, uint256 y) internal pure returns (uint256) {
        return SidioraMath.mulDiv(x, y, Q128);
    }

    /// @notice Divides a value by a Q128.128 fixed-point number
    /// @param x The dividend
    /// @param y The Q128.128 divisor
    /// @return The result (not in Q128 format — regular uint256)
    function divQ128(uint256 x, uint256 y) internal pure returns (uint256) {
        return SidioraMath.mulDiv(x, Q128, y);
    }

    /// @notice Converts a regular uint256 to Q128.128 format
    /// @param x The value to convert
    /// @return The Q128.128 representation
    function toQ128(uint256 x) internal pure returns (uint256) {
        return x * Q128;
    }

    /// @notice Converts a Q128.128 value back to regular uint256 (truncates)
    /// @param x The Q128.128 value
    /// @return The truncated uint256
    function fromQ128(uint256 x) internal pure returns (uint256) {
        return x >> 128;
    }
}
