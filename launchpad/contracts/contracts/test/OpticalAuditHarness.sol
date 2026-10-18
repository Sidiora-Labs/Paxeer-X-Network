// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

/// @dev Test-only dispatch harness: deliberately unrestricted so tests exercise production authentication.
contract OpticalAuditHarness {
    address public tokenAddress;
    address public feeAccumulator;
    uint256 public virtualReserve = 1000000;
    constructor(address token, address accumulator) { tokenAddress = token; feeAccumulator = accumulator; }
    function callTarget(address target, bytes calldata payload) external returns (bytes memory result) {
        (bool ok, bytes memory data) = target.call(payload);
        if (!ok) assembly { revert(add(data, 32), mload(data)) }
        return data;
    }
    function getReserves() external view returns (uint256, uint256, uint256) { return (virtualReserve, 0, 1000000); }
}
