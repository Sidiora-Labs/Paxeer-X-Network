// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

/// @title ReentrancyGuard
/// @notice Prevents reentrant calls to protected functions
/// @dev Uses a storage slot flag to detect and block reentrancy
abstract contract ReentrancyGuard {
    /// @dev keccak256("sidiora.reentrancyguard.storage") - 1
    bytes32 private constant _REENTRANCY_SLOT =
        0x9b779b17422d0df92223018b32b4d1fa46e071723d6817e2486d003becc55f00;

    uint256 private constant _NOT_ENTERED = 1;
    uint256 private constant _ENTERED = 2;

    error ReentrancyGuardReentrantCall();

    constructor() {
        _setStatus(_NOT_ENTERED);
    }

    modifier nonReentrant() {
        _nonReentrantBefore();
        _;
        _nonReentrantAfter();
    }

    function _nonReentrantBefore() private {
        if (_getStatus() == _ENTERED) revert ReentrancyGuardReentrantCall();
        _setStatus(_ENTERED);
    }

    function _nonReentrantAfter() private {
        _setStatus(_NOT_ENTERED);
    }

    function _getStatus() private view returns (uint256 status) {
        bytes32 slot = _REENTRANCY_SLOT;
        assembly {
            status := sload(slot)
        }
    }

    function _setStatus(uint256 status) private {
        bytes32 slot = _REENTRANCY_SLOT;
        assembly {
            sstore(slot, status)
        }
    }

    /// @dev Initialize reentrancy guard for proxied contracts (no constructor)
    function _initReentrancyGuard() internal {
        _setStatus(_NOT_ENTERED);
    }
}
