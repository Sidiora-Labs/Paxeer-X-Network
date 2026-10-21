// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

/// @title IQuoter
/// @notice Interface for read-only quote and data access (no state changes)
interface IQuoter {
    // --- Structs ---
    struct QuoteResult {
        uint256 amountOut;
        uint256 feeAmount;
        uint256 priceImpactBps;
    }

    struct PoolStats {
        uint256 virtualUsdl;
        uint256 realUsdl;
        uint256 tokenReserve;
        uint256 cumulativeVolume;
        uint256 currentFeeBps;
        uint256 poolAge;
        uint256 marketCap;
        uint256 price;
    }

    struct MultihopQuoteResult {
        uint256 amountOut;            // Final tokenOut amount
        uint256 intermediateUsdl;     // USDL received from sell leg
        uint256 sellFeeAmount;        // Fee paid on sell leg (in tokenIn)
        uint256 buyFeeAmount;         // Fee paid on buy leg (in USDL)
        uint256 sellPriceImpactBps;   // Price impact on sell leg
        uint256 buyPriceImpactBps;    // Price impact on buy leg
        uint256 combinedPriceImpactBps; // Approximate combined price impact
        address poolA;                // Pool used for sell leg
        address poolB;                // Pool used for buy leg
    }

    // --- Functions ---

    /// @notice Quote exact input amount for a swap
    /// @param pool The pool address
    /// @param amountIn Exact input amount
    /// @param isBuy True = USDL→Token, False = Token→USDL
    /// @return result QuoteResult with amountOut, feeAmount, priceImpact
    function quoteExactInput(
        address pool,
        uint256 amountIn,
        bool isBuy
    ) external view returns (QuoteResult memory result);

    /// @notice Get current pool price in USDL
    function getPoolPrice(address pool) external view returns (uint256);

    /// @notice Get comprehensive pool statistics
    function getPoolStats(address pool) external view returns (PoolStats memory);

    /// @notice Get market capitalization in USDL
    function getMarketCap(address pool) external view returns (uint256);

    /// @notice Get all pools (paginated) from PoolRegistry
    function getAllPools(uint256 offset, uint256 limit) external view returns (address[] memory);

    /// @notice Get pools by creator from PoolRegistry
    function getPoolsByCreator(address creator) external view returns (address[] memory);

    /// @notice Simulate a multihop swap: Token A → USDL → Token B
    /// @param tokenIn Address of the token to sell
    /// @param tokenOut Address of the token to buy
    /// @param amountIn Amount of tokenIn to sell
    /// @return result MultihopQuoteResult with both legs' details
    function quoteMultihop(
        address tokenIn,
        address tokenOut,
        uint256 amountIn
    ) external view returns (MultihopQuoteResult memory result);
}
