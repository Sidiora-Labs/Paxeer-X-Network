// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

/// @title TransferHelper
/// @notice Safe ERC20 transfer wrappers that handle non-standard return values
/// @dev Handles tokens that return bool, return nothing, or revert
library TransferHelper {
    error TransferFailed();
    error TransferFromFailed();
    error ApproveFailed();

    function _validReturn(bytes memory data) private pure returns (bool) {
        if (data.length == 0) return true;
        if (data.length != 32) return false;
        uint256 result;
        assembly { result := mload(add(data, 32)) }
        return result == 1;
    }

    /// @notice Safely transfers tokens (handles non-standard ERC20s)
    function safeTransfer(address token, address to, uint256 value) internal {
        (bool success, bytes memory data) = token.call(
            abi.encodeWithSelector(0xa9059cbb, to, value) // transfer(address,uint256)
        );
        if (token.code.length == 0 || !success || !_validReturn(data)) {
            revert TransferFailed();
        }
    }

    /// @notice Safely transfers tokens from a sender (handles non-standard ERC20s)
    function safeTransferFrom(
        address token,
        address from,
        address to,
        uint256 value
    ) internal {
        (bool success, bytes memory data) = token.call(
            abi.encodeWithSelector(0x23b872dd, from, to, value) // transferFrom(address,address,uint256)
        );
        if (token.code.length == 0 || !success || !_validReturn(data)) {
            revert TransferFromFailed();
        }
    }

    /// @notice Safely approves a spender (handles non-standard ERC20s)
    function safeApprove(address token, address spender, uint256 value) internal {
        (bool success, bytes memory data) = token.call(
            abi.encodeWithSelector(0x095ea7b3, spender, value) // approve(address,uint256)
        );
        if (token.code.length == 0 || !success || !_validReturn(data)) {
            revert ApproveFailed();
        }
    }
}
