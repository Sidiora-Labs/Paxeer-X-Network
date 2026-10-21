// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

/// @title ITreasury
/// @notice Interface for the protocol treasury contract
interface ITreasury {
    // --- Errors ---
    error InsufficientBalance();
    error Unauthorized();
    error ZeroAddress();
    error ZeroAmount();

    // --- Events ---
    event Deposited(address indexed token, address indexed from, uint256 amount);
    event Withdrawn(address indexed token, address indexed to, uint256 amount);

    // --- Functions ---
    function recordDeposit(address token, uint256 amount) external;
    function deposit(address token, uint256 amount) external;
    function withdraw(address token, address to, uint256 amount) external;
    function getBalance(address token) external view returns (uint256);
}
