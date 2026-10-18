// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

/// @title IPoolRegistry
/// @notice Interface for on-chain pool discovery and metadata storage
interface IPoolRegistry {
    // --- Errors ---
    error Unauthorized();
    error ZeroAddress();
    error DuplicateToken();
    error DuplicatePool();
    error PoolNotFound();

    // --- Structs ---
    struct PoolMetadata {
        address creator;
        address token;
        address optical;
        uint256 nftId;
        uint256 createdAt;
        uint256 createdBlock;
    }

    // --- Events ---
    event PoolRegistered(
        address indexed pool,
        address indexed token,
        address indexed creator,
        address optical,
        uint256 nftId,
        uint256 timestamp
    );

    // --- Functions ---

    /// @notice Register a new pool (factory-only)
    function register(
        address pool,
        address token,
        address creator,
        address optical,
        uint256 nftId
    ) external;

    /// @notice Get pool address by token address
    function getPoolByToken(address token) external view returns (address);

    /// @notice Get all pools created by a specific address
    function getPoolsByCreator(address creator) external view returns (address[] memory);
    function getPoolCountByCreator(address creator) external view returns (uint256);
    function getPoolsByCreatorPage(address creator, uint256 offset, uint256 limit) external view returns (address[] memory);

    /// @notice Get the NFT token ID associated with a pool
    function getNftIdByPool(address pool) external view returns (uint256);

    /// @notice Get full metadata for a pool
    function getPoolMetadata(address pool) external view returns (PoolMetadata memory);

    /// @notice Get paginated list of all pools
    function getAllPools(uint256 offset, uint256 limit) external view returns (address[] memory);

    /// @notice Get total number of registered pools
    function getPoolCount() external view returns (uint256);

    /// @notice Check if a pool is registered
    function isRegisteredPool(address pool) external view returns (bool);
}
