// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

import "../base/ReentrancyGuard.sol";

contract MockReentrancyGuard is ReentrancyGuard {
    uint256 public counter;

    function protectedIncrement() external nonReentrant {
        counter++;
    }

    function unprotectedIncrement() external {
        counter++;
    }

    function reentrantCall() external nonReentrant {
        // Try to call protectedIncrement again (should fail)
        this.protectedIncrement();
    }

    function crossFunctionReentrantCall() external nonReentrant {
        // Try to call another protected function
        this.protectedIncrement();
    }
}

contract ReentrancyAttacker {
    MockReentrancyGuard public target;
    uint256 public attackCount;

    constructor(address _target) {
        target = MockReentrancyGuard(_target);
    }

    function attack() external {
        target.protectedIncrement();
    }

    receive() external payable {
        if (attackCount < 3) {
            attackCount++;
            target.protectedIncrement();
        }
    }
}
