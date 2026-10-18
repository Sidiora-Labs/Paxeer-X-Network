// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

import "./Proxy.sol";
import "./ERC1967Utils.sol";

/// @title BeaconProxy
/// @notice Proxy that reads its implementation from a beacon contract
/// @dev Multiple BeaconProxy instances share one UpgradeableBeacon.
///      Upgrading the beacon upgrades all proxies atomically.
contract BeaconProxy is Proxy {
    error BeaconCallFailed();
    error BeaconInitializationFailed();

    /// @param beacon The UpgradeableBeacon address
    /// @param data Optional initialization calldata
    constructor(address beacon, bytes memory data) {
        ERC1967Utils.setBeacon(beacon);
        address impl = _getBeaconImplementation(beacon);
        if (data.length > 0) {
            (bool success, bytes memory reason) = impl.delegatecall(data);
            if (!success) {
                if (reason.length == 0) revert BeaconInitializationFailed();
                assembly { revert(add(reason, 32), mload(reason)) }
            }
        }
    }

    /// @notice Returns the current implementation by querying the beacon
    function _implementation() internal view override returns (address) {
        return _getBeaconImplementation(ERC1967Utils.getBeacon());
    }

    /// @dev Reads the implementation() function from the beacon
    function _getBeaconImplementation(address beacon) internal view returns (address impl) {
        (bool success, bytes memory data) = beacon.staticcall(
            abi.encodeWithSignature("implementation()")
        );
        if (!success || data.length < 32) revert BeaconCallFailed();
        impl = abi.decode(data, (address));
        if (impl.code.length == 0) revert BeaconCallFailed();
    }
}
