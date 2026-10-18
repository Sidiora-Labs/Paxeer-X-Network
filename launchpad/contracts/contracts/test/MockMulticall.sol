// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

import "../base/Multicall.sol";

contract MockMulticall is Multicall {
    uint256 public value1;
    uint256 public value2;

    function setValue1(uint256 _val) external {
        value1 = _val;
    }

    function setValue2(uint256 _val) external {
        value2 = _val;
    }

    function getValue1() external view returns (uint256) {
        return value1;
    }

    function revertingFunction() external pure {
        revert("intentional revert");
    }
}
