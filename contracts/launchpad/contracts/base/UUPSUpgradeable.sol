// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

import "./ERC1967Utils.sol";
import "./Initializable.sol";

/// @title UUPSUpgradeable
/// @notice UUPS (Universal Upgradeable Proxy Standard) upgrade logic
/// @dev Implementation contracts inherit this. Proxy delegates to implementation.
///      Authorization for upgrades is defined by subclasses via _authorizeUpgrade.
abstract contract UUPSUpgradeable is Initializable {
    /// @dev keccak256("sidiora.uups.storage") - 1
    bytes32 private constant _UUPS_STORAGE =
        0x8b73c3c69bb8fe3d512ecc4cf759cc79239f7b179b0ffacaa9a75d522b39400f;

    error UnauthorizedUpgrade();
    error UUPSNotThroughProxy();
    error UUPSNotThroughActiveProxy();
    error InvalidImplementation();
    error NonPayableUpgrade();
    error UpgradeInitializationFailed();

    /// @dev Address of this implementation (set at deployment)
    address private immutable __self = address(this);

    /// @notice Checks that the function is called through a proxy
    modifier onlyProxy() {
        if (address(this) == __self) revert UUPSNotThroughProxy();
        if (ERC1967Utils.getImplementation() != __self) revert UUPSNotThroughActiveProxy();
        _;
    }

    /// @notice Checks that the function is NOT called through a proxy
    modifier notDelegated() {
        if (address(this) != __self) revert UUPSNotThroughActiveProxy();
        _;
    }

    /// @notice Returns the ERC-1822 proxiable UUID
    /// @dev Must return the ERC-1967 implementation slot to be compatible
    function proxiableUUID() external view notDelegated returns (bytes32) {
        return ERC1967Utils.IMPLEMENTATION_SLOT;
    }

    /// @notice Upgrades the implementation to a new address
    /// @param newImplementation The address of the new implementation
    /// @param data Optional calldata for initialization after upgrade
    function upgradeToAndCall(
        address newImplementation,
        bytes memory data
    ) external payable virtual onlyProxy {
        _authorizeUpgrade(newImplementation);
        _upgradeToAndCallUUPS(newImplementation, data);
    }

    /// @notice Override this to define who can authorize upgrades
    function _authorizeUpgrade(address newImplementation) internal virtual;

    /// @dev Performs the upgrade, verifying the new implementation is UUPS-compatible
    function _upgradeToAndCallUUPS(
        address newImplementation,
        bytes memory data
    ) internal {
        if (data.length == 0 && msg.value != 0) revert NonPayableUpgrade();
        // Verify the new implementation is a valid UUPS contract
        try UUPSUpgradeable(newImplementation).proxiableUUID() returns (bytes32 slot) {
            if (slot != ERC1967Utils.IMPLEMENTATION_SLOT) {
                revert InvalidImplementation();
            }
        } catch {
            revert InvalidImplementation();
        }

        ERC1967Utils.setImplementation(newImplementation);

        if (data.length > 0) {
            (bool success, bytes memory reason) = newImplementation.delegatecall(data);
            if (!success) {
                if (reason.length == 0) revert UpgradeInitializationFailed();
                assembly { revert(add(reason, 32), mload(reason)) }
            }
        }
    }
}
