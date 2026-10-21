// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

/// @title UpgradeableBeacon
/// @notice Stores and upgrades the implementation address for beacon proxies
/// @dev Owner can upgrade. All BeaconProxy instances read implementation from here.
contract UpgradeableBeacon {
    address private _implementation;
    address private _owner;
    address public pendingOwner;

    error InvalidImplementation();
    error Unauthorized();
    error ZeroAddress();

    event Upgraded(address indexed implementation);
    event OwnershipTransferStarted(address indexed previousOwner, address indexed pendingOwner);
    event OwnershipTransferred(address indexed previousOwner, address indexed newOwner);

    constructor(address initialImplementation, address initialOwner) {
        if (initialOwner == address(0)) revert ZeroAddress();
        _setImplementation(initialImplementation);
        _owner = initialOwner;
        emit OwnershipTransferred(address(0), initialOwner);
    }

    modifier onlyOwner() {
        if (msg.sender != _owner) revert Unauthorized();
        _;
    }

    /// @notice Returns the current implementation address
    function implementation() public view returns (address) {
        return _implementation;
    }

    /// @notice Returns the current owner
    function owner() public view returns (address) {
        return _owner;
    }

    /// @notice Upgrades the implementation to a new address
    function upgradeTo(address newImplementation) external onlyOwner {
        _setImplementation(newImplementation);
    }

    /// @notice Transfers ownership to a new address
    function transferOwnership(address newOwner) external onlyOwner {
        if (newOwner == address(0)) revert ZeroAddress();
        pendingOwner = newOwner;
        emit OwnershipTransferStarted(_owner, newOwner);
    }

    function acceptOwnership() external {
        if (msg.sender != pendingOwner) revert Unauthorized();
        address old = _owner;
        _owner = msg.sender;
        pendingOwner = address(0);
        emit OwnershipTransferred(old, msg.sender);
    }

    function _setImplementation(address newImplementation) private {
        if (newImplementation.code.length == 0) revert InvalidImplementation();
        _implementation = newImplementation;
        emit Upgraded(newImplementation);
    }
}
