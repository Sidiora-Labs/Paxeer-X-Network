// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;
// Private local-chain fixture; not a production USDL implementation.
contract FundingToken {
    uint8 public immutable decimals;
    mapping(address => uint256) public balanceOf;
    event Transfer(address indexed from, address indexed to, uint256 value);
    constructor(uint8 precision) { decimals = precision; balanceOf[msg.sender] = 1000000000000; }
    function transfer(address to, uint256 value) external returns (bool) {
        require(to != address(0) && balanceOf[msg.sender] >= value, "invalid transfer");
        balanceOf[msg.sender] -= value;
        balanceOf[to] += value;
        emit Transfer(msg.sender, to, value);
        return true;
    }
}
contract RejectNative { receive() external payable { revert("rejected"); } }
