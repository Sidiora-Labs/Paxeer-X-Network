// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

import "../libraries/FixedPoint128.sol";

/// @notice Wrapper to expose FixedPoint128 library functions for testing
contract FixedPoint128Wrapper {
    function Q128() external pure returns (uint256) {
        return FixedPoint128.Q128;
    }

    function mulQ128(uint256 x, uint256 y) external pure returns (uint256) {
        return FixedPoint128.mulQ128(x, y);
    }

    function divQ128(uint256 x, uint256 y) external pure returns (uint256) {
        return FixedPoint128.divQ128(x, y);
    }

    function toQ128(uint256 x) external pure returns (uint256) {
        return FixedPoint128.toQ128(x);
    }

    function fromQ128(uint256 x) external pure returns (uint256) {
        return FixedPoint128.fromQ128(x);
    }
}
