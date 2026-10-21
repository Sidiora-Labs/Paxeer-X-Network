// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.27;

import {IPECOROrders} from "../interfaces/IPECOROrders.sol";
import {IPECORVault} from "../interfaces/IPECORVault.sol";
import {IPriceOracle} from "../interfaces/IPriceOracle.sol";
import {ITransactionTracker} from "../interfaces/ITransactionTracker.sol";
import {Initializable} from "../../base/Initializable.sol";
import {UUPSUpgradeable} from "../../base/UUPSUpgradeable.sol";
import {AccessControl} from "../../base/AccessControl.sol";
import {ReentrancyGuard} from "../../base/ReentrancyGuard.sol";
import {Pausable} from "../../base/Pausable.sol";
import {PECOROrderViews, PECOROrderPricing} from "./PECOROrderViews.sol";

/// @title PECOROrders — Limit / Stop-Loss / Stop-Limit order engine
/// @notice UUPS-upgradeable order engine that escrows user funds in
///         PECORVault v2 at place-time and executes atomically at match-time
///         via keeper-triggered pushes. Split from PECOR.sol for contract
///         size and surface-area separation (spec §7.7).
/// @dev Interface: `contracts/meta-ag/interfaces/IPECOROrders.sol`.
///      Vault must grant OPERATOR_ROLE to this contract before placement.
///
/// Inheritance (spec §7.7):
///   IPECOROrders, Initializable, UUPSUpgradeable, AccessControl,
///   ReentrancyGuard, Pausable
///
/// Roles:
///   - DEFAULT_ADMIN_ROLE → Timelock at deploy (invariant S1)
///   - KEEPER_ROLE        → authorized keepers (rotated via {setKeeper})
///
/// Storage layout (append-only per S12):
///   slot 0:  AccessControl._roles             (mapping)
///   slot 1:  priceOracle                      (address)
///   slot 2:  vault                            (address)
///   slot 3:  transactionTracker               (address)
///   slot 4:  nextOrderId                      (uint256)
///   slot 5:  limitOrders                      (mapping)
///   slot 6:  stopLimitOrders                  (mapping)
///   slot 7:  userLimitOrders                  (mapping)
///   slot 8:  userStopLimitOrders              (mapping)
///   slot 9:  activeLimitOrderIds              (uint256[])
///   slot 10: activeStopLimitOrderIds          (uint256[])
///   slot 11: _limitOrderActiveIndex           (mapping, 1-indexed)
///   slot 12: _stopLimitOrderActiveIndex       (mapping, 1-indexed)
///   slot 13: keepers                          (mapping — mirror of KEEPER_ROLE)
///   slot 14..63: __gap[50]
contract PECOROrders is
    IPECOROrders,
    Initializable,
    UUPSUpgradeable,
    AccessControl,
    ReentrancyGuard,
    Pausable
{

    PECOROrderViews private immutable _views;

    bytes32 public constant KEEPER_ROLE = keccak256("KEEPER_ROLE");

    uint256 public constant BPS_DENOMINATOR = 10_000;

    error ZeroAddress();
    error ZeroAmount();
    error InvalidPrice();
    error InvalidExpiry();
    error InvalidPriceRange();
    error NotAStablecoin();
    error TokenIsStablecoin();
    error NotOrderOwner();
    error OrderNotPending();
    error OrderNotActivated();
    error OrderExpired();
    error OrderCannotCancel();
    error PriceNotMet();
    error InsufficientLiquidity();
    error OnlySelf();
    error LiabilityMigrationRequired();
    error MigrationAlreadyComplete();
    error InvalidPricing();
    error InsufficientOutput();
    event OrderExecutionFailed(uint256 indexed orderId, bool stopLimit);
    event OrderExpiredAndRefunded(uint256 indexed orderId, address indexed user, uint256 amount);
    event TrackerCallFailed(address indexed tracker, bytes4 indexed selector);
    event TransactionTrackerUpdated(address indexed tracker);
    event OrderMinimumOutputUpdated(uint256 indexed orderId, uint256 minimum);
    event PricingConfigUpdated(uint256 swapFeeBps, uint256 tier1FeeBps, uint256 tier2FeeBps, bool impactEnabled, uint256 scalarBps);
    event OrderFeesCollected(address indexed token, address indexed collector, uint256 amount);

    IPriceOracle public priceOracle;
    IPECORVault public vault;
    ITransactionTracker public transactionTracker;

    /// @notice Monotonically increasing order id. Starts at 1 so that `0`
    ///         reliably means "not a valid order id".
    uint256 public nextOrderId;

    mapping(uint256 => LimitOrder) public limitOrders;
    mapping(uint256 => StopLimitOrder) public stopLimitOrders;
    mapping(address => uint256[]) public userLimitOrders;
    mapping(address => uint256[]) public userStopLimitOrders;

    uint256[] public activeLimitOrderIds;
    uint256[] public activeStopLimitOrderIds;

    /// @dev 1-indexed active-list positions; 0 means "not in active list".
    mapping(uint256 => uint256) private _limitOrderActiveIndex;
    mapping(uint256 => uint256) private _stopLimitOrderActiveIndex;

    /// @notice Mirror of KEEPER_ROLE membership for O(1) view.
    mapping(address => bool) public keepers;

    /// @dev Reserved storage for future upgrades (S12: append-only, 50 * 32 bytes).
    mapping(uint256 => uint256) public orderMinOutput;
    uint256 public swapFeeBps;
    uint256 public tier1FeeBps;
    uint256 public tier2FeeBps;
    uint256 public priceImpactScalarBps;
    bool public priceImpactEnabled;
    address public feeCollector;
    mapping(address => uint256) public accruedFees;
    bool public escrowInitialized;
    uint256 public limitMigrationCursor;
    uint256 public stopMigrationCursor;
    uint256[40] private __gap;

    modifier liabilitiesInitialized() {
        if (!escrowInitialized) revert LiabilityMigrationRequired();
        _;
    }

    /// @custom:oz-upgrades-unsafe-allow constructor
    constructor() {
        _disableInitializers();
        _views = new PECOROrderViews();
    }

    /// @inheritdoc IPECOROrders
    function initialize(
        address priceOracle_,
        address vault_,
        address tracker_,
        address admin_
    ) external override initializer {
        if (priceOracle_ == address(0)) revert ZeroAddress();
        if (vault_ == address(0)) revert ZeroAddress();
        if (admin_ == address(0)) revert ZeroAddress();

        _initReentrancyGuard();
        priceOracle = IPriceOracle(priceOracle_);
        vault = IPECORVault(vault_);
        transactionTracker = ITransactionTracker(tracker_);
        nextOrderId = 1;
        escrowInitialized = true;
        tier1FeeBps = 20;
        tier2FeeBps = 50;
        priceImpactScalarBps = 100;
        priceImpactEnabled = true;
        feeCollector = admin_;

        _grantRole(DEFAULT_ADMIN_ROLE, admin_);
    }

    function _authorizeUpgrade(address) internal override onlyRole(DEFAULT_ADMIN_ROLE) {}

    /// @inheritdoc IPECOROrders
    /// @dev Rotates KEEPER_ROLE and mirrors the flag in `keepers` for O(1) reads.
    function setKeeper(
        address keeper,
        bool authorized
    ) external override onlyRole(DEFAULT_ADMIN_ROLE) {
        if (keeper == address(0)) revert ZeroAddress();
        keepers[keeper] = authorized;
        if (authorized) {
            _grantRole(KEEPER_ROLE, keeper);
        } else {
            _revokeRole(KEEPER_ROLE, keeper);
        }
        emit KeeperUpdated(keeper, authorized);
    }

    /// @inheritdoc IPECOROrders
    function pause() external override {
        if (!hasRole(DEFAULT_ADMIN_ROLE, msg.sender)) _checkRole(PAUSER_ROLE, msg.sender);
        _pause();
    }

    /// @inheritdoc IPECOROrders
    function unpause() external override onlyRole(DEFAULT_ADMIN_ROLE) {
        _unpause();
    }

    function placeLimitBuy(address stablecoin, address token, uint256 amount, uint256 target, uint256 expiry)
        external override nonReentrant liabilitiesInitialized whenNotPaused returns (uint256)
    { return _placeLimit(stablecoin, token, amount, target, expiry, OrderType.LIMIT_BUY, 0); }

    function placeLimitSell(address token, address stablecoin, uint256 amount, uint256 target, uint256 expiry)
        external override nonReentrant liabilitiesInitialized whenNotPaused returns (uint256)
    { return _placeLimit(stablecoin, token, amount, target, expiry, OrderType.LIMIT_SELL, 0); }

    function placeStopLoss(address token, address stablecoin, uint256 amount, uint256 trigger, uint256 expiry)
        external override nonReentrant liabilitiesInitialized whenNotPaused returns (uint256)
    { return _placeLimit(stablecoin, token, amount, trigger, expiry, OrderType.STOP_LOSS, 0); }

    function placeStopLimitBuy(address stablecoin, address token, uint256 amount, uint256 stop, uint256 limit, uint256 expiry)
        external override nonReentrant liabilitiesInitialized whenNotPaused returns (uint256)
    { return _placeStopLimit(stablecoin, token, amount, stop, limit, expiry, OrderType.STOP_LIMIT_BUY, 0); }

    function placeStopLimitSell(address token, address stablecoin, uint256 amount, uint256 stop, uint256 limit, uint256 expiry)
        external override nonReentrant liabilitiesInitialized whenNotPaused returns (uint256)
    { return _placeStopLimit(stablecoin, token, amount, stop, limit, expiry, OrderType.STOP_LIMIT_SELL, 0); }

    /// @notice Atomic placement with an enforceable delivered-output floor.
    /// All trigger/limit prices are USD per whole nonstable token, not pair rates.
    function placeOrder(OrderRequest calldata request)
        external override nonReentrant liabilitiesInitialized whenNotPaused returns (uint256)
    {
        if (uint8(request.orderType) < 3) return _placeLimit(request.stablecoin, request.token,
            request.amount, request.triggerPrice, request.expiresAt, request.orderType, request.minimumOutput);
        return _placeStopLimit(request.stablecoin, request.token, request.amount,
            request.triggerPrice, request.limitPrice, request.expiresAt, request.orderType, request.minimumOutput);
    }

    function _placeLimit(address stablecoin, address token, uint256 amount, uint256 target, uint256 expiry,
        OrderType kind, uint256 minimum) internal returns (uint256 id)
    {
        _validatePair(stablecoin, token);
        if (amount == 0) revert ZeroAmount();
        if (target == 0) revert InvalidPrice();
        _validateExpiry(expiry);
        address input = kind == OrderType.LIMIT_BUY ? stablecoin : token;
        amount = vault.pullTokens(input, msg.sender, amount);
        vault.reserveTokens(input, amount);
        id = nextOrderId++;
        limitOrders[id] = LimitOrder(id, msg.sender, stablecoin, token, amount, target, kind,
            OrderStatus.PENDING, block.timestamp, expiry);
        userLimitOrders[msg.sender].push(id);
        _addToActiveLimitOrders(id);
        orderMinOutput[id] = minimum;
        emit LimitOrderCreated(id, msg.sender, kind, amount, target);
        if (minimum != 0) emit OrderMinimumOutputUpdated(id, minimum);
        if (kind == OrderType.STOP_LOSS) _track(abi.encodeCall(ITransactionTracker.recordStopLossPlaced, (id, msg.sender, token, amount, target)));
        else _track(abi.encodeCall(ITransactionTracker.recordLimitOrderPlaced,
            (id, msg.sender, token, stablecoin, amount, target, kind == OrderType.LIMIT_BUY)));
    }

    function _placeStopLimit(address stablecoin, address token, uint256 amount, uint256 stop, uint256 limit,
        uint256 expiry, OrderType kind, uint256 minimum) internal returns (uint256 id)
    {
        _validatePair(stablecoin, token);
        if (amount == 0) revert ZeroAmount();
        if (stop == 0 || limit == 0) revert InvalidPrice();
        bool buy = kind == OrderType.STOP_LIMIT_BUY;
        if (buy ? limit < stop : limit > stop) revert InvalidPriceRange();
        _validateExpiry(expiry);
        address input = buy ? stablecoin : token;
        amount = vault.pullTokens(input, msg.sender, amount);
        vault.reserveTokens(input, amount);
        id = nextOrderId++;
        stopLimitOrders[id] = StopLimitOrder(id, msg.sender, stablecoin, token, amount, stop, limit,
            kind, OrderStatus.PENDING, block.timestamp, expiry);
        userStopLimitOrders[msg.sender].push(id);
        _addToActiveStopLimitOrders(id);
        orderMinOutput[id] = minimum;
        emit StopLimitOrderCreated(id, msg.sender, kind, amount, stop, limit);
        if (minimum != 0) emit OrderMinimumOutputUpdated(id, minimum);
        _track(abi.encodeCall(ITransactionTracker.recordStopLimitPlaced,
            (id, msg.sender, token, amount, stop, limit, buy)));
    }

    function cancelLimitOrder(uint256 id) external override nonReentrant liabilitiesInitialized {
        this.refundOrderIsolated(id, false, false, msg.sender);
    }

    function cancelStopLimitOrder(uint256 id) external override nonReentrant liabilitiesInitialized {
        this.refundOrderIsolated(id, true, false, msg.sender);
    }

    /// @inheritdoc IPECOROrders
    function executeLimitOrder(uint256 orderId) external override nonReentrant liabilitiesInitialized whenNotPaused onlyRole(KEEPER_ROLE) {
        _executeLimitOrder(orderId);
    }

    /// @inheritdoc IPECOROrders
    function executeStopLimitOrder(
        uint256 orderId
    ) external override nonReentrant liabilitiesInitialized whenNotPaused onlyRole(KEEPER_ROLE) {
        _executeStopLimitOrder(orderId);
    }

    /// @inheritdoc IPECOROrders
    function checkStopLimitActivation(uint256 orderId) external override nonReentrant liabilitiesInitialized whenNotPaused onlyRole(KEEPER_ROLE) {
        StopLimitOrder storage order = stopLimitOrders[orderId];
        if (order.id == 0 || order.status != OrderStatus.PENDING) revert OrderNotPending();
        if (order.expiresAt != 0 && order.expiresAt <= block.timestamp) revert OrderExpired();

        uint256 currentPrice = priceOracle.getPrice(order.token);
        bool shouldActivate = order.orderType == OrderType.STOP_LIMIT_BUY
            ? currentPrice >= order.stopPrice
            : currentPrice <= order.stopPrice;
        if (shouldActivate) {
            order.status = OrderStatus.ACTIVATED;
            emit StopLimitActivated(orderId, currentPrice);
            if (address(transactionTracker) != address(0)) {
                _track(abi.encodeCall(ITransactionTracker.recordStopLimitActivated, (orderId, currentPrice)));
            }
        }
    }

    /// @inheritdoc IPECOROrders
    function batchExecuteLimitOrders(
        uint256[] calldata orderIds
    ) external override nonReentrant liabilitiesInitialized whenNotPaused onlyRole(KEEPER_ROLE) returns (uint256 executedCount) {
        uint256 n = orderIds.length;
        for (uint256 i = 0; i < n; ++i) {
            try this.executeLimitOrderIsolated(orderIds[i]) { ++executedCount; }
            catch { emit OrderExecutionFailed(orderIds[i], false); }
        }
    }

    /// @inheritdoc IPECOROrders
    function batchCheckAndExecuteStopLimits(
        uint256[] calldata orderIds
    )
        external
        override
        nonReentrant liabilitiesInitialized
        whenNotPaused
        onlyRole(KEEPER_ROLE)
        returns (uint256 activatedCount, uint256 executedCount)
    {
        uint256 n = orderIds.length;
        for (uint256 i; i < n; ++i) {
            try this.activateAndExecuteStopLimitIsolated(orderIds[i]) returns (bool activated, bool executed) {
                if (activated) ++activatedCount;
                if (executed) ++executedCount;
            } catch { emit OrderExecutionFailed(orderIds[i], true); }
        }
    }

    /// @inheritdoc IPECOROrders
    function getLimitOrder(uint256 orderId) external view override returns (LimitOrder memory) {
        return limitOrders[orderId];
    }

    /// @inheritdoc IPECOROrders
    function getStopLimitOrder(
        uint256 orderId
    ) external view override returns (StopLimitOrder memory) {
        return stopLimitOrders[orderId];
    }

    /// @inheritdoc IPECOROrders
    function getUserLimitOrders(
        address user
    ) external view override returns (uint256[] memory) {
        return userLimitOrders[user];
    }

    /// @inheritdoc IPECOROrders
    function getUserStopLimitOrders(
        address user
    ) external view override returns (uint256[] memory) {
        return userStopLimitOrders[user];
    }

    function getActiveLimitOrderCount() external view returns (uint256) {
        return activeLimitOrderIds.length;
    }

    function getActiveStopLimitOrderCount() external view returns (uint256) {
        return activeStopLimitOrderIds.length;
    }

    /// @inheritdoc IPECOROrders
    function canExecuteLimitOrder(uint256 id) external view override returns (bool, string memory) {
        return _views.canExecuteLimitOrder(address(this), id);
    }

    function orderExecutionQuote(uint256 id, bool stop) external view returns (address output, uint256 gross, uint256 net) {
        return _views.quoteOrder(address(this), id, stop);
    }

    function getPricingConfig() external view returns (PECOROrderPricing memory) {
        return PECOROrderPricing(swapFeeBps, tier1FeeBps, tier2FeeBps, priceImpactScalarBps, priceImpactEnabled);
    }

    function orderExecutionPaused() external view returns (bool) { return _paused() || !escrowInitialized; }

    function getExecutableLimitOrders(uint256 maxCount) external view override returns (uint256[] memory orderIds) {
        (orderIds,) = getOrdersPage(0, 100, maxCount, 0);
    }

    function getActivatableStopLimits(uint256 maxCount) external view override returns (uint256[] memory orderIds) {
        (orderIds,) = getOrdersPage(0, 100, maxCount, 1);
    }

    function getExecutableStopLimits(uint256 maxCount) external view override returns (uint256[] memory orderIds) {
        (orderIds,) = getOrdersPage(0, 100, maxCount, 2);
    }

    /// @notice kind=0 executable limit, 1 activatable stop, 2 executable stop.
    /// Cursor is an active-array index; pin one block across pages because fills
    /// and pruning swap-and-pop the indexes. Scanning and allocation cap at 100.
    function getOrdersPage(uint256 cursor, uint256 maxScanned, uint256 maxCount, uint8 kind)
        public view returns (uint256[] memory ids, uint256 next)
    {
        return _views.getOrdersPage(address(this), cursor, maxScanned, maxCount, kind);
    }

    function _executeLimitOrder(uint256 orderId) internal {
        LimitOrder storage order = limitOrders[orderId];
        if (order.id == 0 || order.status != OrderStatus.PENDING) revert OrderNotPending();
        if (order.expiresAt != 0 && order.expiresAt <= block.timestamp) revert OrderExpired();
        uint256 currentPrice = priceOracle.getPrice(order.token);
        bool buy = order.orderType == OrderType.LIMIT_BUY;
        if (buy || order.orderType == OrderType.STOP_LOSS) {
            if (currentPrice > order.targetPrice) revert PriceNotMet();
        } else if (currentPrice < order.targetPrice) revert PriceNotMet();
        order.status = OrderStatus.EXECUTED;
        _removeFromActiveLimitOrders(orderId);
        uint256 outputAmount = _settle(orderId, order.user, buy ? order.stablecoin : order.token,
            buy ? order.token : order.stablecoin, order.amount);
        emit LimitOrderExecuted(orderId, currentPrice);
        if (order.orderType == OrderType.STOP_LOSS) {
            _track(abi.encodeCall(ITransactionTracker.recordStopLossTriggered,
                (orderId, order.user, order.targetPrice, currentPrice, outputAmount)));
        }
        _track(abi.encodeCall(ITransactionTracker.recordLimitOrderExecuted, (orderId, order.user, currentPrice)));
    }

    function _executeStopLimitOrder(uint256 orderId) internal {
        StopLimitOrder storage order = stopLimitOrders[orderId];
        if (order.id == 0 || order.status != OrderStatus.ACTIVATED) revert OrderNotActivated();
        if (order.expiresAt != 0 && order.expiresAt <= block.timestamp) revert OrderExpired();
        uint256 currentPrice = priceOracle.getPrice(order.token);
        bool buy = order.orderType == OrderType.STOP_LIMIT_BUY;
        if (buy ? currentPrice > order.limitPrice : currentPrice < order.limitPrice) revert PriceNotMet();
        order.status = OrderStatus.EXECUTED;
        _removeFromActiveStopLimitOrders(orderId);
        _settle(orderId, order.user, buy ? order.stablecoin : order.token,
            buy ? order.token : order.stablecoin, order.amount);
        emit StopLimitExecuted(orderId, currentPrice);
        _track(abi.encodeCall(ITransactionTracker.recordStopLimitExecuted, (orderId, order.user, currentPrice)));
    }

    function _settle(uint256 id, address user, address input, address output, uint256 amount) internal returns (uint256 net) {
        uint256 gross;
        (gross, net) = _price(input, output, amount);
        if (net < orderMinOutput[id] || net == 0) revert InsufficientOutput();
        if (!vault.hasLiquidity(output, gross)) revert InsufficientLiquidity();
        vault.releaseTokens(input, amount);
        uint256 retained = gross - net;
        if (retained != 0) {
            accruedFees[output] += retained;
            vault.reserveTokens(output, retained);
        }
        vault.pushTokens(output, user, net);
    }

    /// @dev Same defaults, tiers, fee stacking and impact formula as PECOR.
    /// Administrative changes are explicit per-engine; users can pin minOutput.
    function _price(address input, address output, uint256 amount) internal view returns (uint256 gross, uint256 net) {
        return _views.price(address(this), input, output, amount);
    }

    function executeLimitOrderIsolated(uint256 id) external whenNotPaused {
        if (msg.sender != address(this)) revert OnlySelf();
        _executeLimitOrder(id);
    }

    function activateAndExecuteStopLimitIsolated(uint256 id) external whenNotPaused returns (bool activated, bool executed) {
        if (msg.sender != address(this)) revert OnlySelf();
        StopLimitOrder storage order = stopLimitOrders[id];
        if (order.id == 0) revert OrderNotPending();
        if (order.expiresAt != 0 && order.expiresAt <= block.timestamp) revert OrderExpired();
        if (order.status == OrderStatus.PENDING) {
            uint256 currentPrice = priceOracle.getPrice(order.token);
            if (order.orderType == OrderType.STOP_LIMIT_BUY ? currentPrice >= order.stopPrice : currentPrice <= order.stopPrice) {
                order.status = OrderStatus.ACTIVATED;
                activated = true;
                emit StopLimitActivated(id, currentPrice);
                _track(abi.encodeCall(ITransactionTracker.recordStopLimitActivated, (id, currentPrice)));
            }
        }
        if (order.status == OrderStatus.ACTIVATED) {
            // Keep a successful activation even when a fill is not yet eligible.
            try this.executeStopLimitOrderIsolated(id) { executed = true; }
            catch { emit OrderExecutionFailed(id, true); }
        }
    }

    function executeStopLimitOrderIsolated(uint256 id) external whenNotPaused {
        if (msg.sender != address(this)) revert OnlySelf();
        _executeStopLimitOrder(id);
    }

    function setTransactionTracker(address tracker) external onlyRole(DEFAULT_ADMIN_ROLE) {
        transactionTracker = ITransactionTracker(tracker);
        emit TransactionTrackerUpdated(tracker);
    }

    function setPricingConfig(uint256 fee, uint256 tier1, uint256 tier2, bool impact, uint256 scalar)
        external onlyRole(DEFAULT_ADMIN_ROLE)
    {
        if (tier2 < tier1 || fee > 200 || tier1 > 200 || tier2 > 200 || fee + tier1 + tier2 > 200 || scalar > 500) revert InvalidPricing();
        swapFeeBps = fee; tier1FeeBps = tier1; tier2FeeBps = tier2;
        priceImpactEnabled = impact; priceImpactScalarBps = scalar;
        emit PricingConfigUpdated(fee, tier1, tier2, impact, scalar);
    }

    function setFeeCollector(address collector) external onlyRole(DEFAULT_ADMIN_ROLE) {
        if (collector == address(0)) revert ZeroAddress();
        feeCollector = collector;
    }

    function collectFees(address token) external nonReentrant liabilitiesInitialized {
        if (msg.sender != feeCollector) revert NotOrderOwner();
        uint256 amount = accruedFees[token];
        if (amount == 0) revert ZeroAmount();
        accruedFees[token] = 0;
        vault.releaseTokens(token, amount);
        vault.pushTokens(token, feeCollector, amount);
        emit OrderFeesCollected(token, feeCollector, amount);
    }

    /// @notice Owner may change their output floor. Existing placement ABI uses
    /// USD price triggers, not pair exchange-rate guarantees; floor defaults to 0.
    function setOrderMinOutput(uint256 id, uint256 minimum) external nonReentrant liabilitiesInitialized {
        LimitOrder storage order = limitOrders[id];
        StopLimitOrder storage stop = stopLimitOrders[id];
        if (order.user == msg.sender && order.status == OrderStatus.PENDING ||
            stop.user == msg.sender && (stop.status == OrderStatus.PENDING || stop.status == OrderStatus.ACTIVATED)) {
            orderMinOutput[id] = minimum;
            emit OrderMinimumOutputUpdated(id, minimum);
        } else revert NotOrderOwner();
    }

    /// @notice Anyone may expire/refund an order atomically to its owner.
    function expireLimitOrder(uint256 id) external nonReentrant liabilitiesInitialized {
        this.refundOrderIsolated(id, false, true, msg.sender);
    }

    function expireStopLimitOrder(uint256 id) external nonReentrant liabilitiesInitialized {
        this.refundOrderIsolated(id, true, true, msg.sender);
    }

    /// @dev One bytecode refund implementation; callers hold the outer guard.
    function refundOrderIsolated(uint256 id, bool stop, bool expired, address caller) external {
        if (msg.sender != address(this)) revert OnlySelf();
        address user;
        address input;
        uint256 amount;
        uint256 expiry;
        if (stop) {
            StopLimitOrder storage order = stopLimitOrders[id];
            if (!expired && order.user != caller) revert NotOrderOwner();
            if (order.id == 0 || order.status != OrderStatus.PENDING && order.status != OrderStatus.ACTIVATED) revert OrderCannotCancel();
            user = order.user;
            input = order.orderType == OrderType.STOP_LIMIT_BUY ? order.stablecoin : order.token;
            amount = order.amount; expiry = order.expiresAt;
            order.status = expired ? OrderStatus.EXPIRED : OrderStatus.CANCELLED;
            _removeFromActiveStopLimitOrders(id);
        } else {
            LimitOrder storage order = limitOrders[id];
            if (!expired && order.user != caller) revert NotOrderOwner();
            if (order.id == 0 || order.status != OrderStatus.PENDING) revert OrderNotPending();
            user = order.user;
            input = order.orderType == OrderType.LIMIT_BUY ? order.stablecoin : order.token;
            amount = order.amount; expiry = order.expiresAt;
            order.status = expired ? OrderStatus.EXPIRED : OrderStatus.CANCELLED;
            _removeFromActiveLimitOrders(id);
        }
        if (expired && (expiry == 0 || expiry > block.timestamp)) revert InvalidExpiry();
        vault.releaseTokens(input, amount);
        vault.pushTokens(input, user, amount);
        if (expired) emit OrderExpiredAndRefunded(id, user, amount);
        else if (stop) emit StopLimitCancelled(id);
        else {
            emit LimitOrderCancelled(id);
            _track(abi.encodeCall(ITransactionTracker.recordLimitOrderCancelled, (id, user)));
        }
    }

    function _track(bytes memory payload) internal {
        address tracker = address(transactionTracker);
        if (tracker == address(0)) return;
        // No returndata copy: broken analytics cannot exhaust settlement gas.
        bool success;
        assembly ("memory-safe") { success := call(500000, tracker, 0, add(payload, 32), mload(payload), 0, 0) }
        if (!success || tracker.code.length == 0) emit TrackerCallFailed(tracker, bytes4(payload));
    }

    function liabilitiesReady() external view returns (bool) { return escrowInitialized; }

    /// @notice Reserve old pending/activated escrow on a paused upgraded proxy.
    /// Cancellation/placement stay gated until every active order is migrated.
    function migrateEscrowReservations(uint256 maxCount) external onlyRole(DEFAULT_ADMIN_ROLE) whenPaused nonReentrant {
        if (escrowInitialized) revert MigrationAlreadyComplete();
        if (maxCount == 0 || maxCount > 100) revert ZeroAmount();
        uint256 processed;
        while (limitMigrationCursor < activeLimitOrderIds.length && processed < maxCount) {
            LimitOrder storage o = limitOrders[activeLimitOrderIds[limitMigrationCursor++]];
            if (o.status == OrderStatus.PENDING) vault.reserveTokens(o.orderType == OrderType.LIMIT_BUY ? o.stablecoin : o.token, o.amount);
            ++processed;
        }
        while (stopMigrationCursor < activeStopLimitOrderIds.length && processed < maxCount) {
            StopLimitOrder storage o = stopLimitOrders[activeStopLimitOrderIds[stopMigrationCursor++]];
            if (o.status == OrderStatus.PENDING || o.status == OrderStatus.ACTIVATED) vault.reserveTokens(o.orderType == OrderType.STOP_LIMIT_BUY ? o.stablecoin : o.token, o.amount);
            ++processed;
        }
        if (limitMigrationCursor == activeLimitOrderIds.length && stopMigrationCursor == activeStopLimitOrderIds.length) escrowInitialized = true;
    }

    function _grantRole(bytes32 role, address account) internal override returns (bool changed) {
        changed = super._grantRole(role, account);
        if (role == KEEPER_ROLE) keepers[account] = true;
    }

    function _revokeRole(bytes32 role, address account) internal override returns (bool changed) {
        changed = super._revokeRole(role, account);
        if (role == KEEPER_ROLE) keepers[account] = false;
    }

    function _validatePair(address stablecoin, address token) internal view {
        if (!vault.isStablecoin(stablecoin)) revert NotAStablecoin();
        if (vault.isStablecoin(token)) revert TokenIsStablecoin();
    }

    function _validateExpiry(uint256 expiresAt) internal view {
        if (expiresAt != 0 && expiresAt <= block.timestamp) revert InvalidExpiry();
    }

    function _addToActiveLimitOrders(uint256 orderId) internal {
        activeLimitOrderIds.push(orderId);
        _limitOrderActiveIndex[orderId] = activeLimitOrderIds.length;
    }

    function _addToActiveStopLimitOrders(uint256 orderId) internal {
        activeStopLimitOrderIds.push(orderId);
        _stopLimitOrderActiveIndex[orderId] = activeStopLimitOrderIds.length;
    }

    function _removeFromActiveLimitOrders(uint256 orderId) internal {
        uint256 idx = _limitOrderActiveIndex[orderId];
        if (idx == 0) return;
        uint256 i = idx - 1;
        uint256 last = activeLimitOrderIds.length - 1;
        if (i != last) {
            uint256 lastId = activeLimitOrderIds[last];
            activeLimitOrderIds[i] = lastId;
            _limitOrderActiveIndex[lastId] = idx;
        }
        activeLimitOrderIds.pop();
        delete _limitOrderActiveIndex[orderId];
    }

    function _removeFromActiveStopLimitOrders(uint256 orderId) internal {
        uint256 idx = _stopLimitOrderActiveIndex[orderId];
        if (idx == 0) return;
        uint256 i = idx - 1;
        uint256 last = activeStopLimitOrderIds.length - 1;
        if (i != last) {
            uint256 lastId = activeStopLimitOrderIds[last];
            activeStopLimitOrderIds[i] = lastId;
            _stopLimitOrderActiveIndex[lastId] = idx;
        }
        activeStopLimitOrderIds.pop();
        delete _stopLimitOrderActiveIndex[orderId];
    }
}

