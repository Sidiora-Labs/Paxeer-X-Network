// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

/// @title PoolAddress
/// @notice Computes deterministic pool addresses from BeaconProxy CREATE2 parameters
/// @dev Used by Factory, Router, and Quoter to predict pool addresses before deployment
library PoolAddress {
    /// @notice Computes the CREATE2 address for a pool BeaconProxy
    /// @param factory The factory contract address (deployer)
    /// @param beacon The PoolBeacon address
    /// @param token The SidioraERC20 token address (used in salt)
    /// @param creationCode The BeaconProxy creation code
    /// @return pool The deterministic pool address
    function computeAddress(
        address factory,
        address beacon,
        address token,
        bytes memory creationCode,
        bytes memory initData
    ) internal pure returns (address pool) {
        bytes32 salt = keccak256(abi.encodePacked(token));
        bytes32 initCodeHash = keccak256(
            abi.encodePacked(creationCode, abi.encode(beacon, initData))
        );
        pool = address(
            uint160(
                uint256(
                    keccak256(
                        abi.encodePacked(bytes1(0xff), factory, salt, initCodeHash)
                    )
                )
            )
        );
    }

    /// @notice Computes the salt used for pool CREATE2 deployment
    /// @param token The token address
    /// @return salt The salt bytes32
    function computeSalt(address token) internal pure returns (bytes32) {
        return keccak256(abi.encodePacked(token));
    }
}
