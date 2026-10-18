// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

/// @title IFeeAccumulator
/// @notice Interface for fee tracking and distribution across pools
interface IFeeAccumulator {
    // --- Errors ---
    error Unauthorized();
    error ZeroAddress();
    error ZeroAmount();
    error NoFeesAccumulated();
    error WrongStrategy();
    error AlreadyClaimed();
    error AirdropNotTriggered();
    error InsufficientSurplus();
    error UnsupportedSnapshotToken();

    // --- Events ---
    event FeeRecorded(
        address indexed pool,
        uint256 feeAmount,
        uint256 protocolCut,
        uint256 poolCut
    );

    event FeesClaimed(
        address indexed pool,
        address indexed recipient,
        uint256 amount
    );

    event FeesBurned(
        address indexed pool,
        uint256 amount
    );

    event AirdropTriggered(
        address indexed pool,
        uint256 totalAmount,
        uint256 epoch
    );

    event AirdropClaimed(
        address indexed pool,
        address indexed holder,
        uint256 amount,
        uint256 epoch
    );

    event LpRewardsSent(
        address indexed pool,
        uint256 amount
    );

    event ProtocolFeeSwept(
        uint256 amount
    );

    event OpticalSurplusRecorded(
        address indexed pool,
        uint256 amount
    );

    event OpticalSurplusClaimed(
        address indexed pool,
        address indexed recipient,
        uint256 amount
    );

    // --- Functions ---

    /// @notice Record fees from a swap (pool-only)
    /// @param pool The pool address the fees are from
    /// @param feeAmount Total fee amount in USDL
    function recordFee(address pool, uint256 feeAmount) external;

    /// @notice Claim accumulated fees for a pool (CLAIM strategy, FeesRouter-only)
    /// @param pool The pool address
    /// @param recipient Address to receive the fees
    /// @return amount The amount of USDL claimed
    function claim(address pool, address recipient) external returns (uint256 amount);

    /// @notice Burn accumulated fees for a pool (BURN strategy, FeesRouter-only)
    /// @param pool The pool address
    /// @return amount The amount of USDL burned
    function burn(address pool) external returns (uint256 amount);

    /// @notice Trigger airdrop distribution for a pool (AIRDROP strategy, FeesRouter-only)
    /// @param pool The pool address
    /// @return totalAmount The total amount distributed
    function triggerAirdrop(address pool) external returns (uint256 totalAmount);
    function supportsAirdrop(address pool) external view returns (bool);

    /// @notice Claim airdrop share for a token holder
    /// @param pool The pool address
    /// @param holder The token holder address
    /// @return amount The amount of USDL claimed
    function claimAirdrop(address pool, address holder) external returns (uint256 amount);
    function claimAirdropEpoch(address pool, address holder, uint256 epoch) external returns (uint256 amount);

    /// @notice Send accumulated fees to pool as LP rewards (LP_REWARDS strategy, FeesRouter-only)
    /// @param pool The pool address
    /// @return amount The amount of USDL sent
    function sendLpRewards(address pool) external returns (uint256 amount);

    /// @notice Authorize a pool to record fees (Factory-only via FACTORY_ROLE)
    /// @param pool The pool address to authorize
    function authorizePool(address pool) external;

    /// @notice Get accumulated fees for a pool
    function getAccumulatedFees(address pool) external view returns (uint256);

    /// @notice Get pending protocol fees waiting to be swept
    function getProtocolFeesPending() external view returns (uint256);

    /// @notice Get airdrop balance for a pool
    function getAirdropBalance(address pool) external view returns (uint256);

    /// @notice Get cumulative USDL donated to pool liquidity
    function getLpRewardsBalance(address pool) external view returns (uint256);

    /// @notice Get current airdrop epoch for a pool
    function getAirdropEpoch(address pool) external view returns (uint256);

    /// @notice Check if holder has claimed for a specific epoch
    function hasClaimedAirdrop(address pool, address holder, uint256 epoch) external view returns (bool);

    /// @notice Authorize an optical to claim surplus (OPTICAL_GRANTER_ROLE only)
    /// @param optical The optical contract address to authorize
    function authorizeOptical(address optical) external;

    /// @notice Record optical surplus when beforeFeeDistribution returns adjustedFee < feeAmount
    /// @param pool The pool address
    /// @param amount The surplus amount to record
    function recordOpticalSurplus(address pool, uint256 amount) external;

    /// @notice Claim optical surplus for a pool (OPTICAL_CLAIM_ROLE only)
    /// @param pool The pool address
    /// @param amount Amount to claim
    /// @param recipient Address to receive the USDL
    function claimOpticalSurplus(address pool, uint256 amount, address recipient) external;

    /// @notice Get optical surplus balance for a pool
    function getOpticalSurplus(address pool) external view returns (uint256);
}
