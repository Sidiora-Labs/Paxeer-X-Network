// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

import "../libraries/TransferHelper.sol";

/// @notice Wrapper to expose TransferHelper library functions for testing
contract TransferHelperWrapper {
    function safeTransfer(address token, address to, uint256 value) external {
        TransferHelper.safeTransfer(token, to, value);
    }

    function safeTransferFrom(address token, address from, address to, uint256 value) external {
        TransferHelper.safeTransferFrom(token, from, to, value);
    }

    function safeApprove(address token, address spender, uint256 value) external {
        TransferHelper.safeApprove(token, spender, value);
    }
}
