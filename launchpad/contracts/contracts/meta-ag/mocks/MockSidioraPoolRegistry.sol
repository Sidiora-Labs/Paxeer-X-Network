// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.27;

/// @title MockSidioraPoolRegistry
/// @notice Minimal Sidiora pool registry used by SidioraFeedAdapter tests.
/// @dev Matches the live `IPoolRegistry.getPoolByToken(address)` selector only; all other
///      registry methods are out of scope for the Phase 2 oracle tests.
contract MockSidioraPoolRegistry {
    mapping(address => address) private _poolByToken;
    bool public revertOnLookup;

    function setPoolByToken(address token, address pool) external {
        _poolByToken[token] = pool;
    }

    function setRevertOnLookup(bool v) external {
        revertOnLookup = v;
    }

    function getPoolByToken(address token) external view returns (address) {
        if (revertOnLookup) revert("MockSidioraPoolRegistry: forced revert");
        return _poolByToken[token];
    }
}
