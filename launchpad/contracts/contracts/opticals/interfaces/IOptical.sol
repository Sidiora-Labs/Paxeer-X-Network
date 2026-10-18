// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

/// @title IOptical
/// @notice Interface for optical hook plugins that inject custom logic into pool lifecycle.
/// @dev Inspired by Uniswap V4's hook system, adapted for Sidiora's launchpad model.
///      Each optical implements a subset of hooks indicated by getFlags().
///      Bit 0: beforeSwap, Bit 1: afterSwap,
///      Bit 2: beforeFeeDistribution, Bit 3: afterFeeDistribution
interface IOptical {
    /// @notice Called before a swap executes in the pool.
    /// @param pool The pool address executing the swap
    /// @param sender The address initiating the swap
    /// @param isBuy True if buying tokens with USDL, false if selling tokens for USDL
    /// @param amountIn The input amount for the swap
    /// @return proceed Whether the swap should continue (false = reject)
    /// @return amountDelta Adjustment to amountIn (positive = increase, negative = decrease)
    function beforeSwap(
        address pool,
        address sender,
        bool isBuy,
        uint256 amountIn
    ) external returns (bool proceed, int256 amountDelta);

    /// @notice Called after a swap completes in the pool.
    /// @param pool The pool address that executed the swap
    /// @param sender The address that initiated the swap
    /// @param isBuy True if it was a buy, false if sell
    /// @param amountIn The input amount used
    /// @param amountOut The output amount received
    /// @return selector The function selector to confirm execution (bytes4)
    function afterSwap(
        address pool,
        address sender,
        bool isBuy,
        uint256 amountIn,
        uint256 amountOut
    ) external returns (bytes4);

    /// @notice Called before fee distribution occurs.
    /// @param pool The pool address
    /// @param feeAmount The fee amount about to be distributed
    /// @return adjustedFee The adjusted fee amount after optical processing
    function beforeFeeDistribution(
        address pool,
        uint256 feeAmount
    ) external returns (uint256 adjustedFee);

    /// @notice Called after fee distribution occurs.
    /// @param pool The pool address
    /// @param feeAmount The fee amount that was distributed
    /// @return selector The function selector to confirm execution (bytes4)
    function afterFeeDistribution(
        address pool,
        uint256 feeAmount
    ) external returns (bytes4);

    /// @notice Returns a bitmap of active hooks for this optical.
    /// @dev Bit 0: beforeSwap, Bit 1: afterSwap,
    ///      Bit 2: beforeFeeDistribution, Bit 3: afterFeeDistribution
    ///      Pool checks this to skip unused callbacks (gas optimization).
    /// @return flags The active hook flags bitmap
    function getFlags() external view returns (uint8 flags);
}
