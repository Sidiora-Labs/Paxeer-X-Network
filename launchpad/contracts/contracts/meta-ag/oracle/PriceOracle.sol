// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.27;

import {IPriceOracle} from "../interfaces/IPriceOracle.sol";
import {Initializable} from "../../base/Initializable.sol";
import {UUPSUpgradeable} from "../../base/UUPSUpgradeable.sol";
import {AccessControl} from "../../base/AccessControl.sol";
import {SidioraMath} from "../../libraries/SidioraMath.sol";
import {Pausable} from "../../base/Pausable.sol";

/// @title PriceOracle
/// @notice Multi-relayer, staleness-protected, TWAP-enabled price oracle (Sidiora Meta-AG).
/// @dev UUPS-upgradeable. Zero external deps — composes in-house bases only.
///
/// Spec: docs/architecture/pecor-sidiora-aggregator-spec.md §7.1
/// Migrated from dev/PriceOracle.sol with:
///   - `Ownable` → `AccessControl` (DEFAULT_ADMIN_ROLE + RELAYER_ROLE)
///   - Added `initialize(admin)` + `_authorizeUpgrade` gated by Timelock (S1)
///   - Added `__gap[50]` at storage tail for safe future upgrades (S12)
///   - Custom errors in place of require strings
///
/// Storage layout (append-only per S12):
///   slot 0: AccessControl._roles
///   slot 1: authorizedRelayers
///   slot 2: _tokenConfigs
///   slot 3: _latestPrices
///   slot 4: _priceHistory
///   slot 5: _currentRounds
///   slot 6: _priceCumulativeLast
///   slot 7: _lastCumulativeTimestamp
///   slot 8: _twapSnapshots
///   slot 9: _twapSnapshotIndex
///   slot 10: _registeredTokens
///   slot 11: _tokenIndex
///   slot 12..61: __gap[50]
contract PriceOracle is
    IPriceOracle,
    Initializable,
    UUPSUpgradeable,
    AccessControl,
    Pausable
{
    bytes32 public constant RELAYER_ROLE = keccak256("RELAYER_ROLE");

    uint8 public constant override PRICE_DECIMALS = 18;
    uint256 public constant MAX_HISTORY_DEPTH = 1000;
    uint256 public constant MAX_TOKEN_PAGE_SIZE = 100;
    uint256 private constant _BPS_DENOMINATOR = 10_000;

    struct TWAPSnapshot {
        uint256 cumulativePrice;
        uint256 timestamp;
    }

    error ZeroAddress();
    error InvalidArrayLength();
    error InvalidConfig();
    error TokenAlreadyRegistered();
    error TokenNotConfigured();
    error PriceOutOfBounds();
    error StalePrice();
    error TwapWindowInvalid();
    error PriceJumpExceeded(address token, uint256 previousPrice, uint256 proposedPrice, uint256 limitBps);
    event MaxPriceJumpUpdated(address indexed token, uint256 previousLimitBps, uint256 limitBps);

    struct PriceStatus {
        bool registered;
        bool publishingPaused;
        bool fresh;
        bool heartbeatOverdue;
        bool twapReady;
        uint256 price;
        uint256 timestamp;
        uint256 roundId;
        uint256 oldestObservationTimestamp;
        uint256 retainedObservationCount;
    }

    /// @notice Relayer authorization mirror (kept as mapping for O(1) `isAuthorizedRelayer` view).
    /// @dev Synchronized by internal role hooks for grants, revocations and renunciations.
    mapping(address => bool) public authorizedRelayers;

    mapping(address => TokenConfig) private _tokenConfigs;
    mapping(address => PriceData) private _latestPrices;
    mapping(address => mapping(uint256 => PriceData)) private _priceHistory;
    mapping(address => uint256) private _currentRounds;

    mapping(address => uint256) private _priceCumulativeLast;
    mapping(address => uint256) private _lastCumulativeTimestamp;
    mapping(address => mapping(uint256 => TWAPSnapshot)) private _twapSnapshots;
    mapping(address => uint256) private _twapSnapshotIndex;

    address[] private _registeredTokens;
    mapping(address => uint256) private _tokenIndex;

    /// @dev Reserved storage for future upgrades (S12: append-only, 50 * 32 bytes).
    uint256[50] private __gap;

    // Appended after legacy slots: historical storage remains intact on upgrade.
    struct Observation { uint256 cumulative; uint256 timestamp; uint256 price; }
    mapping(address => mapping(uint256 => Observation)) private _observations;
    mapping(address => uint256) private _observationCount;
    mapping(address => uint256) private _firstBoundedRound;
    /// @notice Optional maximum change from the last accepted price; zero disables.
    mapping(address => uint256) public maxPriceJumpBps;


    /// @custom:oz-upgrades-unsafe-allow constructor
    constructor() {
        _disableInitializers();
    }

    /// @inheritdoc IPriceOracle
    function initialize(address admin) external override initializer {
        if (admin == address(0)) revert ZeroAddress();
        _grantRole(DEFAULT_ADMIN_ROLE, admin);
    }

    modifier tokenRegistered(address token) {
        if (!_tokenConfigs[token].isRegistered) revert TokenNotConfigured();
        _;
    }

    function _authorizeUpgrade(address) internal override onlyRole(DEFAULT_ADMIN_ROLE) {}

    /// @inheritdoc IPriceOracle
    function updatePrice(
        address token,
        uint256 price
    ) external override onlyRole(RELAYER_ROLE) whenNotPaused tokenRegistered(token) {
        _updatePrice(token, price);
    }

    /// @inheritdoc IPriceOracle
    function batchUpdatePrices(
        address[] calldata tokens,
        uint256[] calldata prices
    ) external override onlyRole(RELAYER_ROLE) whenNotPaused {
        uint256 len = tokens.length;
        if (len == 0 || len != prices.length) revert InvalidArrayLength();

        uint256 applied;
        for (uint256 i = 0; i < len; ++i) {
            if (_tokenConfigs[tokens[i]].isRegistered) {
                _updatePrice(tokens[i], prices[i]);
                ++applied;
            }
        }

        emit BatchPriceUpdate(msg.sender, applied, block.timestamp);
    }

    /// @inheritdoc IPriceOracle
    function getPrice(
        address token
    ) external view override tokenRegistered(token) returns (uint256 price) {
        PriceData storage data = _latestPrices[token];
        if (data.timestamp == 0) revert StalePrice();

        TokenConfig storage config = _tokenConfigs[token];
        if (block.timestamp - data.timestamp > config.maxStaleness) revert StalePrice();
        return data.price;
    }

    /// @inheritdoc IPriceOracle
    function getPriceWithTimestamp(
        address token
    )
        external
        view
        override
        tokenRegistered(token)
        returns (uint256 price, uint256 timestamp, uint256 roundId)
    {
        PriceData storage data = _latestPrices[token];
        if (data.timestamp == 0) revert StalePrice();
        TokenConfig storage config = _tokenConfigs[token];
        if (block.timestamp - data.timestamp > config.maxStaleness) revert StalePrice();
        return (data.price, data.timestamp, data.roundId);
    }

    /// @inheritdoc IPriceOracle
    function getTWAP(
        address token,
        uint256 period
    ) external view override tokenRegistered(token) returns (uint256 twapPrice) {
        if (period == 0 || period > block.timestamp) revert TwapWindowInvalid();
        PriceData storage latest = _latestPrices[token];
        if (latest.timestamp == 0 || block.timestamp - latest.timestamp > _tokenConfigs[token].maxStaleness) revert StalePrice();
        uint256 count = _observationCount[token];
        if (count == 0) revert TwapWindowInvalid();
        uint256 target = block.timestamp - period;
        uint256 low = count > MAX_HISTORY_DEPTH ? count - MAX_HISTORY_DEPTH : 0;
        uint256 high = count;
        if (_observations[token][low % MAX_HISTORY_DEPTH].timestamp > target) revert TwapWindowInvalid();
        // Upper-bound search selects the last observation at or before the exact target.
        while (low + 1 < high) {
            uint256 mid = (low + high) / 2;
            if (_observations[token][mid % MAX_HISTORY_DEPTH].timestamp <= target) low = mid;
            else high = mid;
        }
        Observation storage start = _observations[token][low % MAX_HISTORY_DEPTH];
        uint256 startCumulative = start.cumulative + start.price * (target - start.timestamp);
        uint256 endCumulative = _priceCumulativeLast[token] + latest.price * (block.timestamp - latest.timestamp);
        return (endCumulative - startCumulative) / period;
    }

    /// @inheritdoc IPriceOracle
    function isPriceStale(address token) external view override returns (bool stale) {
        if (!_tokenConfigs[token].isRegistered) return true;
        PriceData storage data = _latestPrices[token];
        if (data.timestamp == 0) return true;
        return (block.timestamp - data.timestamp) > _tokenConfigs[token].maxStaleness;
    }

    /// @inheritdoc IPriceOracle
    function getPrices(
        address[] calldata tokens
    )
        external
        view
        override
        returns (uint256[] memory prices, uint256[] memory timestamps, bool[] memory staleFlags)
    {
        uint256 len = tokens.length;
        prices = new uint256[](len);
        timestamps = new uint256[](len);
        staleFlags = new bool[](len);

        for (uint256 i = 0; i < len; ++i) {
            PriceData storage data = _latestPrices[tokens[i]];
            TokenConfig storage config = _tokenConfigs[tokens[i]];
            if (!config.isRegistered || data.timestamp == 0) {
                staleFlags[i] = true;
                continue;
            }
            prices[i] = data.price;
            timestamps[i] = data.timestamp;
            staleFlags[i] = (block.timestamp - data.timestamp) > config.maxStaleness;
        }
    }

    /// @inheritdoc IPriceOracle
    function getPriceHistory(
        address token,
        uint256 count
    ) external view override tokenRegistered(token) returns (PriceData[] memory history) {
        uint256 currentRound = _currentRounds[token];
        if (count > currentRound) count = currentRound;
        if (count > MAX_HISTORY_DEPTH) count = MAX_HISTORY_DEPTH;

        history = new PriceData[](count);
        for (uint256 i = 0; i < count; ++i) {
            history[i] = _priceHistory[token][currentRound - i];
        }
    }

    /// @inheritdoc IPriceOracle
    function getLatestRound(
        address token
    ) external view override tokenRegistered(token) returns (PriceData memory data) {
        return _latestPrices[token];
    }

    /// @inheritdoc IPriceOracle
    function getRoundData(
        address token,
        uint256 roundId
    ) external view override tokenRegistered(token) returns (PriceData memory data) {
        if (roundId == 0 || roundId > _currentRounds[token] || _priceHistory[token][roundId].timestamp == 0) revert TokenNotConfigured();
        return _priceHistory[token][roundId];
    }

    /// @inheritdoc IPriceOracle
    function getTokenConfig(
        address token
    ) external view override returns (TokenConfig memory config) {
        return _tokenConfigs[token];
    }

    /// @inheritdoc IPriceOracle
    function getRegisteredTokens() external view override returns (address[] memory tokens) {
        return _registeredTokens;
    }

    /// @notice Bounded discovery for monitoring and integrations; limit is capped at 100.
    function registeredTokenCount() external view returns (uint256) { return _registeredTokens.length; }

    function getRegisteredTokensPage(uint256 cursor, uint256 limit)
        external view returns (address[] memory tokens, uint256 nextCursor)
    {
        uint256 length = _registeredTokens.length;
        if (cursor >= length) return (new address[](0), length);
        if (limit > MAX_TOKEN_PAGE_SIZE) limit = MAX_TOKEN_PAGE_SIZE;
        uint256 remaining = length - cursor;
        if (limit > remaining) limit = remaining;
        tokens = new address[](limit);
        for (uint256 i; i < limit; ++i) tokens[i] = _registeredTokens[cursor + i];
        return (tokens, cursor + limit);
    }

    /// @notice Nonreverting publication and retained-window readiness. Pause only stops writes.
    /// @dev A zero/invalid period never reports TWAP ready; freshness uses the existing inclusive expiry.
    function getPriceStatus(address token, uint256 period) external view returns (PriceStatus memory status) {
        TokenConfig storage config = _tokenConfigs[token];
        PriceData storage latest = _latestPrices[token];
        status.registered = config.isRegistered;
        status.publishingPaused = _paused();
        status.price = latest.price;
        status.timestamp = latest.timestamp;
        status.roundId = latest.roundId;
        uint256 count = _observationCount[token];
        status.retainedObservationCount = count > MAX_HISTORY_DEPTH ? MAX_HISTORY_DEPTH : count;
        if (count != 0) {
            uint256 oldest = count > MAX_HISTORY_DEPTH ? count - MAX_HISTORY_DEPTH : 0;
            status.oldestObservationTimestamp = _observations[token][oldest % MAX_HISTORY_DEPTH].timestamp;
        }
        if (!config.isRegistered || latest.timestamp == 0 || latest.timestamp > block.timestamp) {
            status.heartbeatOverdue = config.isRegistered;
            return status;
        }
        uint256 age = block.timestamp - latest.timestamp;
        status.fresh = age <= config.maxStaleness;
        status.heartbeatOverdue = age > config.heartbeatInterval;
        status.twapReady = status.fresh && count != 0 && period != 0 && period <= block.timestamp
            && status.oldestObservationTimestamp <= block.timestamp - period;
    }

    /// @notice Governed opt-in jump bound; stale prices remain anchors until explicitly reconfigured.
    /// @dev This limits individual publications, not cumulative movement by a trusted relayer.
    function setMaxPriceJumpBps(address token, uint256 limitBps)
        external onlyRole(DEFAULT_ADMIN_ROLE) tokenRegistered(token)
    {
        if (limitBps > _BPS_DENOMINATOR) revert InvalidConfig();
        emit MaxPriceJumpUpdated(token, maxPriceJumpBps[token], limitBps);
        maxPriceJumpBps[token] = limitBps;
    }

    /// @inheritdoc IPriceOracle
    function isAuthorizedRelayer(
        address relayer
    ) external view override returns (bool authorized) {
        return hasRole(RELAYER_ROLE, relayer);
    }

    function getCurrentRound(address token) external view returns (uint256) {
        return _currentRounds[token];
    }

    /// @inheritdoc IPriceOracle
    function registerToken(
        address token,
        uint256 heartbeatInterval,
        uint256 deviationThresholdBps,
        uint256 minPrice,
        uint256 maxPrice,
        uint256 maxStaleness
    ) external override onlyRole(DEFAULT_ADMIN_ROLE) {
        if (token == address(0)) revert ZeroAddress();
        if (_tokenConfigs[token].isRegistered) revert TokenAlreadyRegistered();
        if (
            heartbeatInterval == 0 ||
            maxStaleness == 0 ||
            maxPrice <= minPrice ||
            deviationThresholdBps > _BPS_DENOMINATOR
        ) revert InvalidConfig();

        _tokenConfigs[token] = TokenConfig({
            isRegistered: true,
            heartbeatInterval: heartbeatInterval,
            deviationThresholdBps: deviationThresholdBps,
            maxPriceBound: maxPrice,
            minPriceBound: minPrice,
            maxStaleness: maxStaleness
        });

        _registeredTokens.push(token);
        _tokenIndex[token] = _registeredTokens.length;

        emit TokenRegistered(
            token,
            heartbeatInterval,
            deviationThresholdBps,
            minPrice,
            maxPrice,
            maxStaleness
        );
    }

    /// @inheritdoc IPriceOracle
    function updateTokenConfig(
        address token,
        TokenConfig calldata config
    ) external override onlyRole(DEFAULT_ADMIN_ROLE) tokenRegistered(token) {
        if (
            config.heartbeatInterval == 0 ||
            config.maxStaleness == 0 ||
            config.maxPriceBound <= config.minPriceBound ||
            config.deviationThresholdBps > _BPS_DENOMINATOR
        ) revert InvalidConfig();

        _tokenConfigs[token] = TokenConfig({
            isRegistered: true,
            heartbeatInterval: config.heartbeatInterval,
            deviationThresholdBps: config.deviationThresholdBps,
            maxPriceBound: config.maxPriceBound,
            minPriceBound: config.minPriceBound,
            maxStaleness: config.maxStaleness
        });

        emit TokenConfigUpdated(
            token,
            config.heartbeatInterval,
            config.deviationThresholdBps,
            config.minPriceBound,
            config.maxPriceBound,
            config.maxStaleness
        );
    }

    /// @inheritdoc IPriceOracle
    function setRelayer(
        address relayer,
        bool authorized
    ) external override onlyRole(DEFAULT_ADMIN_ROLE) {
        if (relayer == address(0)) revert ZeroAddress();
        authorizedRelayers[relayer] = authorized;
        if (authorized) {
            _grantRole(RELAYER_ROLE, relayer);
        } else {
            _revokeRole(RELAYER_ROLE, relayer);
        }
        emit RelayerUpdated(relayer, authorized);
    }

    /// @inheritdoc IPriceOracle
    function pause() external override {
        if (!hasRole(DEFAULT_ADMIN_ROLE, msg.sender)) _checkRole(PAUSER_ROLE, msg.sender);
        _pause();
    }

    /// @inheritdoc IPriceOracle
    function unpause() external override onlyRole(DEFAULT_ADMIN_ROLE) {
        _unpause();
    }

    function _grantRole(bytes32 role, address account) internal override returns (bool changed) {
        changed = super._grantRole(role, account);
        if (role == RELAYER_ROLE) authorizedRelayers[account] = true;
    }

    function _revokeRole(bytes32 role, address account) internal override returns (bool changed) {
        changed = super._revokeRole(role, account);
        if (role == RELAYER_ROLE) authorizedRelayers[account] = false;
    }

    // deviationThresholdBps is relayer publication-trigger metadata, not an on-chain
    // jump circuit breaker. Absolute bounds are enforced; relayers remain trusted.
    function _updatePrice(address token, uint256 price) internal {
        TokenConfig storage config = _tokenConfigs[token];

        if (price == 0 || price < config.minPriceBound || price > config.maxPriceBound) revert PriceOutOfBounds();

        uint256 limit = maxPriceJumpBps[token];
        uint256 previous = _latestPrices[token].price;
        if (limit != 0 && previous != 0) {
            uint256 difference = price > previous ? price - previous : previous - price;
            // Exact integer inequality: diff <= floor(previous * limit / 10000).
            if (difference > SidioraMath.mulDiv(previous, limit, _BPS_DENOMINATOR)) {
                revert PriceJumpExceeded(token, previous, price, limit);
            }
        }

        _updateTWAPAccumulator(token);

        uint256 newRound = _currentRounds[token] + 1;
        _currentRounds[token] = newRound;

        PriceData memory newData = PriceData({
            price: price,
            timestamp: block.timestamp,
            roundId: newRound,
            relayer: msg.sender
        });

        if (_firstBoundedRound[token] == 0) _firstBoundedRound[token] = newRound;
        if (newRound >= _firstBoundedRound[token] + MAX_HISTORY_DEPTH) delete _priceHistory[token][newRound - MAX_HISTORY_DEPTH];
        _priceHistory[token][newRound] = newData;
        _latestPrices[token] = newData;

        _takeTWAPSnapshot(token);

        emit PriceUpdated(token, price, newRound, msg.sender, block.timestamp);
    }

    function _updateTWAPAccumulator(address token) internal {
        PriceData storage latest = _latestPrices[token];

        if (latest.timestamp == 0) {
            _lastCumulativeTimestamp[token] = block.timestamp;
            return;
        }

        uint256 timeElapsed = block.timestamp - _lastCumulativeTimestamp[token];
        if (timeElapsed > 0) {
            _priceCumulativeLast[token] += latest.price * timeElapsed;
            _lastCumulativeTimestamp[token] = block.timestamp;
        }
    }

    function _takeTWAPSnapshot(address token) internal {
        uint256 count = _observationCount[token];
        // Coalesce same-block updates; the final price applies to the next interval.
        if (count > 0 && _observations[token][(count - 1) % MAX_HISTORY_DEPTH].timestamp == block.timestamp) {
            _observations[token][(count - 1) % MAX_HISTORY_DEPTH].price = _latestPrices[token].price;
            return;
        }
        _observations[token][count % MAX_HISTORY_DEPTH] = Observation(_priceCumulativeLast[token], block.timestamp, _latestPrices[token].price);
        _observationCount[token] = count + 1;
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
