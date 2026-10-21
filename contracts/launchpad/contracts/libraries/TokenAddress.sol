// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

/// @title TokenAddress
/// @notice Computes deterministic SidioraERC20 token addresses from CREATE2 parameters
/// @dev Used by Factory, Router, and Quoter to predict token addresses before deployment
library TokenAddress {
    /// @notice Computes the CREATE2 address for a SidioraERC20 token
    /// @param factory The factory contract address (deployer)
    /// @param creator The market creator address
    /// @param name Token name
    /// @param symbol Token symbol
    /// @param nonce Creator's nonce for uniqueness
    /// @param creationCode The SidioraERC20 creation code (without constructor args)
    /// @param totalSupply Token total supply
    /// @param recipient Initial token recipient (the pool)
    /// @return token The deterministic token address
    function computeAddress(
        address factory,
        address creator,
        string memory name,
        string memory symbol,
        uint256 nonce,
        bytes memory creationCode,
        uint256 totalSupply,
        address recipient
    ) internal pure returns (address token) {
        bytes32 salt = computeSalt(creator, name, symbol, nonce);
        bytes32 initCodeHash = keccak256(
            abi.encodePacked(creationCode, abi.encode(name, symbol, totalSupply, recipient))
        );
        token = address(
            uint160(
                uint256(
                    keccak256(
                        abi.encodePacked(bytes1(0xff), factory, salt, initCodeHash)
                    )
                )
            )
        );
    }

    /// @notice Computes the salt for token CREATE2 deployment
    /// @param creator The market creator
    /// @param name Token name
    /// @param symbol Token symbol
    /// @param nonce Creator's nonce
    /// @return salt The salt bytes32
    function computeSalt(
        address creator,
        string memory name,
        string memory symbol,
        uint256 nonce
    ) internal pure returns (bytes32) {
        return keccak256(abi.encodePacked(creator, name, symbol, nonce));
    }
}
