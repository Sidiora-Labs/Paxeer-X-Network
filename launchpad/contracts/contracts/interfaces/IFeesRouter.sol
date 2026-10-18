// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

/// @title IFeesRouter
/// @notice Interface for NFT-holder fee management
interface IFeesRouter {
    error UnsupportedSnapshotToken();
    // --- Errors ---
    error NotNftOwner();
    error WrongStrategy();
    error ZeroAddress();

    // --- Events ---
    event FeesClaimed(uint256 indexed nftId, address indexed owner, uint256 amount);
    event FeesBurned(uint256 indexed nftId, uint256 amount);
    event AirdropExecuted(uint256 indexed nftId, uint256 amount);
    event AirdropClaimed(uint256 indexed nftId, address indexed holder, uint256 amount);
    event LpRewardsExecuted(uint256 indexed nftId, uint256 amount);
    event FeeStrategyChanged(uint256 indexed nftId, uint8 oldStrategy, uint8 newStrategy);

    // --- Functions ---

    /// @notice Set fee strategy for a pool NFT (owner-only)
    /// @param nftId The NFT token ID
    /// @param newStrategy New fee strategy (0=CLAIM,1=BURN,2=AIRDROP,3=LP_REWARDS)
    function setFeeStrategy(uint256 nftId, uint8 newStrategy) external;

    /// @notice Claim accumulated fees (CLAIM strategy only, NFT owner)
    /// @param nftId The NFT token ID
    /// @return amount USDL amount claimed
    function claimFees(uint256 nftId) external returns (uint256 amount);

    /// @notice Execute burn of accumulated fees (BURN strategy only, NFT owner)
    /// @param nftId The NFT token ID
    /// @return amount USDL amount burned
    function executeBurn(uint256 nftId) external returns (uint256 amount);

    /// @notice Execute airdrop distribution (AIRDROP strategy only, NFT owner)
    /// @param nftId The NFT token ID
    /// @return amount Total USDL distributed
    function executeAirdrop(uint256 nftId) external returns (uint256 amount);

    /// @notice Claim airdrop share as a token holder
    /// @param nftId The NFT token ID for the pool
    /// @return amount USDL amount claimed
    function claimAirdrop(uint256 nftId) external returns (uint256 amount);
    function claimAirdropEpoch(uint256 nftId, uint256 epoch) external returns (uint256 amount);

    /// @notice Execute LP rewards transfer (LP_REWARDS strategy only, NFT owner)
    /// @param nftId The NFT token ID
    /// @return amount USDL amount sent to pool
    function executeLpRewards(uint256 nftId) external returns (uint256 amount);
}
