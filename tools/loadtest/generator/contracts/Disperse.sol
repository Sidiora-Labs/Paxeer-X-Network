// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

interface IERC20 {
    function transfer(address to, uint256 value) external returns (bool);
    function transferFrom(
        address from,
        address to,
        uint256 value
    ) external returns (bool);
}

contract Disperse {
    address public owner;
    uint256 public fixedEtherAmount;
    uint256 public fixedTokenAmount;

    constructor(uint256 etherAmount, uint256 tokenAmount) {
        owner = msg.sender;
        fixedEtherAmount = etherAmount;
        fixedTokenAmount = tokenAmount;
    }

    function disperseEther(
        address[] calldata recipients,
        uint256[] calldata values
    ) external payable {
        for (uint256 i = 0; i < recipients.length; i++)
            payable(recipients[i]).transfer(values[i]);
        uint256 balance = address(this).balance;
        if (balance > 0) payable(msg.sender).transfer(balance);
    }

    function disperseEtherFixed(address[] calldata recipients) external payable {
        require(msg.value == fixedEtherAmount * recipients.length);
        for (uint256 i = 0; i < recipients.length; i++)
            payable(recipients[i]).transfer(fixedEtherAmount);
        uint256 balance = address(this).balance;
        if (balance > 0) payable(msg.sender).transfer(balance);
    }

    function disperseToken(
        IERC20 token,
        address[] calldata recipients,
        uint256[] calldata values
    ) external {
        uint256 total = 0;
        for (uint256 i = 0; i < recipients.length; i++) total += values[i];
        require(token.transferFrom(msg.sender, address(this), total));
        for (uint256 i = 0; i < recipients.length; i++)
            require(token.transfer(recipients[i], values[i]));
    }

    function disperseTokenFixed(IERC20 token, address[] calldata recipients) external {
        uint256 total = fixedTokenAmount * recipients.length;
        require(token.transferFrom(msg.sender, address(this), total));
        for (uint256 i = 0; i < recipients.length; i++)
            require(token.transfer(recipients[i], fixedTokenAmount));
    }

    function disperseTokenSimple(
        IERC20 token,
        address[] calldata recipients,
        uint256[] calldata values
    ) external {
        for (uint256 i = 0; i < recipients.length; i++)
            require(token.transferFrom(msg.sender, recipients[i], values[i]));
    }

    function setFixedEtherAmount(uint256 amount) external {
        require(msg.sender == owner);
        fixedEtherAmount = amount;
    }

    function setFixedTokenAmount(uint256 amount) external {
        require(msg.sender == owner);
        fixedTokenAmount = amount;
    }

    function transferOwnership(address newOwner) external {
        require(msg.sender == owner);
        owner = newOwner;
    }
}
