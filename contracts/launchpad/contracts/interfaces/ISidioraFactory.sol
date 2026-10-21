// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

/// @title ISidioraFactory
/// @notice Interface for the market creation orchestrator
interface ISidioraFactory {
    // --- Errors ---
    error ZeroAddress();
    error InsufficientCreationFee();
    error DuplicateToken();

    // --- Events ---
    event MarketCreated(
        address indexed token,
        address indexed pool,
        address indexed creator,
        uint256 nftId,
        address optical
    );

    // --- Functions ---

    /// @notice Create a new market (token + pool + NFT)
    /// @param name Token name
    /// @param symbol Token symbol
    /// @param feeStrategy Initial fee strategy for the pool NFT (0=CLAIM,1=BURN,2=AIRDROP,3=LP_REWARDS)
    /// @param optical Optional optical hook contract (address(0) for none)
    /// @return tokenAddr The deployed token address
    /// @return poolAddr The deployed pool address
    /// @return nftId The minted NFT token ID
    function createMarket(
        string calldata name,
        string calldata symbol,
        uint8 feeStrategy,
        address optical
    ) external returns (address tokenAddr, address poolAddr, uint256 nftId);

    /// @notice Create a new market on behalf of a creator (Router-only via ROUTER_ROLE)
    /// @param creator The actual creator address (receives NFT, becomes guardian)
    /// @param name Token name
    /// @param symbol Token symbol
    /// @param feeStrategy Initial fee strategy for the pool NFT
    /// @param optical Optional optical hook contract (address(0) for none)
    /// @return tokenAddr The deployed token address
    /// @return poolAddr The deployed pool address
    /// @return nftId The minted NFT token ID
    function createMarketFor(
        address creator,
        string calldata name,
        string calldata symbol,
        uint8 feeStrategy,
        address optical
    ) external returns (address tokenAddr, address poolAddr, uint256 nftId);

    /// @notice Get the nonce for a creator (used for CREATE2 determinism)
    function getNonce(address creator) external view returns (uint256);

    /// @notice Get the pool beacon address
    function poolBeacon() external view returns (address);

    /// @notice Get the NFT contract address
    function nftContract() external view returns (address);

    /// @notice Get the pool registry address
    function poolRegistry() external view returns (address);

    /// @notice Get the event emitter address
    function eventEmitter() external view returns (address);

    /// @notice Get the protocol config address
    function protocolConfig() external view returns (address);

    /// @notice Get the treasury address
    function treasury() external view returns (address);
}
