// SPDX-License-Identifier: MIT
pragma solidity ^0.8.27;

/**
 * @title WPAX9
 * @notice Wrapped Paxeer — 1:1 wrapped version of the native PAXEER token
 * @dev Modern Solidity 0.8.x port of canonical WETH9 behavior
 */
contract WPAX9 {
    string public constant name = "Wrapped Paxeer";
    string public constant symbol = "WPAX";
    uint8 public constant decimals = 18;

    event Approval(address indexed src, address indexed guy, uint256 wad);
    event Transfer(address indexed src, address indexed dst, uint256 wad);
    event Deposit(address indexed dst, uint256 wad);
    event Withdrawal(address indexed src, uint256 wad);

    mapping(address => uint256) public balanceOf;
    mapping(address => mapping(address => uint256)) public allowance;

    // ============ Fallback & Deposit ============

    receive() external payable {
        deposit();
    }

    fallback() external payable {
        deposit();
    }

    function deposit() public payable {
        balanceOf[msg.sender] += msg.value;
        emit Deposit(msg.sender, msg.value);
    }

    // ============ Withdraw ============

    function withdraw(uint256 wad) public {
        require(balanceOf[msg.sender] >= wad, "WPAX9: insufficient balance");
        balanceOf[msg.sender] -= wad;
        (bool success, ) = payable(msg.sender).call{value: wad}("");
        require(success, "WPAX9: native transfer failed");
        emit Withdrawal(msg.sender, wad);
    }

    // ============ ERC20 Functions ============

    function totalSupply() public view returns (uint256) {
        return address(this).balance;
    }

    function approve(address guy, uint256 wad) public returns (bool) {
        allowance[msg.sender][guy] = wad;
        emit Approval(msg.sender, guy, wad);
        return true;
    }

    function transfer(address dst, uint256 wad) public returns (bool) {
        return transferFrom(msg.sender, dst, wad);
    }

    function transferFrom(address src, address dst, uint256 wad) public returns (bool) {
        require(balanceOf[src] >= wad, "WPAX9: insufficient balance");

        if (src != msg.sender && allowance[src][msg.sender] != type(uint256).max) {
            require(allowance[src][msg.sender] >= wad, "WPAX9: insufficient allowance");
            allowance[src][msg.sender] -= wad;
        }

        balanceOf[src] -= wad;
        balanceOf[dst] += wad;

        emit Transfer(src, dst, wad);
        return true;
    }
}