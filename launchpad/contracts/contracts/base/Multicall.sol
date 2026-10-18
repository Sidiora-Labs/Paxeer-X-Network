// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

/// @title Multicall
/// @notice Batch multiple calls in a single transaction
/// @dev Enables batching of multiple function calls to this contract
abstract contract Multicall {
    error MulticallFailed(uint256 index, bytes reason);

    /// @notice Executes multiple calls in a single transaction
    /// @param data Array of encoded function calls
    /// @return results Array of return data from each call
    function multicall(bytes[] calldata data) external returns (bytes[] memory results) {
        results = new bytes[](data.length);
        for (uint256 i = 0; i < data.length; i++) {
            (bool success, bytes memory result) = address(this).delegatecall(data[i]);
            if (!success) {
                revert MulticallFailed(i, result);
            }
            results[i] = result;
        }
    }
}
