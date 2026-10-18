// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

/// @title MockPoolForOptical
/// @notice Minimal mock pool that returns configurable reserves for optical testing
contract MockPoolForOptical {
    address public feeAccumulator;
    address public opticalAddress;
    function setFeeAccumulator(address value) external { feeAccumulator = value; }
    function setOpticalAddress(address value) external { opticalAddress = value; }
    uint256 private _virtualUsdl;
    uint256 private _realUsdl;
    uint256 private _tokenReserve;
    address private _tokenAddress;

    function setReserves(uint256 virtualUsdl, uint256 realUsdl, uint256 tokenRes) external {
        _virtualUsdl = virtualUsdl;
        _realUsdl = realUsdl;
        _tokenReserve = tokenRes;
    }

    function setTokenAddress(address token) external {
        _tokenAddress = token;
    }

    function getReserves() external view returns (uint256, uint256, uint256) {
        return (_virtualUsdl, _realUsdl, _tokenReserve);
    }

    function getEffectiveReserves() external view returns (uint256, uint256) {
        return (_virtualUsdl + _realUsdl, _tokenReserve);
    }

    function tokenAddress() external view returns (address) {
        return _tokenAddress;
    }

    function creationTimestamp() external view returns (uint256) {
        return block.timestamp;
    }
}
