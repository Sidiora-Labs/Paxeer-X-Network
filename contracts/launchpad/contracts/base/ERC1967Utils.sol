// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

/// @title ERC1967Utils
/// @notice Storage slot helpers for ERC-1967 proxy patterns
/// @dev Defines standard storage slots for implementation, admin, and beacon addresses
library ERC1967Utils {
    /// @dev bytes32(uint256(keccak256("eip1967.proxy.implementation")) - 1)
    bytes32 internal constant IMPLEMENTATION_SLOT =
        0x360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc;

    /// @dev bytes32(uint256(keccak256("eip1967.proxy.beacon")) - 1)
    bytes32 internal constant BEACON_SLOT =
        0xa3f0ad74e5423aebfd80d3ef4346578335a9a72aeaee59ff6cb3582b35133d50;

    /// @dev bytes32(uint256(keccak256("eip1967.proxy.admin")) - 1)
    bytes32 internal constant ADMIN_SLOT =
        0xb53127684a568b3173ae13b9f8a6016e243e63b6e8ee1178d6a717850b5d6103;

    error InvalidImplementation();
    error InvalidBeacon();

    event Upgraded(address indexed implementation);
    event BeaconUpgraded(address indexed beacon);
    event AdminChanged(address previousAdmin, address newAdmin);

    /// @notice Returns the current implementation address
    function getImplementation() internal view returns (address impl) {
        assembly {
            impl := sload(IMPLEMENTATION_SLOT)
        }
    }

    /// @notice Sets the implementation address
    function setImplementation(address newImplementation) internal {
        if (newImplementation.code.length == 0) revert InvalidImplementation();
        assembly {
            sstore(IMPLEMENTATION_SLOT, newImplementation)
        }
        emit Upgraded(newImplementation);
    }

    /// @notice Returns the current beacon address
    function getBeacon() internal view returns (address beacon) {
        assembly {
            beacon := sload(BEACON_SLOT)
        }
    }

    /// @notice Sets the beacon address
    function setBeacon(address newBeacon) internal {
        if (newBeacon.code.length == 0) revert InvalidBeacon();
        assembly {
            sstore(BEACON_SLOT, newBeacon)
        }
        emit BeaconUpgraded(newBeacon);
    }

    /// @notice Returns the current admin address
    function getAdmin() internal view returns (address admin) {
        assembly {
            admin := sload(ADMIN_SLOT)
        }
    }

    /// @notice Sets the admin address
    function setAdmin(address newAdmin) internal {
        address previous = getAdmin();
        assembly {
            sstore(ADMIN_SLOT, newAdmin)
        }
        emit AdminChanged(previous, newAdmin);
    }
}
