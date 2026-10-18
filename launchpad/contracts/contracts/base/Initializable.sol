// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

/// @title Initializable
/// @notice Initialization guard for proxied contracts. Replaces constructors.
/// @dev Supports versioned reinitializers and disableInitializers for implementations.
abstract contract Initializable {
    /// @dev Storage slot for initialization state (avoids storage collision in proxies)
    /// keccak256("sidiora.initializable.storage") - 1
    bytes32 private constant _INITIALIZABLE_STORAGE =
        0x4377386e2795aba8c0ce23a1e6e67bac790e4b4166f14b7e9365049ab946f8c0;

    struct InitializableStorage {
        uint64 initialized;
        bool initializing;
    }

    error AlreadyInitialized();
    error NotInitializing();

    event Initialized(uint64 version);

    modifier initializer() {
        InitializableStorage storage $ = _getInitializableStorage();
        bool isTopLevel = !$.initializing;

        uint64 initialized = $.initialized;
        if (initialized >= 1 && isTopLevel) revert AlreadyInitialized();
        if (initialized != 0 && !isTopLevel) revert AlreadyInitialized();

        if (isTopLevel) {
            $.initializing = true;
        }
        $.initialized = 1;
        _;
        if (isTopLevel) {
            $.initializing = false;
            emit Initialized(1);
        }
    }

    modifier reinitializer(uint64 version) {
        InitializableStorage storage $ = _getInitializableStorage();
        if ($.initializing || $.initialized >= version) revert AlreadyInitialized();
        $.initialized = version;
        $.initializing = true;
        _;
        $.initializing = false;
        emit Initialized(version);
    }

    modifier onlyInitializing() {
        if (!_isInitializing()) revert NotInitializing();
        _;
    }

    /// @notice Locks the contract, preventing any future initialization
    /// @dev Call in the constructor of implementation contracts
    function _disableInitializers() internal virtual {
        InitializableStorage storage $ = _getInitializableStorage();
        if ($.initializing) revert AlreadyInitialized();
        if ($.initialized != type(uint64).max) {
            $.initialized = type(uint64).max;
            emit Initialized(type(uint64).max);
        }
    }

    function _getInitializedVersion() internal view returns (uint64) {
        return _getInitializableStorage().initialized;
    }

    function _isInitializing() internal view returns (bool) {
        return _getInitializableStorage().initializing;
    }

    function _getInitializableStorage() private pure returns (InitializableStorage storage $) {
        assembly {
            $.slot := _INITIALIZABLE_STORAGE
        }
    }
}
