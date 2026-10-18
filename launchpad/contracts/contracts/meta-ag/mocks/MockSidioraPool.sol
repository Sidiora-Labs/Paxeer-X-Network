// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.27;

/// @title MockSidioraPool
/// @notice Minimal Sidiora-style pool used by SidioraFeedAdapter unit tests.
/// @dev Returns configurable price + reserves; optional forced revert on either view.
contract MockSidioraPool {
    address public opticalAddress;
    function setOpticalAddress(address optical) external { opticalAddress = optical; }
    uint256 public price;
    uint256 public virtualUsdl;
    uint256 public realUsdl;
    uint256 public tokenReserve;
    bool public revertOnGetPrice;
    bool public revertOnGetReserves;

    function setPrice(uint256 p) external {
        price = p;
    }

    function setReserves(uint256 v, uint256 r, uint256 t) external {
        virtualUsdl = v;
        realUsdl = r;
        tokenReserve = t;
    }

    function setRevertOnGetPrice(bool v) external {
        revertOnGetPrice = v;
    }

    function setRevertOnGetReserves(bool v) external {
        revertOnGetReserves = v;
    }

    function getPrice() external view returns (uint256) {
        if (revertOnGetPrice) revert("MockSidioraPool: forced revert");
        return price;
    }

    function getReserves() external view returns (uint256, uint256, uint256) {
        if (revertOnGetReserves) revert("MockSidioraPool: forced revert");
        return (virtualUsdl, realUsdl, tokenReserve);
    }
}
