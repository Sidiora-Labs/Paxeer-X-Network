// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

/// @title ISidioraNFT
/// @notice Interface for the ERC721 contract representing fee rights per pool
interface ISidioraNFT {
    // --- Errors ---
    error InvalidStrategy();

    // --- Events ---
    event FeeStrategyChanged(uint256 indexed tokenId, uint8 oldStrategy, uint8 newStrategy);
    event PoolNFTMinted(uint256 indexed tokenId, address indexed creator, address indexed pool);

    // --- Functions ---

    /// @notice Mint a new pool NFT (factory-only via MINTER_ROLE)
    /// @param to The NFT recipient (pool creator)
    /// @param pool The pool address this NFT represents
    /// @param strategy Initial fee strategy
    /// @return tokenId The minted token ID
    function mint(address to, address pool, uint8 strategy) external returns (uint256 tokenId);

    /// @notice Get the fee strategy for a token
    function getFeeStrategy(uint256 tokenId) external view returns (uint8);

    /// @notice Set the fee strategy for a token (caller must be owner or approved)
    function setFeeStrategy(uint256 tokenId, uint8 newStrategy) external;

    /// @notice Get the pool address associated with an NFT
    function getPoolAddress(uint256 tokenId) external view returns (address);

    /// @notice Get current token ID counter
    function nextTokenId() external view returns (uint256);
}
