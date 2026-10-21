// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

/// @notice Minimal stand-in for IOpticalRegistry used by EventEmitter v2
///         auth-mesh tests. Exposes the `isApproved(address)` selector the
///         EventEmitter expects without spinning up the full registry.
contract MockOpticalRegistry {
    mapping(address => bool) private _approved;

    function setApproved(address optical, bool approved) external {
        _approved[optical] = approved;
    }

    function isApproved(address optical) external view returns (bool) {
        return _approved[optical];
    }
}
