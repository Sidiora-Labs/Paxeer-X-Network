// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

/// @dev Adversarial targets exercise incident-call isolation and return bounds.
contract EmergencyPauseTarget {
    uint256 public immutable mode;
    bool private halted;
    constructor(uint256 mode_) { mode = mode_; }
    function emergencyPaused() external view returns (bool) {
        if (mode == 3) assembly { mstore(0, 2) return(0, 32) }
        if (mode == 4) assembly { return(0, 65536) }
        return halted;
    }
    function pause() external {
        if (mode == 1) revert("BROKEN");
        if (mode == 2) return;
        if (mode == 5) assembly { for {} 1 {} {} }
        if (mode == 6) assembly { revert(0, 65536) }
        halted = true;
    }
}
