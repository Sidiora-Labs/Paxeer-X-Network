// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

/// @notice Minimal stand-in for IPoolRegistry used by EventEmitter v2
///         auth-mesh tests. Allows the test to mark arbitrary addresses
///         as "registered pools" without spinning up the full Launchpad
///         deployment graph (Factory → NFT → PoolRegistry).
contract MockPoolRegistry {
    mapping(address => bool) private _registered;

    function setRegistered(address pool, bool registered) external {
        _registered[pool] = registered;
    }

    function isRegisteredPool(address pool) external view returns (bool) {
        return _registered[pool];
    }
}