/*
 * ══════════════════════════════════════════════════════════════════════════════════
 *
 *              Paxlabs HyperPax-OS-Protocol LICENSE (HyperPax-OS-Protocol)
 *                 Copyright © 2026 Paxlabs Inc. All rights reserved.
 *           
 *
 * ══════════════════════════════════════════════════════════════════════════════════
 *
 *   HyperPax-OS-Protocol License — Summary (non-binding): You may read, use, deploy, 
 *   and integrate HyperPax-OS-Protocol. If you Modify and distribute/deploy 
 *   the Modified version, you must release your changes under this same license.
 *   NO Commercial License is required until you cross a Commercial Trigger 
 *   (e.g., Charged Fees > US$100,000 in any rolling 12-month period or in any 
 *   single calendar month, or Liquidity Under Control > US$10,000,000).
 *   This summary is for convenience only.
 *
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *   1) DEFINITION
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *
 *   1.1  "Licensed Work" means the HyperPax-OS-Protocol stack as released in this repository,
 *        including: (a) the core HyperPax-OS-Protocol execution engine, instruction/handler
 *        interfaces, example instruction libraries published by Paxlabs, SDK stubs, schemas,
 *        configuration, tests, tooling, bytecode/ABIs, and deployment scripts; (b) all
 *        documentation and technical specifications published by Paxlabs; and (c) all updates,
 *        patches, and new versions of the foregoing that Paxlabs publishes under this license.
 *
 *   1.2  "Charged Fees" means all monetary or in-kind value (fiat, crypto, tokens, credits,
 *        rebates, or other consideration) that You or Your Affiliates directly or indirectly
 *        receive or accrue in connection with operating, offering, or providing access to any
 *        product or service that is powered by, routed through, or materially enabled by the
 *        Licensed Work, including without limitation: (a) swap/trade/execution fees, positive-
 *        slippage capture, spreads, mark-ups, retained priority/tips; (b) maker/taker rebates,
 *        order-flow payments, routing/referral/affiliate fees, MEV/builder/bundle payments and
 *        other extractable-value shares; (c) subscription, seat, usage, API, or platform fees
 *        attributable to the Licensed Work; (d) performance/incentive/carried-interest fees,
 *        revenue shares, or similar participation; and (e) token grants, rewards, airdrops,
 *        distributions, or rebates received by or for You or Your Affiliates in consideration
 *        of or tied to such operations. Charged Fees are measured on a gross-receipts basis at
 *        fair-market value in USD when received or accrued, include amounts received by
 *        agents/designees or wallets You control, and must be reasonably allocated for bundles.
 *        Anti-avoidance: relabeling, splitting, routing through Affiliates/Related Parties,
 *        offsetting, or deferring does not exclude amounts; Affiliates/common control are
 *        aggregated; the Control or Benefit Principle applies.
 *
 *   1.3  "Commercial License" means a separate written agreement between Paxlabs and You
 *        (and/or Your Affiliates), that grants You the right to engage in Commercial Use of
 *        the Licensed Work subject to negotiated terms, conditions, and fees.
 *
 *   1.4  "Control or Benefit Principle." Triggers and obligations apply where you or your
 *        Affiliates control the relevant activity or benefit economically from it (directly
 *        or through agents/DAOs under your direction).
 *
 *   1.5  "Rolling Year" means any period of twelve (12) consecutive months measured on a
 *        rolling basis.
 *
 *   1.6  "Liquidity Under Control (LUC)" means the aggregate fair-market USD value of real,
 *        non-synthetic, non-levered, withdrawable assets that Your (or Your Affiliates'/
 *        agents') products, services, or code can instruct or cause to be moved or committed
 *        via the Licensed Work (e.g., wallet balances under automated control, committed
 *        liquidity, or programmatic authorization) at the time assessed.
 *
 *   1.7  "Modify" (and "Modified Work") means to change, fork, translate, extend, or create a
 *        derivative work of the Licensed Work, including: (a) altering source or bytecode;
 *        (b) creating plug-ins/modules/instruction programs that run in the same program/
 *        runtime or EVM address space (e.g., static/dynamic linking, delegatecall/proxy
 *        patterns); or (c) bundling the Licensed Work and additions as a single product.
 *
 *   1.8  "You" (and "Your") means the individual or legal entity exercising rights under this
 *        license, and its Affiliates. "Affiliates" are entities controlling, controlled by, or
 *        under common control with a party, directly or indirectly.
 *
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *   2) GRANT
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *
 *   2.1  Source and Object Use. Subject to Sections 3–11, Paxlabs grants You a worldwide,
 *        non-exclusive license to use, copy, distribute unmodified source/object forms of the
 *        Licensed Work.
 *
 *   2.2  Pure Caller Use (Integration-Only / Non-Modifying). Pure Caller Use means building or
 *        operating products or services that interact with the Licensed Work solely by forming
 *        calldata, submitting transactions, or reading state through published ABIs, APIs, or
 *        RPC endpoints, without distributing any Modified Work. Pure Caller Use is permitted
 *        under this License and does not, by itself, trigger any payment obligations or
 *        Commercial License. However, if in connection with Pure Caller Use You or Your
 *        Affiliates (a) charge or retain any fees, spreads, rebates, incentives, or other
 *        consideration; (b) meet any Trigger in Section 5.2; such use constitutes Commercial
 *        Use and requires obtaining a Commercial License from Paxlabs. Even where Pure Caller
 *        Use is not met, see §5.3 for the current enforcement waiver applicable to Volume
 *        Activities.
 *
 *   2.3  Audit/Research Safe Harbor. Security auditors and researchers may compile, test, and
 *        report on the Licensed Work in the course of good-faith security research.
 *
 *   2.4  For any distribution, public display, public performance, publication, reporting,
 *        disclosure, or other public communication of any portion of the Licensed Work or any
 *        analysis, results, or outputs derived from the Licensed Work, You must preserve all
 *        existing copyright, license, and attribution notices included in the Licensed Work
 *        and must include a reasonable attribution identifying the source as
 *        "HyperPaxeer — © Paxlabs Inc 2026" (or any successor notice included in the
 *        Licensed Work).
 *
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *   3) COPYLEFT FOR MODIFICATIONS & EXTENSIONS
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *
 *   3.1  If you Modify or distribute any portion of the Licensed Work, you must:
 *        A. Publish under this same license (LicenseRef-Paxlabs-HyperPax-OS-Protocol license),
 *           at no charge, complete corresponding source of any portions of Your work that
 *           modify, extend, incorporate, or otherwise rely on the Licensed Work;
 *        B. Preserve existing copyright, license, and third-party notices;
 *        C. Add prominent attribution: "Powered by HyperPax-OS-Protocol — © Paxlabs Inc 2026"
 *           in repository README and UI where applicable;
 *        D. Clearly mark changes and date of change;
 *        E. Provide build and deployment instructions sufficient for reproducibility.
 *
 *   3.2  This copyleft covers all forms of Modification, combination, or use of the Licensed
 *        Work in or with other code, products, or systems.
 *
 *   3.3  The obligations in §§3.1 A-B apply only to components that are derivative of the
 *        Licensed Work. Independent code that simply calls, interfaces with, or is distributed
 *        alongside the Licensed Work is not subject to this requirement.
 *
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *   4) NON-COMMERCIAL FREE USE
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *
 *   Non-commercial use (including experimentation, prototyping, hackathons, research, community
 *   pilots) is free of charge, subject to Section 3 for any Modifications; and provided that
 *   such use does not constitute or involve any activity described in Section 5.
 *
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *   5) COMMERCIAL TRIGGER
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *
 *   5.1  Commercial Use: Any Commercial Use of the Licensed Work requires a Commercial License
 *        from Paxlabs, unless otherwise expressly stated. Commercial Use means any use of the
 *        Licensed Work that provides, enables, or is integrated into any product, service,
 *        system, workflow, or operation from which You or Your Affiliates derive, or reasonably
 *        expect to derive, monetary or in-kind commercial value, directly or indirectly,
 *        including through Charged Fees or other consideration.
 *
 *   5.2  Without limiting §5.1, You (and Your Affiliates) must obtain a Commercial License from
 *        Paxlabs if any of the following occur:
 *        A. Aggregated Fees Trigger: Your aggregated Charged Fees attributable to usage of the
 *           Licensed Work exceed USD 100,000 in any Rolling Year.
 *        B. LUC Trigger: Your LUC exceeds USD 10,000,000 at any time.
 *        C. Operator/Liquidity Provider Direct-Use. You (or Your Affiliate) operate instruction
 *           programs or services (e.g., deploying, offering, or running products or services
 *           powered by, routed through, or materially enabled by the Licensed Work) or acting
 *           as a Liquidity Provider that directly exercise the Licensed Work (bypassing Paxeer
 *           Network/Paxlabs or other permitted/licensed interfaces) to capture fees or value
 *           and, in doing so, satisfy Triggers A or B above.
 *        You must aggregate commonly-controlled/Affiliated entities; no disaggregation, white-
 *        labeling, or similar structuring to avoid a Trigger is permitted. The Control or
 *        Benefit Principle applies.
 *
 *   5.3  Notwithstanding §§ 5.1 - 5.2, Paxlabs presently waives enforcement of the Commercial
 *        Triggers for parties whose activities consist primarily of routing order flow,
 *        aggregation, arbitrage, or market-making through the Licensed Work ("Volume
 *        Activities"), including where such parties (i) trade with their own or third-party
 *        capital and/or (ii) charge or retain fees, spreads, rebates, or other compensation.
 *        This waiver is not a license, creates no reliance rights, and is revocable by Paxlabs
 *        at any time in its sole discretion, including with respect to existing users, by
 *        (a) public notice in the project repository or (b) direct notice. Upon notice of
 *        revocation, you must within ten (10) days cease the Volume Activities or obtain a
 *        Commercial License; continued use thereafter constitutes unauthorized Commercial Use.
 *        This waiver does not excuse past breaches unrelated to this subsection.
 *
 *   5.4  Crossing any Trigger or any other Commercial Use requires you to contact Paxlabs
 *        within 15 days at license@Paxlabs.com to execute Commercial License. Commercial
 *        License terms are confidential and may change.
 *
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *   6) AUDIT
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *
 *   Once per year, Paxlabs may request an independent revenue/LUC audit (under NDA) at
 *   Paxlabs's expense; if under-reporting exceeds 5%, You reimburse reasonable audit costs in
 *   addition to other remedies. Paxlabs may also request an additional attestation "for cause"
 *   (objective indications of a Trigger). You must reasonably cooperate with any such
 *   attestation, including providing accurate records, logs, and other information reasonably
 *   necessary.
 *
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *   7) ADDITIONAL INTELLECTUAL PROPERTY TERMS
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *
 *   7.1  Patents. Paxlabs grants a limited, non-exclusive, worldwide license under Paxlabs's
 *        patent claims that read on the Licensed Work solely to the extent necessary to
 *        exercise the rights expressly granted to You under this License. Nothing in this
 *        License grants You any right to patent, claim, or seek protection for (a) the
 *        Licensed Work (whether modified or unmodified), (b) any Modification of the Licensed
 *        Work, or (c) any work or system that incorporates, combines with, or depends on the
 *        Licensed Work. This patent license terminates if you (or your Affiliates) stop using
 *        the Licensed Work or assert any patent claim against Paxlabs or compliant users of
 *        the Licensed Work. No implied patent license is granted beyond this clause.
 *
 *   7.2  Trademarks & Branding. Trademarks. No rights are granted to use any Paxlabs/
 *        HyperPax-OS-Protocol/Paxeer Network names, logos, or trademarks, or any "Powered by
 *        HyperPaxeer" or similar designation, except solely to make truthful statements of
 *        compatibility or integration. Any use must comply with Paxlabs's brand guidelines and
 *        may require separate written permission.
 *
 *   7.3  Except as expressly granted, no other rights (by implication, estoppel, or otherwise)
 *        are granted, copyrights, patents, trade secrets, trademarks, or other IP.
 *
 *   7.4  You must not suggest Paxlabs endorses or certifies Your product absent a written
 *        agreement.
 *
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *   8) WARRANTY & LIABILITY
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *
 *   THE LICENSED WORK IS PROVIDED BY PAXLABS "AS IS" AND "AS AVAILABLE," WITHOUT WARRANTIES OF
 *   ANY KIND, EXPRESS OR IMPLIED, INCLUDING MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE,
 *   NON-INFRINGEMENT, AND THAT OPERATION WILL BE UNINTERRUPTED OR ERROR-FREE. TO THE MAXIMUM
 *   EXTENT PERMITTED BY LAW, PAXLABS, ITS AFFILIATES AND CONTRIBUTORS ARE NOT LIABLE FOR
 *   INDIRECT, INCIDENTAL, SPECIAL, CONSEQUENTIAL, EXEMPLARY, OR PUNITIVE DAMAGES, OR LOST
 *   PROFITS/REVENUE/GOODWILL, ARISING FROM OR RELATED TO THIS LICENSE OR THE LICENSED WORK,
 *   EVEN IF ADVISED OF THE POSSIBILITY.
 *
 *   Nothing in this Section limits Paxlabs's ability to seek injunctive relief without bond in
 *   addition to other remedies or to enforce Your obligations under this License.
 *
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *   9) TERMINATION
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *
 *   Material breach, including any breach of §§ 2-9, not cured within 15 days of notice
 *   terminates this License. Prior compliant distributions survive. Sections 2.4, 3, 8, 10,
 *   11.3 survive termination.
 *
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *   10) GOVERNING LAW; VENUE; INJUNCTIONS
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *
 *   This License is governed by the laws of the State of New York, excluding conflict rules.
 *   The parties submit to the exclusive jurisdiction and venue of the state and federal courts
 *   located in New York County, New York (SDNY). Each party consents to injunctive relief
 *   (including specific performance) for actual or threatened breach.
 *
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *   11) NOTICES; ASSIGNMENT; ENTIRE AGREEMENT
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *
 *   11.1 Notices. Legal or any other notices to Paxlabs: legal@Paxlabs.com (with subject
 *        "HyperPaxeer Notice").
 *
 *   11.2 Third-Party Components. Portions of the Licensed Work may incorporate, bundle, or
 *        reference third-party components governed by their own licenses. You must comply with
 *        those third-party terms; nothing in this License limits rights granted by those
 *        licenses. Preserve all third-party copyright and license notices. A list of such
 *        components and licenses is provided in THIRD_PARTY_NOTICES (and/or in file headers)
 *        and may be updated from time to time.
 *
 *   11.3 Assignment. You may not assign this License (by law or otherwise) without Paxlabs's
 *        prior written consent; any unauthorized assignment is void. Paxlabs may assign freely.
 *
 *   11.4 Entire Agreement. This License is the entire agreement for the Licensed Work and
 *        supersedes prior understandings. If any provision is unenforceable, it will be
 *        modified to the minimum extent necessary to be enforceable; the remainder stays in
 *        effect. No waiver is effective unless in writing.
 *
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *   12) VERSIONING
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *
 *   Paxlabs may publish new or updated versions of this License from time to time. Each
 *   release of the Licensed Work is governed by the license version identified in the
 *   repository for that release. Paxlabs may also re-release the Licensed Work, or any
 *   portion of it, under different license terms in future releases.
 *
 * ═══════════════════════════════════════════════════════════════════════════════════════════════
 *   END OF LICENSE — Contact: license@Paxlabs.com  |  legal@Paxlabs.com
 * ═══════════════════════════════════════════════════════════════════════════════════════════════
 */
