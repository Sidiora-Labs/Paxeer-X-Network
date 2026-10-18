// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

import "../libraries/BitFlag.sol";

/// @notice Wrapper to expose BitFlag library functions for testing
contract BitFlagWrapper {
    function BEFORE_SWAP() external pure returns (uint8) {
        return BitFlag.BEFORE_SWAP;
    }

    function AFTER_SWAP() external pure returns (uint8) {
        return BitFlag.AFTER_SWAP;
    }

    function BEFORE_FEE_DISTRIBUTION() external pure returns (uint8) {
        return BitFlag.BEFORE_FEE_DISTRIBUTION;
    }

    function AFTER_FEE_DISTRIBUTION() external pure returns (uint8) {
        return BitFlag.AFTER_FEE_DISTRIBUTION;
    }

    function hasFlag(uint8 flags, uint8 flag) external pure returns (bool) {
        return BitFlag.hasFlag(flags, flag);
    }

    function setFlag(uint8 flags, uint8 flag) external pure returns (uint8) {
        return BitFlag.setFlag(flags, flag);
    }

    function clearFlag(uint8 flags, uint8 flag) external pure returns (uint8) {
        return BitFlag.clearFlag(flags, flag);
    }
}
