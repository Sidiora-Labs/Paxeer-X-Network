// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

import "../libraries/PoolAddress.sol";

/// @notice Wrapper to expose PoolAddress library functions for testing
contract PoolAddressWrapper {
    function computeAddress(
        address factory,
        address beacon,
        address token,
        bytes memory creationCode,
        bytes memory initData
    ) external pure returns (address) {
        return PoolAddress.computeAddress(factory, beacon, token, creationCode, initData);
    }

    function computeSalt(address token) external pure returns (bytes32) {
        return PoolAddress.computeSalt(token);
    }
}
