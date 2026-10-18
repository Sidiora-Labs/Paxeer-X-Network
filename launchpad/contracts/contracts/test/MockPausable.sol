// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

import "../base/Pausable.sol";

contract MockPausable is Pausable {
    uint256 public value;

    function protectedAction(uint256 _val) external whenNotPaused {
        value = _val;
    }

    function emergencyAction() external whenPaused returns (bool) {
        return true;
    }

    function pause() external {
        _pause();
    }

    function unpause() external {
        _unpause();
    }

    function paused() external view returns (bool) {
        return _paused();
    }
}
