// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

/// @title Proxy
/// @notice Base delegatecall proxy. Subclasses must implement _implementation().
/// @dev All calls are forwarded to the implementation via delegatecall.
abstract contract Proxy {
    /// @notice Returns the current implementation address
    function _implementation() internal view virtual returns (address);

    /// @notice Delegates the current call to the implementation
    function _delegate(address implementation) internal virtual {
        assembly {
            calldatacopy(0, 0, calldatasize())
            let result := delegatecall(gas(), implementation, 0, calldatasize(), 0, 0)
            returndatacopy(0, 0, returndatasize())
            switch result
            case 0 { revert(0, returndatasize()) }
            default { return(0, returndatasize()) }
        }
    }

    /// @notice Fallback function delegates all calls to the implementation
    fallback() external payable virtual {
        _delegate(_implementation());
    }

    /// @notice Receive function delegates to the implementation
    receive() external payable virtual {
        _delegate(_implementation());
    }
}
