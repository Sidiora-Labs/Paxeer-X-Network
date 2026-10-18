// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

/// @title ISidioraPool
/// @notice Interface for the core AMM pool contract (beacon proxy instances)
interface ISidioraPool {
    // --- Errors ---
    error DeadlineExpired();
    error SlippageExceeded();
    error InsufficientLiquidity();
    error InsufficientInput();
    error ZeroAddress();
    error ZeroAmount();

    // --- Structs ---
    struct PoolInfo {
        address tokenAddress;
        address opticalAddress;
        uint256 virtualUsdlReserve;
        uint256 realUsdlBalance;
        uint256 tokenReserve;
        uint256 creationTimestamp;
        uint256 cumulativeVolume;
    }

    // --- Functions ---

    /// @notice Initialize pool state (called once by factory via beacon proxy)
    function initialize(
        address _tokenAddress,
        address _usdlAddress,
        address _opticalAddress,
        address _feeAccumulator,
        address _eventEmitter,
        address _protocolConfig,
        address _guardian,
        uint256 _virtualUsdlReserve,
        uint256 _tokenReserve
    ) external;

    /// @notice Execute a swap
    /// @param amountIn Amount of input token
    /// @param minAmountOut Minimum output (slippage protection)
    /// @param isBuy True = USDL→Token, False = Token→USDL
    /// @param recipient Address to receive output tokens
    /// @param deadline Transaction deadline timestamp
    /// @return amountOut Actual output amount
    function swap(
        uint256 amountIn,
        uint256 minAmountOut,
        bool isBuy,
        address recipient,
        uint256 deadline
    ) external returns (uint256 amountOut);

    function swapFor(uint256 amountIn, uint256 minAmountOut, bool isBuy, address trader, address recipient, uint256 deadline) external returns (uint256);
    function currentFeeBps() external view returns (uint256);

    /// @notice Re-read actual token balances (used by LP_REWARDS strategy)
    /// @return usdlBalance Current USDL balance
    /// @return tokenBalance Current token balance
    function syncReserves() external returns (uint256 usdlBalance, uint256 tokenBalance);

    /// @notice Get current reserves
    function getReserves() external view returns (
        uint256 virtualUsdl,
        uint256 realUsdl,
        uint256 tokenReserve
    );

    /// @notice Get effective reserves (virtual + real USDL, token)
    function getEffectiveReserves() external view returns (
        uint256 effectiveUsdl,
        uint256 tokenReserve
    );

    /// @notice Get current token price in USDL (18 decimal fixed-point)
    function getPrice() external view returns (uint256);

    /// @notice Get full pool info struct
    function getPoolInfo() external view returns (PoolInfo memory);

    /// @notice Get the pool's token address
    function tokenAddress() external view returns (address);

    /// @notice Get the pool's optical address
    function opticalAddress() external view returns (address);

    /// @notice Get the pool's creation timestamp
    function creationTimestamp() external view returns (uint256);

    /// @notice Get cumulative USDL-denominated volume (buy input plus sell output)
    function cumulativeVolume() external view returns (uint256);

    /// @notice Get price snapshots for volatility calculation
    function getPriceSnapshots() external view returns (uint256[8] memory);
}
