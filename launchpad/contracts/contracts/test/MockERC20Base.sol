// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

import "../base/ERC20Base.sol";

contract MockERC20Base is ERC20Base {
    constructor(string memory _name, string memory _symbol, uint8 _decimals)
        ERC20Base(_name, _symbol, _decimals)
    {}

    function mint(address to, uint256 amount) external {
        _mint(to, amount);
    }

    function burn(address from, uint256 amount) external {
        _burn(from, amount);
    }
}
