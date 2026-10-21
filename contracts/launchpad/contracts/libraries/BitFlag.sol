// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

/// @title BitFlag
/// @notice Bitwise operations for optical hook flags
/// @dev Bit 0: BEFORE_SWAP, Bit 1: AFTER_SWAP,
///      Bit 2: BEFORE_FEE_DISTRIBUTION, Bit 3: AFTER_FEE_DISTRIBUTION
library BitFlag {
    uint8 internal constant BEFORE_TOKEN_TRANSFER = 16;
    uint8 internal constant BEFORE_SWAP = 1;           // 0001
    uint8 internal constant AFTER_SWAP = 2;            // 0010
    uint8 internal constant BEFORE_FEE_DISTRIBUTION = 4; // 0100
    uint8 internal constant AFTER_FEE_DISTRIBUTION = 8;  // 1000

    /// @notice Checks if a specific flag is set in the flags bitmap
    function hasFlag(uint8 flags, uint8 flag) internal pure returns (bool) {
        return (flags & flag) != 0;
    }

    /// @notice Sets a specific flag in the flags bitmap
    function setFlag(uint8 flags, uint8 flag) internal pure returns (uint8) {
        return flags | flag;
    }

    /// @notice Clears a specific flag from the flags bitmap
    function clearFlag(uint8 flags, uint8 flag) internal pure returns (uint8) {
        return flags & ~flag;
    }
}
