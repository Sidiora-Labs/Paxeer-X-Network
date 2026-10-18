// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

import "../libraries/TokenAddress.sol";

/// @notice Wrapper to expose TokenAddress library functions for testing
contract TokenAddressWrapper {
    function computeAddress(
        address factory,
        address creator,
        string memory name,
        string memory symbol,
        uint256 nonce,
        bytes memory creationCode,
        uint256 totalSupply,
        address recipient
    ) external pure returns (address) {
        return TokenAddress.computeAddress(factory, creator, name, symbol, nonce, creationCode, totalSupply, recipient);
    }

    function computeSalt(
        address creator,
        string memory name,
        string memory symbol,
        uint256 nonce
    ) external pure returns (bytes32) {
        return TokenAddress.computeSalt(creator, name, symbol, nonce);
    }
}
