// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

import "../BaseOptical.sol";
import "../../libraries/BitFlag.sol";
import "../../interfaces/IFeeAccumulator.sol";

/// @title LaunchpadOptical
/// @notice Preset optical for serious projects: cliff + linear vesting for team wallets,
///         plus a time-limited capital raise fee that diverts a portion of swap fees to the team as USDL.
/// @dev Immutable once deployed. One instance per pool (constructor params are pool-specific).
///
///      HOOKS USED:
///        - beforeTokenTransfer:    Enforce cliff lock + linear vesting on all team transfers
///        - beforeSwap/afterSwap:   Authenticated lifecycle hooks; no duplicate transfer accounting
///        - beforeFeeDistribution:  Divert capitalRaiseBps of pool fees to optical surplus for team
///
///      CAPITAL RAISE FLOW:
///        1. beforeFeeDistribution reduces the pool fee by capitalRaiseBps%
///        2. FeeAccumulator records the difference as opticalSurplus
///        3. Team calls claimCapital() → LaunchpadOptical calls FeeAccumulator.claimOpticalSurplus()
///        4. USDL is transferred to teamClaimAddress
contract LaunchpadOptical is BaseOptical {
    // ============ ERRORS ============

    error CliffActive();
    error VestingExceeded();
    error CapitalRaiseTooHigh();
    error DurationTooLong();
    error NoTeamWallets();
    error TooManyTeamWallets();
    error NothingToClaim();
    error NotTeam();

    // ============ EVENTS ============

    event PoolStartTimeRecorded(address indexed pool, uint256 startTime);
    event SellBlockedCliff(address indexed pool, address indexed sender, uint256 amountIn);
    event SellBlockedVesting(address indexed pool, address indexed sender, uint256 amountIn, uint256 maxSellable);
    event CapitalRaiseAccumulated(address indexed pool, uint256 amount, uint256 totalRaisedAmount);
    event CapitalRaiseClaimed(address indexed pool, uint256 amount, address indexed recipient);

    // ============ CONSTANTS ============

    uint256 public constant MAX_CAPITAL_RAISE_BPS = 1000; // 10%
    uint256 public constant MAX_CLIFF_DURATION = 365 days;
    uint256 public constant MAX_VESTING_DURATION = 1095 days; // 3 years
    uint256 public constant MAX_RAISE_DURATION = 365 days;
    uint256 public constant MAX_TEAM_WALLETS = 20;

    // ============ IMMUTABLES ============

    /// @notice Pool creator wallet (auto-vested)
    address public immutable creator;

    /// @notice Address where accumulated USDL capital is claimed to
    address public immutable teamClaimAddress;

    /// @notice FeeAccumulator address for claiming optical surplus
    address public immutable feeAccumulator;

    /// @notice Hard lock period in seconds before any vesting begins
    uint256 public immutable cliffDuration;

    /// @notice Linear unlock period in seconds after the cliff
    uint256 public immutable vestingDuration;

    /// @notice Fee percentage diverted to team (in bps, max 1000 = 10%)
    uint256 public immutable capitalRaiseBps;

    /// @notice How long the capital raise fee is active (in seconds)
    uint256 public immutable capitalRaiseDuration;

    /// @notice Number of team wallets (excluding creator)
    uint256 public immutable teamWalletCount;

    // ============ STORAGE ============

    /// @notice Whether an address is subject to vesting restrictions
    mapping(address => bool) public isVested;

    /// @notice Authoritative registry creation timestamp, cached on authenticated interaction
    mapping(address => uint256) public poolStartTime;

    /// @notice Cumulative tokens released by each vested wallet per pool (all transfers)
    /// @dev pool → wallet → amount sold
    mapping(address => mapping(address => uint256)) public tokensSold;

    /// @notice USDL accumulated per pool for team capital raise (accounting only)
    mapping(address => uint256) public accumulatedUsdl;

    /// @notice Lifetime total USDL raised per pool
    mapping(address => uint256) public totalRaised;

    // ============ CONSTRUCTOR ============

    /// @param _poolRegistry PoolRegistry address for pool validation
    /// @param _owner Deployer/owner address
    /// @param _creator Pool creator wallet (auto-vested)
    /// @param _teamWallets Additional team wallets to vest
    /// @param _cliffDuration Hard lock period in seconds
    /// @param _vestingDuration Linear unlock period after cliff in seconds
    /// @param _capitalRaiseBps Fee percentage diverted to team (max 1000 = 10%)
    /// @param _capitalRaiseDuration How long the capital raise fee is active in seconds
    /// @param _teamClaimAddress Where accumulated USDL is claimed to
    /// @param _feeAccumulator FeeAccumulator contract address
    constructor(
        address _poolRegistry,
        address _owner,
        address _creator,
        address[] memory _teamWallets,
        uint256 _cliffDuration,
        uint256 _vestingDuration,
        uint256 _capitalRaiseBps,
        uint256 _capitalRaiseDuration,
        address _teamClaimAddress,
        address _feeAccumulator
    ) BaseOptical(_poolRegistry, _owner) {
        if (_creator == address(0)) revert NotPool();
        if (_teamClaimAddress == address(0)) revert NotPool();
        if (_feeAccumulator == address(0)) revert NotPool();
        if (_capitalRaiseBps > MAX_CAPITAL_RAISE_BPS) revert CapitalRaiseTooHigh();
        if (_cliffDuration > MAX_CLIFF_DURATION) revert DurationTooLong();
        if (_vestingDuration > MAX_VESTING_DURATION) revert DurationTooLong();
        if (_capitalRaiseDuration > MAX_RAISE_DURATION) revert DurationTooLong();
        if (_teamWallets.length > MAX_TEAM_WALLETS) revert TooManyTeamWallets();

        creator = _creator;
        teamClaimAddress = _teamClaimAddress;
        feeAccumulator = _feeAccumulator;
        cliffDuration = _cliffDuration;
        vestingDuration = _vestingDuration;
        capitalRaiseBps = _capitalRaiseBps;
        capitalRaiseDuration = _capitalRaiseDuration;
        teamWalletCount = _teamWallets.length;

        // Mark creator as vested
        isVested[_creator] = true;

        // Mark all team wallets as vested
        for (uint256 i = 0; i < _teamWallets.length; i++) {
            if (_teamWallets[i] != address(0)) {
                isVested[_teamWallets[i]] = true;
            }
        }
    }

    // ============ HOOK FLAGS ============

    /// @inheritdoc IOptical
    function getFlags() external pure override returns (uint8) {
        // Swap callbacks + fee diversion + transfer-wide vesting.
        return BitFlag.BEFORE_SWAP | BitFlag.AFTER_SWAP | BitFlag.BEFORE_FEE_DISTRIBUTION | BitFlag.BEFORE_TOKEN_TRANSFER;
    }

    // ============ BEFORE SWAP — VESTING ENFORCEMENT ============

    /// @inheritdoc IOptical
    function beforeSwap(
        address pool,
        address sender,
        bool isBuy,
        uint256 amountIn
    ) external override onlyAttachedPool(pool) returns (bool proceed, int256 amountDelta) {
        // Transfer policy validates and accounts releases before token balances change.
        // A sell's transfer has already consumed its allowance; never count it twice.
        _recordStart(pool);
        sender; isBuy; amountIn;
        return (true, 0);
    }

    function afterSwap(address pool, address, bool, uint256, uint256)
        external override onlyAttachedPool(pool) returns (bytes4)
    {
        return IOptical.afterSwap.selector;
    }

    /// @notice Token-level restriction covers sells, transfers, and delegated transfers.
    function beforeTokenTransfer(address pool, address from, address to, uint256 amount) external {
        IPoolRegistry.PoolMetadata memory meta = _poolMetadata(pool);
        if (msg.sender != meta.token) revert NotPool();
        uint256 start = _recordStart(pool);
        if (from == address(0) || from == to || !isVested[from] || amount == 0) return;
        uint256 elapsed = block.timestamp - start;
        if (elapsed < cliffDuration) revert CliffActive();
        uint256 sold = tokensSold[pool][from];
        if (elapsed < cliffDuration + vestingDuration) {
            uint256 allocation = _getTokenBalance(pool, from) + sold;
            uint256 vested = allocation * (elapsed - cliffDuration) / vestingDuration;
            if (sold + amount > vested) revert VestingExceeded();
        }
        tokensSold[pool][from] = sold + amount;
    }

    function _recordStart(address pool) internal returns (uint256 start) {
        IPoolRegistry.PoolMetadata memory meta = _poolMetadata(pool);
        if (meta.creator != creator) revert NotTeam();
        start = meta.createdAt;
        if (poolStartTime[pool] == 0) {
            poolStartTime[pool] = start;
            emit PoolStartTimeRecorded(pool, start);
        }
    }

    // ============ BEFORE FEE DISTRIBUTION — CAPITAL RAISE ============

    /// @inheritdoc IOptical
    function beforeFeeDistribution(
        address pool,
        uint256 feeAmount
    ) external override onlyFeeAccumulator(pool) returns (uint256 adjustedFee) {
        if (capitalRaiseBps == 0) {
            return feeAmount;
        }

        uint256 startTime = _recordStart(pool);
        if (startTime == 0) {
            return feeAmount;
        }

        uint256 elapsed = block.timestamp - startTime;
        if (elapsed >= capitalRaiseDuration) {
            return feeAmount;
        }

        // Divert capitalRaiseBps of the fee to team
        uint256 raiseAmount = (feeAmount * capitalRaiseBps) / 10000;
        if (raiseAmount == 0) {
            return feeAmount;
        }

        accumulatedUsdl[pool] += raiseAmount;
        totalRaised[pool] += raiseAmount;

        emit CapitalRaiseAccumulated(pool, raiseAmount, totalRaised[pool]);

        return feeAmount - raiseAmount;
    }

    // ============ CAPITAL CLAIM ============

    /// @notice Claim accumulated USDL capital for a pool
    /// @param pool The pool address to claim capital from
    function claimCapital(address pool) external {
        if (msg.sender != teamClaimAddress) revert NotTeam();
        uint256 amount = accumulatedUsdl[pool];
        if (amount == 0) revert NothingToClaim();

        accumulatedUsdl[pool] = 0;
        IFeeAccumulator(feeAccumulator).claimOpticalSurplus(pool, amount, teamClaimAddress);

        emit CapitalRaiseClaimed(pool, amount, teamClaimAddress);
    }

    // ============ VIEW FUNCTIONS ============

    /// @notice Get vesting info for a wallet in a specific pool
    function getVestingInfo(address pool, address wallet) external view returns (
        bool vested,
        uint256 cliffEnd,
        uint256 vestEnd,
        uint256 vestedAmount,
        uint256 sold,
        uint256 maxSellableNow
    ) {
        vested = isVested[wallet];
        uint256 startTime = _poolMetadata(pool).createdAt;

        if (startTime == 0 || !vested) {
            return (vested, 0, 0, 0, 0, 0);
        }

        cliffEnd = startTime + cliffDuration;
        vestEnd = startTime + cliffDuration + vestingDuration;
        sold = tokensSold[pool][wallet];

        uint256 elapsed = block.timestamp - startTime;

        if (elapsed < cliffDuration) {
            // Still in cliff — nothing vested
            return (vested, cliffEnd, vestEnd, 0, sold, 0);
        }

        uint256 currentBalance = _getTokenBalance(pool, wallet);
        uint256 originalAllocation = currentBalance + sold;

        if (elapsed >= cliffDuration + vestingDuration) {
            // Fully vested
            vestedAmount = originalAllocation;
            maxSellableNow = currentBalance;
        } else {
            // Linear vesting
            uint256 vestedFraction = ((elapsed - cliffDuration) * 1e18) / vestingDuration;
            vestedAmount = (originalAllocation * vestedFraction) / 1e18;
            if (vestedAmount > sold) {
                maxSellableNow = vestedAmount - sold;
            }
        }
    }

    /// @notice Get capital raise info for a pool
    function getCapitalRaiseInfo(address pool) external view returns (
        uint256 accumulated,
        uint256 totalRaisedAmount,
        uint256 raiseEndTime,
        bool isActive
    ) {
        accumulated = accumulatedUsdl[pool];
        totalRaisedAmount = totalRaised[pool];
        uint256 startTime = _poolMetadata(pool).createdAt;

        if (startTime > 0) {
            raiseEndTime = startTime + capitalRaiseDuration;
            isActive = block.timestamp < raiseEndTime && capitalRaiseBps > 0;
        }
    }

    /// @notice Check if an address is a team wallet (vested)
    function isTeamWallet(address wallet) external view returns (bool) {
        return isVested[wallet];
    }

    // ============ INTERNAL ============

    /// @dev Read token balance of a wallet via the pool's token address
    function _getTokenBalance(address pool, address wallet) internal view returns (uint256) {
        // Read tokenAddress from the pool
        (bool success, bytes memory data) = pool.staticcall(
            abi.encodeWithSignature("tokenAddress()")
        );
        if (!success || data.length < 32) revert NotPool();
        address token = abi.decode(data, (address));

        // Read balance
        (bool success2, bytes memory data2) = token.staticcall(
            abi.encodeWithSignature("balanceOf(address)", wallet)
        );
        if (!success2 || data2.length < 32) revert NotPool();
        return abi.decode(data2, (uint256));
    }
}
