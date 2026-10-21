// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

/// @title IRouter
/// @notice Interface for the user-facing entry point for all protocol interactions
interface IRouter {
    // --- Errors ---
    error ZeroAddress();
    error ZeroAmount();
    error DeadlineExpired();
    error PoolNotFound();
    error InsufficientBalance();
    error SameToken();

    // --- Structs ---

    /// @notice EIP-2612 permit parameters for gasless approvals
    struct PermitParams {
        uint256 value;
        uint256 deadline;
        uint8 v;
        bytes32 r;
        bytes32 s;
    }

    // --- Events ---
    event MarketCreated(
        address indexed token,
        address indexed pool,
        address indexed creator,
        uint256 nftId
    );

    event Buy(
        address indexed pool,
        address indexed buyer,
        uint256 usdlIn,
        uint256 tokensOut
    );

    event Sell(
        address indexed pool,
        address indexed seller,
        uint256 tokensIn,
        uint256 usdlOut
    );

    event MultihopSwap(
        address indexed sender,
        address indexed tokenIn,
        address indexed tokenOut,
        uint256 amountIn,
        uint256 intermediateUsdl,
        uint256 amountOut
    );

    // --- Core Functions ---

    /// @notice Create a new market (token + pool + NFT)
    /// @param name Token name
    /// @param symbol Token symbol
    /// @param feeStrategy Initial fee strategy (0=CLAIM,1=BURN,2=AIRDROP,3=LP_REWARDS)
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

    /// @notice Buy tokens with USDL
    /// @param pool The pool address to buy from
    /// @param usdlAmountIn Amount of USDL to spend
    /// @param minTokensOut Minimum tokens to receive (slippage protection)
    /// @param deadline Transaction deadline timestamp
    /// @return amountOut Actual tokens received
    function buy(
        address pool,
        uint256 usdlAmountIn,
        uint256 minTokensOut,
        uint256 deadline
    ) external returns (uint256 amountOut);

    /// @notice Sell tokens for USDL
    /// @param pool The pool address to sell to
    /// @param tokenAmountIn Amount of tokens to sell
    /// @param minUsdlOut Minimum USDL to receive (slippage protection)
    /// @param deadline Transaction deadline timestamp
    /// @return amountOut Actual USDL received
    function sell(
        address pool,
        uint256 tokenAmountIn,
        uint256 minUsdlOut,
        uint256 deadline
    ) external returns (uint256 amountOut);

    // --- Multihop ---

    /// @notice Swap Token A → USDL → Token B in a single transaction
    /// @param tokenIn Address of the token to sell
    /// @param tokenOut Address of the token to buy
    /// @param amountIn Amount of tokenIn to sell
    /// @param minAmountOut Minimum tokenOut to receive (end-to-end slippage protection)
    /// @param deadline Transaction deadline timestamp
    /// @return amountOut Actual tokenOut received
    /// @return intermediateUsdl USDL amount received from the sell leg
    function swapTokenForToken(
        address tokenIn,
        address tokenOut,
        uint256 amountIn,
        uint256 minAmountOut,
        uint256 deadline
    ) external returns (uint256 amountOut, uint256 intermediateUsdl);

    // --- Permit Variants ---

    /// @notice Buy tokens with USDL using EIP-2612 permit (no separate approve tx)
    function buyWithPermit(
        address pool,
        uint256 usdlAmountIn,
        uint256 minTokensOut,
        uint256 deadline,
        PermitParams calldata permit
    ) external returns (uint256 amountOut);

    /// @notice Sell tokens for USDL using EIP-2612 permit (no separate approve tx)
    function sellWithPermit(
        address pool,
        uint256 tokenAmountIn,
        uint256 minUsdlOut,
        uint256 deadline,
        PermitParams calldata permit
    ) external returns (uint256 amountOut);

    /// @notice Swap Token A → USDL → Token B using EIP-2612 permit on tokenIn
    function swapTokenForTokenWithPermit(
        address tokenIn,
        address tokenOut,
        uint256 amountIn,
        uint256 minAmountOut,
        uint256 deadline,
        PermitParams calldata permit
    ) external returns (uint256 amountOut, uint256 intermediateUsdl);

    /// @notice Create a new market using EIP-2612 permit for USDL creation fee
    function createMarketWithPermit(
        string calldata name,
        string calldata symbol,
        uint8 feeStrategy,
        address optical,
        PermitParams calldata permit
    ) external returns (address tokenAddr, address poolAddr, uint256 nftId);
}
