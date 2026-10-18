// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

import "../base/Initializable.sol";
import "../base/UUPSUpgradeable.sol";
import "../base/AccessControl.sol";
import "../base/ReentrancyGuard.sol";
import "../interfaces/IFeeAccumulator.sol";
import "../interfaces/IProtocolConfig.sol";
import "../interfaces/IPoolRegistry.sol";
import "../interfaces/ITreasury.sol";
import "../interfaces/IEventEmitter.sol";
import "../libraries/TransferHelper.sol";
import "../libraries/BitFlag.sol";

/// @dev Minimal interface for optical hooks used by FeeAccumulator
interface ICheckpointToken {
    function getPastBalance(address account, uint256 blockNumber) external view returns (uint256);
    function getPastTotalSupply(uint256 blockNumber) external view returns (uint256);
}

interface IOpticalFee {
    function beforeFeeDistribution(address pool, uint256 feeAmount) external returns (uint256 adjustedFee);
    function afterFeeDistribution(address pool, uint256 feeAmount) external returns (bytes4);
    function getFlags() external view returns (uint8);
}

/// @title FeeAccumulator
/// @notice Tracks accumulated fees per pool and executes fee distribution strategies
/// @dev UUPS proxy. Inspired by Aerodrome's PoolFees separation.
contract FeeAccumulator is IFeeAccumulator, Initializable, UUPSUpgradeable, AccessControl, ReentrancyGuard {
    event MirrorFailed(bytes4 indexed selector);
    bytes32 public constant POOL_ROLE = keccak256("POOL_ROLE");
    bytes32 public constant FEES_ROUTER_ROLE = keccak256("FEES_ROUTER_ROLE");
    bytes32 public constant FACTORY_ROLE = keccak256("FACTORY_ROLE");
    bytes32 public constant OPTICAL_CLAIM_ROLE = keccak256("OPTICAL_CLAIM_ROLE");
    bytes32 public constant OPTICAL_GRANTER_ROLE = keccak256("OPTICAL_GRANTER_ROLE");

    /// @notice Authorize a pool to record fees. Called by Factory on market creation.
    function authorizePool(address pool) external onlyRole(FACTORY_ROLE) {
        _grantRole(POOL_ROLE, pool);
    }

    address public constant DEAD_ADDRESS = 0x000000000000000000000000000000000000dEaD;

    address public protocolConfig;
    address public treasury;
    address public poolRegistry;
    address public eventEmitter;
    address public usdlAddress;

    mapping(address => uint256) private _accumulatedFees;
    uint256 private _protocolFeesPending;

    // Airdrop state
    mapping(address => uint256) private _airdropBalance;
    mapping(address => uint256) private _airdropEpoch;
    mapping(address => mapping(uint256 => uint256)) private _airdropEpochAmount;
    mapping(address => mapping(address => mapping(uint256 => bool))) private _airdropClaimed;

    // LP rewards state
    mapping(address => uint256) private _lpRewardsBalance;

    // Optical surplus state (held on behalf of opticals that divert fees)
    mapping(address => uint256) private _opticalSurplus;

    // Appended storage: never reorder legacy proxy fields above.
    uint256 public totalLiabilities;
    mapping(address => mapping(uint256 => uint256)) public airdropSnapshotBlock;
    mapping(address => mapping(uint256 => uint256)) public airdropEligibleSupply;
    mapping(address => mapping(uint256 => uint256)) public airdropEpochRemaining;
    mapping(address => mapping(uint256 => uint256)) public airdropClaimedSupply;

    bool public accountingInitialized;
    uint256 public accountingMigrationCursor;
    mapping(address => bool) private _accountingMigratedPool;
    mapping(address => uint256) public legacyAirdropUnallocated;
    mapping(address => mapping(uint256 => bytes32)) public legacyAirdropRoot;
    mapping(address => mapping(uint256 => uint256)) public legacyAirdropRemaining;
    event AccountingMigrated(uint256 liabilities);
    event LegacyAirdropAllocated(address indexed pool, uint256 indexed epoch, bytes32 root, uint256 budget);

    /// @custom:oz-upgrades-unsafe-allow constructor
    constructor() {
        _disableInitializers();
    }

    function initialize(
        address _protocolConfig,
        address _treasury,
        address _poolRegistry,
        address _eventEmitter,
        address _usdlAddress,
        address _admin
    ) external initializer {
        if (_admin == address(0)) revert ZeroAddress();
        if (_usdlAddress == address(0)) revert ZeroAddress();
        if (_treasury == address(0)) revert ZeroAddress();

        protocolConfig = _protocolConfig;
        treasury = _treasury;
        poolRegistry = _poolRegistry;
        eventEmitter = _eventEmitter;
        usdlAddress = _usdlAddress;

        _grantRole(DEFAULT_ADMIN_ROLE, _admin);
        _initReentrancyGuard();
        accountingInitialized = true;
    }

    /// @inheritdoc IFeeAccumulator
    function recordFee(address pool, uint256 feeAmount) external override onlyRole(POOL_ROLE) nonReentrant {
        if (!accountingInitialized) revert Unauthorized();
        _requirePool(pool);
        if (feeAmount == 0) revert ZeroAmount();
        _requireBacking(feeAmount);
        totalLiabilities += feeAmount;

        // Split fee into protocol and pool portions
        uint256 protocolFeeBps = IProtocolConfig(protocolConfig).protocolFeeBps();
        uint256 protocolCut = (feeAmount * protocolFeeBps) / 10000;
        uint256 poolCut = feeAmount - protocolCut;

        // Accumulate protocol fees
        _protocolFeesPending += protocolCut;

        // Call optical beforeFeeDistribution hook if applicable
        uint256 opticalDivert = 0;
        if (poolRegistry != address(0)) {
            IPoolRegistry.PoolMetadata memory meta = IPoolRegistry(poolRegistry).getPoolMetadata(pool);
            if (meta.optical != address(0)) {
                uint8 flags = IOpticalFee(meta.optical).getFlags();
                if (BitFlag.hasFlag(flags, BitFlag.BEFORE_FEE_DISTRIBUTION)) {
                    uint256 adjustedPoolCut = IOpticalFee(meta.optical).beforeFeeDistribution(pool, poolCut);
                    if (adjustedPoolCut > poolCut) revert InsufficientSurplus();
                    if (adjustedPoolCut < poolCut) {
                        opticalDivert = poolCut - adjustedPoolCut;
                        _opticalSurplus[pool] += opticalDivert;
                        poolCut = adjustedPoolCut;
                        emit OpticalSurplusRecorded(pool, opticalDivert);
        _mirrorFee(7, pool, address(0), opticalDivert, 0, 0, 0);
                    }
                }
            }
        }

        // Accumulate pool fees (after optical diversion)
        _accumulatedFees[pool] += poolCut;

        // Transfer protocol cut to treasury
        if (protocolCut > 0) {
            TransferHelper.safeApprove(usdlAddress, treasury, protocolCut);
            ITreasury(treasury).deposit(usdlAddress, protocolCut);
            _protocolFeesPending -= protocolCut;
            totalLiabilities -= protocolCut;
        }

        if (poolRegistry != address(0)) {
            address optical = IPoolRegistry(poolRegistry).getPoolMetadata(pool).optical;
            if (optical != address(0) && BitFlag.hasFlag(IOpticalFee(optical).getFlags(), BitFlag.AFTER_FEE_DISTRIBUTION)) {
                if (IOpticalFee(optical).afterFeeDistribution(pool, poolCut) != IOpticalFee.afterFeeDistribution.selector) revert Unauthorized();
            }
        }
        emit FeeRecorded(pool, feeAmount, protocolCut, poolCut);
        _mirrorFee(0, pool, address(0), feeAmount, protocolCut, poolCut, 0);
    }

    /// @inheritdoc IFeeAccumulator
    function claim(
        address pool,
        address recipient
    ) external override onlyRole(FEES_ROUTER_ROLE) nonReentrant returns (uint256 amount) {
        if (!accountingInitialized) revert Unauthorized();
        if (recipient == address(0)) revert ZeroAddress();
        amount = _accumulatedFees[pool];
        if (amount == 0) revert NoFeesAccumulated();

        _accumulatedFees[pool] = 0;
        totalLiabilities -= amount;
        TransferHelper.safeTransfer(usdlAddress, recipient, amount);

        emit FeesClaimed(pool, recipient, amount);
        _mirrorFee(1, pool, recipient, amount, 0, 0, 0);
    }

    /// @inheritdoc IFeeAccumulator
    function burn(
        address pool
    ) external override onlyRole(FEES_ROUTER_ROLE) nonReentrant returns (uint256 amount) {
        if (!accountingInitialized) revert Unauthorized();
        amount = _accumulatedFees[pool];
        if (amount == 0) revert NoFeesAccumulated();

        _accumulatedFees[pool] = 0;
        totalLiabilities -= amount;
        TransferHelper.safeTransfer(usdlAddress, DEAD_ADDRESS, amount);

        emit FeesBurned(pool, amount);
        _mirrorFee(2, pool, DEAD_ADDRESS, amount, 0, 0, 0);
    }

    /// @inheritdoc IFeeAccumulator
    function triggerAirdrop(
        address pool
    ) external override onlyRole(FEES_ROUTER_ROLE) nonReentrant returns (uint256 totalAmount) {
        if (!accountingInitialized) revert Unauthorized();
        totalAmount = _accumulatedFees[pool];
        if (totalAmount == 0) revert NoFeesAccumulated();

        _accumulatedFees[pool] = 0;

        IPoolRegistry.PoolMetadata memory meta = IPoolRegistry(poolRegistry).getPoolMetadata(pool);
        if (!supportsAirdrop(pool)) revert UnsupportedSnapshotToken();
        uint256 snapshot = block.number - 1;
        ICheckpointToken token = ICheckpointToken(meta.token);
        uint256 eligible = token.getPastTotalSupply(snapshot)
            - token.getPastBalance(pool, snapshot) - token.getPastBalance(DEAD_ADDRESS, snapshot);
        if (eligible == 0) revert ZeroAmount();
        // Increment epoch and store snapshot
        uint256 newEpoch = _airdropEpoch[pool] + 1;
        _airdropEpoch[pool] = newEpoch;
        _airdropEpochAmount[pool][newEpoch] = totalAmount;
        airdropSnapshotBlock[pool][newEpoch] = snapshot;
        airdropEligibleSupply[pool][newEpoch] = eligible;
        airdropEpochRemaining[pool][newEpoch] = totalAmount;
        _airdropBalance[pool] += totalAmount;

        emit AirdropTriggered(pool, totalAmount, newEpoch);
        _mirrorFee(3, pool, address(0), totalAmount, 0, 0, newEpoch);
    }

    /// @inheritdoc IFeeAccumulator
    function claimAirdrop(
        address pool,
        address holder
    ) external override nonReentrant returns (uint256 amount) {
        if (!accountingInitialized) revert Unauthorized();
        return _claimAirdrop(pool, holder, _airdropEpoch[pool]);
    }

    function claimAirdropEpoch(address pool, address holder, uint256 epoch)
        external override nonReentrant returns (uint256 amount)
    {
        if (!accountingInitialized) revert Unauthorized();
        return _claimAirdrop(pool, holder, epoch);
    }

    function _claimAirdrop(address pool, address holder, uint256 epoch) internal returns (uint256 amount) {
        if (epoch == 0 || epoch > _airdropEpoch[pool]) revert AirdropNotTriggered();
        if (holder == pool || holder == DEAD_ADDRESS || holder == address(0)) revert Unauthorized();
        if (_airdropClaimed[pool][holder][epoch]) revert AlreadyClaimed();
        uint256 supply = airdropEligibleSupply[pool][epoch];
        if (supply == 0) revert AirdropNotTriggered(); // Legacy live-balance epochs require explicit migration.
        address token = IPoolRegistry(poolRegistry).getPoolMetadata(pool).token;
        uint256 balance = ICheckpointToken(token).getPastBalance(holder, airdropSnapshotBlock[pool][epoch]);
        if (balance == 0) revert ZeroAmount();
        amount = _airdropEpochAmount[pool][epoch] * balance / supply;
        _airdropClaimed[pool][holder][epoch] = true;
        uint256 claimedSupply = airdropClaimedSupply[pool][epoch] + balance;
        airdropClaimedSupply[pool][epoch] = claimedSupply;
        // Allocate rounding residue to the final eligible claimant; no epoch funds are lost.
        if (claimedSupply == supply) amount = airdropEpochRemaining[pool][epoch];
        airdropEpochRemaining[pool][epoch] -= amount;
        _airdropBalance[pool] -= amount;
        totalLiabilities -= amount;
        TransferHelper.safeTransfer(usdlAddress, holder, amount);
        emit AirdropClaimed(pool, holder, amount, epoch);
        _mirrorFee(4, pool, holder, amount, 0, 0, epoch);
    }

    /// @inheritdoc IFeeAccumulator
    function sendLpRewards(
        address pool
    ) external override onlyRole(FEES_ROUTER_ROLE) nonReentrant returns (uint256 amount) {
        if (!accountingInitialized) revert Unauthorized();
        amount = _accumulatedFees[pool];
        if (amount == 0) revert NoFeesAccumulated();

        _accumulatedFees[pool] = 0;
        totalLiabilities -= amount;
        _lpRewardsBalance[pool] += amount; // Cumulative liquidity donations, not claimable rewards.
        TransferHelper.safeTransfer(usdlAddress, pool, amount);

        emit LpRewardsSent(pool, amount);
        _mirrorFee(5, pool, pool, amount, 0, 0, 0);
    }

    // --- Views ---

    /// @inheritdoc IFeeAccumulator
    function getAccumulatedFees(address pool) external view override returns (uint256) {
        return _accumulatedFees[pool];
    }

    /// @inheritdoc IFeeAccumulator
    function getProtocolFeesPending() external view override returns (uint256) {
        return _protocolFeesPending;
    }

    /// @inheritdoc IFeeAccumulator
    function getAirdropBalance(address pool) external view override returns (uint256) {
        return _airdropBalance[pool];
    }

    /// @inheritdoc IFeeAccumulator
    function getLpRewardsBalance(address pool) external view override returns (uint256) {
        return _lpRewardsBalance[pool];
    }

    /// @inheritdoc IFeeAccumulator
    /// @notice Existing immutable tokens without checkpoints cannot accept new snapshot distributions.
    function supportsAirdrop(address pool) public view override returns (bool) {
        if (block.number == 0) return false;
        // Capability may be checked during NFT mint before atomic registry registration.
        (bool ok, bytes memory data) = pool.staticcall(abi.encodeWithSignature("tokenAddress()"));
        if (!ok || data.length != 32) return false;
        address token = abi.decode(data, (address));
        if (token.code.length == 0) return false;
        (ok, data) = token.staticcall(abi.encodeWithSelector(ICheckpointToken.getPastTotalSupply.selector, block.number - 1));
        if (!ok || data.length != 32) return false;
        (ok, data) = token.staticcall(abi.encodeWithSelector(ICheckpointToken.getPastBalance.selector, pool, block.number - 1));
        return ok && data.length == 32;
    }

    function getAirdropEpoch(address pool) external view override returns (uint256) {
        return _airdropEpoch[pool];
    }

    /// @inheritdoc IFeeAccumulator
    function hasClaimedAirdrop(
        address pool,
        address holder,
        uint256 epoch
    ) external view override returns (bool) {
        return _airdropClaimed[pool][holder][epoch];
    }

    // --- Internal ---

    function _getTokenBalance(address token, address account) internal view returns (uint256) {
        (bool success, bytes memory data) = token.staticcall(
            abi.encodeWithSignature("balanceOf(address)", account)
        );
        if (!success || data.length < 32) return 0;
        return abi.decode(data, (uint256));
    }

    function _getTokenTotalSupply(address token) internal view returns (uint256) {
        (bool success, bytes memory data) = token.staticcall(
            abi.encodeWithSignature("totalSupply()")
        );
        if (!success || data.length < 32) return 0;
        return abi.decode(data, (uint256));
    }

    /// @notice Authorize an optical contract to claim surplus. Callable by OPTICAL_GRANTER_ROLE (e.g. LaunchpadOpticalFactory).
    function authorizeOptical(address optical) external onlyRole(OPTICAL_GRANTER_ROLE) {
        _grantRole(OPTICAL_CLAIM_ROLE, optical);
    }

    // --- Optical Surplus ---

    /// @inheritdoc IFeeAccumulator
    function recordOpticalSurplus(address pool, uint256 amount) external override onlyRole(POOL_ROLE) nonReentrant {
        if (!accountingInitialized) revert Unauthorized();
        _requirePool(pool);
        if (amount == 0) return;
        _requireBacking(amount);
        totalLiabilities += amount;
        _opticalSurplus[pool] += amount;
        emit OpticalSurplusRecorded(pool, amount);
        _mirrorFee(7, pool, address(0), amount, 0, 0, 0);
    }

    /// @inheritdoc IFeeAccumulator
    function claimOpticalSurplus(
        address pool,
        uint256 amount,
        address recipient
    ) external override nonReentrant {
        if (!accountingInitialized) revert Unauthorized();
        if (IPoolRegistry(poolRegistry).getPoolMetadata(pool).optical != msg.sender || msg.sender.code.length == 0) revert Unauthorized();
        if (recipient == address(0)) revert ZeroAddress();
        if (amount == 0) revert ZeroAmount();
        if (_opticalSurplus[pool] < amount) revert InsufficientSurplus();

        _opticalSurplus[pool] -= amount;
        totalLiabilities -= amount;
        TransferHelper.safeTransfer(usdlAddress, recipient, amount);

        emit OpticalSurplusClaimed(pool, recipient, amount);
        _mirrorFee(8, pool, recipient, amount, 0, 0, 0);
    }

    /// @inheritdoc IFeeAccumulator
    function getOpticalSurplus(address pool) external view override returns (uint256) {
        return _opticalSurplus[pool];
    }

    /// @notice Upgrade-only, bounded liability reconciliation. All value mutations stay blocked until complete.
    function migrateAccounting(uint256 maxPools) external onlyRole(DEFAULT_ADMIN_ROLE) {
        if (accountingInitialized || maxPools == 0) revert Unauthorized();
        uint256 count = IPoolRegistry(poolRegistry).getPoolCount();
        uint256 cursor = accountingMigrationCursor;
        uint256 n = maxPools > count - cursor ? count - cursor : maxPools;
        address[] memory pools = IPoolRegistry(poolRegistry).getAllPools(cursor, n);
        for (uint256 i; i < pools.length; ++i) {
            _reconcileLegacyPool(pools[i]);
        }
        accountingMigrationCursor = cursor + n;
        if (accountingMigrationCursor == count) {
            if (_getTokenBalance(usdlAddress, address(this)) < totalLiabilities) revert InsufficientSurplus();
            _protocolFeesPending = 0; // Legacy counter represented fees already deposited in Treasury.
            accountingInitialized = true;
            emit AccountingMigrated(totalLiabilities);
        }
    }

    /// @notice Include legacy liability keys discovered from events outside the registered-pool inventory.
    /// @dev The legacy recorder permitted arbitrary pool keys; published migration inventory must include them.
    function migrateAdditionalAccounting(address[] calldata pools) external onlyRole(DEFAULT_ADMIN_ROLE) {
        if (accountingInitialized) revert Unauthorized();
        for (uint256 i; i < pools.length; ++i) _reconcileLegacyPool(pools[i]);
    }

    function _reconcileLegacyPool(address pool) internal {
        if (_accountingMigratedPool[pool]) return;
        _accountingMigratedPool[pool] = true;
        totalLiabilities += _accumulatedFees[pool] + _airdropBalance[pool] + _opticalSurplus[pool];
        legacyAirdropUnallocated[pool] = _airdropBalance[pool];
    }

    /// @notice Assign existing legacy custody to an immutable, externally reconstructed entitlement root.
    /// @dev Root leaf is keccak256(abi.encode(pool, epoch, holder, amount)); historical claims must be excluded.
    ///      Administrator must publish the reconstruction and allocations before invoking this migration.
    function allocateLegacyAirdrop(address pool, uint256 epoch, bytes32 root, uint256 budget)
        external onlyRole(DEFAULT_ADMIN_ROLE)
    {
        if (!accountingInitialized || root == bytes32(0) || budget == 0 || epoch == 0 || epoch > _airdropEpoch[pool]
            || airdropEligibleSupply[pool][epoch] != 0 || legacyAirdropRoot[pool][epoch] != bytes32(0)
            || budget > legacyAirdropUnallocated[pool]) revert Unauthorized();
        legacyAirdropUnallocated[pool] -= budget;
        legacyAirdropRoot[pool][epoch] = root;
        legacyAirdropRemaining[pool][epoch] = budget;
        emit LegacyAirdropAllocated(pool, epoch, root, budget);
    }

    function claimLegacyAirdrop(address pool, uint256 epoch, address holder, uint256 amount, bytes32[] calldata proof)
        external nonReentrant
    {
        if (!accountingInitialized || amount == 0 || holder == address(0)) revert Unauthorized();
        if (_airdropClaimed[pool][holder][epoch]) revert AlreadyClaimed();
        bytes32 hash = keccak256(abi.encode(pool, epoch, holder, amount));
        for (uint256 i; i < proof.length; ++i) {
            bytes32 sibling = proof[i];
            hash = hash < sibling ? keccak256(abi.encodePacked(hash, sibling)) : keccak256(abi.encodePacked(sibling, hash));
        }
        if (hash != legacyAirdropRoot[pool][epoch] || amount > legacyAirdropRemaining[pool][epoch]) revert Unauthorized();
        _airdropClaimed[pool][holder][epoch] = true;
        legacyAirdropRemaining[pool][epoch] -= amount;
        _airdropBalance[pool] -= amount;
        totalLiabilities -= amount;
        TransferHelper.safeTransfer(usdlAddress, holder, amount);
        emit AirdropClaimed(pool, holder, amount, epoch);
        _mirrorFee(4, pool, holder, amount, 0, 0, epoch);
    }

    function _mirrorFee(uint8 kind, address pool, address party, uint256 amount, uint256 protocolCut, uint256 poolCut, uint256 epoch) internal {
        if (eventEmitter == address(0)) return;
        try IEventEmitter(eventEmitter).emitFeeFlow(kind, pool, party, amount, protocolCut, poolCut, epoch) {}
        catch { emit MirrorFailed(IEventEmitter.emitFeeFlow.selector); }
    }

    function _requirePool(address pool) internal view {
        if (msg.sender != pool || pool.code.length == 0 || !IPoolRegistry(poolRegistry).isRegisteredPool(pool)) revert Unauthorized();
    }

    function _requireBacking(uint256 amount) internal view {
        if (_getTokenBalance(usdlAddress, address(this)) < totalLiabilities + amount) revert InsufficientSurplus();
    }

    function _authorizeUpgrade(address) internal override onlyRole(DEFAULT_ADMIN_ROLE) {}
}
