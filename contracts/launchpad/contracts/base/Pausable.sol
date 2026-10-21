// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

/// @title Pausable
/// @notice Emergency pause mechanism with whenNotPaused/whenPaused modifiers
abstract contract Pausable {
    bytes32 public constant PAUSER_ROLE = keccak256("PAUSER_ROLE");

    /// @notice Canonical incident-controller status; does not expose recovery authority.
    function emergencyPaused() external view returns (bool) { return _paused(); }

    /// @dev keccak256("sidiora.pausable.storage") - 1
    bytes32 private constant _PAUSABLE_SLOT =
        0x62e78cea01bee320cd4e420270b5ea74000d11b0c9f74754ebdbfc544b05a258;

    error Paused();
    error NotPaused();

    event PauseToggled(bool paused);

    modifier whenNotPaused() {
        if (_paused()) revert Paused();
        _;
    }

    modifier whenPaused() {
        if (!_paused()) revert NotPaused();
        _;
    }

    function _paused() internal view returns (bool paused_) {
        bytes32 slot = _PAUSABLE_SLOT;
        assembly {
            paused_ := sload(slot)
        }
    }

    function _pause() internal whenNotPaused {
        bytes32 slot = _PAUSABLE_SLOT;
        assembly {
            sstore(slot, 1)
        }
        emit PauseToggled(true);
    }

    function _unpause() internal whenPaused {
        bytes32 slot = _PAUSABLE_SLOT;
        assembly {
            sstore(slot, 0)
        }
        emit PauseToggled(false);
    }
}
