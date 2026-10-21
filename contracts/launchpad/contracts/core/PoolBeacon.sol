// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

import "../base/UpgradeableBeacon.sol";

/// @title PoolBeacon
/// @notice Stores the SidioraPool implementation address. All pool proxies read from this.
/// @dev Thin wrapper around UpgradeableBeacon. Owner = Timelock.
///      Upgrading this ONE contract upgrades ALL pools atomically.
contract PoolBeacon is UpgradeableBeacon {
    constructor(
        address initialImplementation,
        address initialOwner
    ) UpgradeableBeacon(initialImplementation, initialOwner) {}
}
