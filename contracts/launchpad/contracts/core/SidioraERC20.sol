// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

import "../base/ERC20Base.sol";

interface ITokenTransferPool { function opticalAddress() external view returns (address); }
interface ITokenTransferOptical {
    function getFlags() external view returns (uint8);
    function beforeTokenTransfer(address pool, address from, address to, uint256 amount) external;
}

/// @title SidioraERC20
/// @notice Minimal immutable ERC20 token deployed via CREATE2 by the Factory
/// @dev Total supply minted once in constructor to recipient. No mint/burn after creation.
contract SidioraERC20 is ERC20Base {
    address public immutable deploymentFactory;
    address public pool;
    error InvalidPoolBinding();

    /// @notice One-time binding by deployer before circulating supply is released.
    function bindPool(address pool_) external {
        if (msg.sender != deploymentFactory || pool != address(0) || pool_.code.length == 0) revert InvalidPoolBinding();
        pool = pool_;
    }

    function _transfer(address from, address to, uint256 amount) internal override {
        if (pool != address(0)) {
            address optical = ITokenTransferPool(pool).opticalAddress();
            if (optical != address(0) && (ITokenTransferOptical(optical).getFlags() & 16) != 0) {
                ITokenTransferOptical(optical).beforeTokenTransfer(pool, from, to, amount);
            }
        }
        super._transfer(from, to, amount);
    }

    /// @param _name Token name
    /// @param _symbol Token symbol
    /// @param _totalSupply Total supply to mint (6 decimals)
    /// @param _recipient Address to receive the entire supply (the pool)
    constructor(
        string memory _name,
        string memory _symbol,
        uint256 _totalSupply,
        address _recipient
    ) ERC20Base(_name, _symbol, 6) {
        deploymentFactory = msg.sender;
        if (_recipient == address(0)) revert ZeroAddress();
        _mint(_recipient, _totalSupply);
    }
}
