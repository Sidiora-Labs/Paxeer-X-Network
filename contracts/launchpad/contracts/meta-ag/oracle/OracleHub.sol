// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.27;

import {IOracleHub} from "../interfaces/IOracleHub.sol";
import {IDataFeedAdapter} from "../interfaces/IDataFeedAdapter.sol";
import {SidioraMath} from "../../libraries/SidioraMath.sol";
import {IPriceOracle} from "../interfaces/IPriceOracle.sol";

import {Initializable} from "../../base/Initializable.sol";
import {UUPSUpgradeable} from "../../base/UUPSUpgradeable.sol";
import {AccessControl} from "../../base/AccessControl.sol";
import {Pausable} from "../../base/Pausable.sol";

/// @title OracleHub
/// @notice Meta-oracle aggregator — pluggable, prioritized, deviation-circuit-broken.
/// @dev UUPS-upgradeable. Composes exclusively in-house bases.
///
/// Spec: docs/architecture/pecor-sidiora-aggregator-spec.md §7.2
///
/// Aggregation semantics preserved from `dev/OracleHub.sol`:
///   - `getPrice` returns the first valid adapter in priority order (skips low-confidence /
///     stale / zero-price feeds).
///   - `getAggregatedPrice` returns a confidence-weighted median filtered by deviation
///     threshold (S8 deviation circuit-breaker) relative to the highest-priority valid feed.
///   - `getTWAP` is strict: invalid, stale or unavailable windows revert.
///
/// Storage layout (append-only per S12):
///   slot 0: AccessControl._roles
///   slot 1: _adapterIds (bytes32[])
///   slot 2: _adapters mapping
///   slot 3: _adapterAddresses mapping
///   slot 4: deviationThresholdBps
///   slot 5: minConfidence
///   slot 6: primaryOracle
///   slot 7..56: __gap[50]
contract OracleHub is
    IOracleHub,
    Initializable,
    UUPSUpgradeable,
    AccessControl,
    Pausable
{
    uint256 public constant BPS_DENOMINATOR = 10_000;
    uint256 public constant MAX_ADAPTERS = 20;

    error ZeroAddress();
    error InvalidConfig();
    error MaxAdaptersReached();
    error AdapterAlreadyRegistered();
    error AdapterNotFound();
    error NoActiveAdapters();
    error SourceIdConflict();
    event AdapterRemoved(bytes32 indexed sourceId, address indexed adapter);

    bytes32[] private _adapterIds;
    mapping(bytes32 => AdapterInfo) private _adapters;
    mapping(address => bool) private _adapterAddresses;

    uint256 public override deviationThresholdBps;
    uint256 public override minConfidence;
    address public primaryOracle;

    /// @dev Reserved storage for future upgrades (S12).
    uint256[50] private __gap;

    /// @custom:oz-upgrades-unsafe-allow constructor
    constructor() {
        _disableInitializers();
    }

    /// @inheritdoc IOracleHub
    function initialize(
        address _primaryOracle,
        uint256 _deviationBps,
        uint256 _minConfidence,
        address admin
    ) external override initializer {
        if (_primaryOracle == address(0) || admin == address(0)) revert ZeroAddress();
        if (_deviationBps > BPS_DENOMINATOR || _minConfidence > BPS_DENOMINATOR) {
            revert InvalidConfig();
        }
        primaryOracle = _primaryOracle;
        deviationThresholdBps = _deviationBps;
        minConfidence = _minConfidence;
        _grantRole(DEFAULT_ADMIN_ROLE, admin);
    }

    function _authorizeUpgrade(address) internal override onlyRole(DEFAULT_ADMIN_ROLE) {}

    function pause() external override {
        if (!hasRole(DEFAULT_ADMIN_ROLE, msg.sender)) _checkRole(PAUSER_ROLE, msg.sender);
        _pause();
    }

    function unpause() external override onlyRole(DEFAULT_ADMIN_ROLE) {
        _unpause();
    }

    function setPrimaryOracle(address oracle) external override onlyRole(DEFAULT_ADMIN_ROLE) {
        if (oracle == address(0)) revert ZeroAddress();
        primaryOracle = oracle;
        emit PrimaryOracleUpdated(oracle);
    }

    function setDeviationThreshold(
        uint256 deviationBps
    ) external override onlyRole(DEFAULT_ADMIN_ROLE) {
        if (deviationBps > BPS_DENOMINATOR) revert InvalidConfig();
        deviationThresholdBps = deviationBps;
        emit DeviationThresholdUpdated(deviationBps);
    }

    function setMinConfidence(
        uint256 _minConfidence
    ) external override onlyRole(DEFAULT_ADMIN_ROLE) {
        if (_minConfidence > BPS_DENOMINATOR) revert InvalidConfig();
        minConfidence = _minConfidence;
        emit MinConfidenceUpdated(_minConfidence);
    }

    /// @inheritdoc IOracleHub
    function registerAdapter(
        address adapter,
        uint256 priority
    ) external override onlyRole(DEFAULT_ADMIN_ROLE) {
        if (adapter == address(0)) revert ZeroAddress();
        if (_adapterAddresses[adapter]) revert AdapterAlreadyRegistered();
        if (_adapterIds.length >= MAX_ADAPTERS) revert MaxAdaptersReached();

        bytes32 sid = IDataFeedAdapter(adapter).sourceId();
        if (_adapters[sid].adapter != address(0)) revert AdapterAlreadyRegistered();

        string memory name = IDataFeedAdapter(adapter).adapterName();

        _adapters[sid] = AdapterInfo({
            adapter: adapter,
            priority: priority,
            active: true,
            sourceId: sid,
            name: name
        });
        _adapterAddresses[adapter] = true;
        _adapterIds.push(sid);

        _sortAdaptersByPriority();

        emit AdapterRegistered(sid, adapter, priority);
    }

    /// @inheritdoc IOracleHub
    function deactivateAdapter(bytes32 sid) external override onlyRole(DEFAULT_ADMIN_ROLE) {
        if (_adapters[sid].adapter == address(0)) revert AdapterNotFound();
        _adapters[sid].active = false;
        emit AdapterDeactivated(sid);
    }

    /// @inheritdoc IOracleHub
    function activateAdapter(bytes32 sid) external override onlyRole(DEFAULT_ADMIN_ROLE) {
        if (_adapters[sid].adapter == address(0)) revert AdapterNotFound();
        _adapters[sid].active = true;
        emit AdapterActivated(sid);
    }

    /// @inheritdoc IOracleHub
    function setAdapterPriority(
        bytes32 sid,
        uint256 newPriority
    ) external override onlyRole(DEFAULT_ADMIN_ROLE) {
        if (_adapters[sid].adapter == address(0)) revert AdapterNotFound();
        _adapters[sid].priority = newPriority;
        _sortAdaptersByPriority();
        emit AdapterPriorityUpdated(sid, newPriority);
    }

    /// @inheritdoc IOracleHub
    function getPrice(
        address token
    ) external view override whenNotPaused returns (uint256 price) {
        uint256 len = _adapterIds.length;
        if (len == 0) revert NoActiveAdapters();

        for (uint256 i = 0; i < len; ++i) {
            AdapterInfo storage info = _adapters[_adapterIds[i]];
            if (!info.active) continue;

            (bool valid, IDataFeedAdapter.FeedPrice memory feed) = _validatedFeed(info.adapter, token);
            if (!valid) continue;

            return feed.price;
        }

        revert NoActiveAdapters();
    }

    /// @inheritdoc IOracleHub
    function getAggregatedPrice(
        address token
    )
        external
        view
        override
        whenNotPaused
        returns (AggregatedPrice memory result)
    {
        uint256 len = _adapterIds.length;
        if (len == 0) return result;

        uint256[] memory prices = new uint256[](len);
        uint256[] memory confidences = new uint256[](len);
        uint256[] memory timestamps = new uint256[](len);
        bytes32 primarySrc;
        uint256 validCount;
        uint256 bestPriority = type(uint256).max;

        for (uint256 i = 0; i < len; ++i) {
            AdapterInfo storage info = _adapters[_adapterIds[i]];
            if (!info.active) continue;

            (bool valid, IDataFeedAdapter.FeedPrice memory feed) = _validatedFeed(info.adapter, token);
            if (!valid) continue;

            prices[validCount] = feed.price;
            confidences[validCount] = feed.confidence;
            timestamps[validCount] = feed.timestamp;

            if (info.priority < bestPriority) {
                bestPriority = info.priority;
                primarySrc = info.sourceId;
            }

            ++validCount;
        }

        if (validCount == 0) return result;

        uint256 referencePrice = prices[0];
        uint256 filteredCount;
        uint256 totalConfidence;

        for (uint256 i = 0; i < validCount; ++i) {
            uint256 p = prices[i];
            uint256 c = confidences[i];
            uint256 dev = p > referencePrice
                ? SidioraMath.mulDiv(p - referencePrice, BPS_DENOMINATOR, referencePrice)
                : SidioraMath.mulDiv(referencePrice - p, BPS_DENOMINATOR, referencePrice);
            if (dev > deviationThresholdBps) continue;

            prices[filteredCount] = p;
            confidences[filteredCount] = c;
            if (filteredCount == 0 || timestamps[i] < result.timestamp) result.timestamp = timestamps[i];
            totalConfidence += c;
            ++filteredCount;
        }

        if (filteredCount == 0 || totalConfidence == 0) return result;

        // Bounded insertion sort, retaining confidence attached to each source price.
        for (uint256 i = 1; i < filteredCount; ++i) {
            uint256 p = prices[i]; uint256 c = confidences[i]; uint256 j = i;
            while (j > 0 && prices[j - 1] > p) {
                prices[j] = prices[j - 1]; confidences[j] = confidences[j - 1]; --j;
            }
            prices[j] = p; confidences[j] = c;
        }
        uint256 weight;
        for (uint256 i; i < filteredCount; ++i) {
            weight += confidences[i];
            if (weight >= (totalConfidence + 1) / 2) { result.price = prices[i]; break; }
        }
        result.confidence = totalConfidence / filteredCount;
        result.sourceCount = filteredCount;
        result.primarySource = primarySrc;
    }

    /// @inheritdoc IOracleHub
    function getPriceFromSource(
        address token,
        bytes32 sid
    ) external view override returns (IDataFeedAdapter.FeedPrice memory feed) {
        AdapterInfo storage info = _adapters[sid];
        if (info.adapter == address(0)) revert AdapterNotFound();
        return IDataFeedAdapter(info.adapter).getFeedPrice(token);
    }

    /// @inheritdoc IOracleHub
    function getPricesBatch(
        address[] calldata tokens
    )
        external
        view
        override
        whenNotPaused
        returns (uint256[] memory prices, uint256[] memory confidences)
    {
        uint256 tokenLen = tokens.length;
        prices = new uint256[](tokenLen);
        confidences = new uint256[](tokenLen);

        uint256 adapterLen = _adapterIds.length;
        for (uint256 t = 0; t < tokenLen; ++t) {
            for (uint256 i = 0; i < adapterLen; ++i) {
                AdapterInfo storage info = _adapters[_adapterIds[i]];
                if (!info.active) continue;

                (bool valid, IDataFeedAdapter.FeedPrice memory feed) = _validatedFeed(info.adapter, tokens[t]);
                if (!valid) continue;

                prices[t] = feed.price;
                confidences[t] = feed.confidence;
                break;
            }
        }
    }

    /// @inheritdoc IOracleHub
    function getTWAP(
        address token,
        uint256 period
    ) external view override whenNotPaused returns (uint256 twapPrice) {
        if (period == 0) revert InvalidConfig();
        return IPriceOracle(primaryOracle).getTWAP(token, period);
    }

    /// @inheritdoc IOracleHub
    function isPriceAvailable(
        address token
    ) external view override returns (bool available, uint256 bestConfidence) {
        if (_paused()) return (false, 0);
        uint256 len = _adapterIds.length;
        for (uint256 i = 0; i < len; ++i) {
            AdapterInfo storage info = _adapters[_adapterIds[i]];
            if (!info.active) continue;

            (bool valid, IDataFeedAdapter.FeedPrice memory feed) = _validatedFeed(info.adapter, token);
            if (!valid) continue;

            if (feed.confidence > bestConfidence) {
                bestConfidence = feed.confidence;
                available = true;
            }
        }
    }

    function getAdapters() external view override returns (AdapterInfo[] memory adapters) {
        uint256 len = _adapterIds.length;
        adapters = new AdapterInfo[](len);
        for (uint256 i = 0; i < len; ++i) {
            adapters[i] = _adapters[_adapterIds[i]];
        }
    }

    function getAdapter(bytes32 sid) external view override returns (AdapterInfo memory info) {
        if (_adapters[sid].adapter == address(0)) revert AdapterNotFound();
        return _adapters[sid];
    }

    function adapterCount() external view override returns (uint256 count) {
        return _adapterIds.length;
    }

    /// @notice Remove a retired source, reclaiming its bounded capacity and address.
    function removeAdapter(bytes32 sid) external override onlyRole(DEFAULT_ADMIN_ROLE) {
        address adapter = _adapters[sid].adapter;
        if (adapter == address(0)) revert AdapterNotFound();
        delete _adapterAddresses[adapter];
        delete _adapters[sid];
        for (uint256 i; i < _adapterIds.length; ++i) {
            if (_adapterIds[i] != sid) continue;
            for (uint256 j = i; j + 1 < _adapterIds.length; ++j) _adapterIds[j] = _adapterIds[j + 1];
            _adapterIds.pop(); break;
        }
        emit AdapterRemoved(sid, adapter);
    }

    // External self-call confines metadata reverts, malformed ABI and timestamp failures
    // to one source. Only bounded, sane, confident feeds reach any selection API.
    function validatedFeed(address adapter, address token) external view returns (IDataFeedAdapter.FeedPrice memory feed) {
        if (msg.sender != address(this)) revert InvalidConfig();
        feed = IDataFeedAdapter(adapter).getFeedPrice(token);
        if (feed.price == 0 || feed.price > type(uint256).max / BPS_DENOMINATOR || feed.timestamp == 0 || feed.timestamp > block.timestamp ||
            feed.confidence == 0 || feed.confidence > BPS_DENOMINATOR || feed.confidence < minConfidence ||
            block.timestamp - feed.timestamp > IDataFeedAdapter(adapter).maxStaleness()) revert InvalidConfig();
    }

    function _validatedFeed(address adapter, address token) internal view returns (bool valid, IDataFeedAdapter.FeedPrice memory feed) {
        try this.validatedFeed(adapter, token) returns (IDataFeedAdapter.FeedPrice memory f) { return (true, f); }
        catch { return (false, feed); }
    }

    /// @dev Insertion sort by priority ASC. O(n^2) — fine since MAX_ADAPTERS == 20.
    function _sortAdaptersByPriority() internal {
        uint256 n = _adapterIds.length;
        for (uint256 i = 1; i < n; ++i) {
            bytes32 key = _adapterIds[i];
            uint256 keyPriority = _adapters[key].priority;
            int256 j = int256(i) - 1;
            while (j >= 0 && _adapters[_adapterIds[uint256(j)]].priority > keyPriority) {
                _adapterIds[uint256(j + 1)] = _adapterIds[uint256(j)];
                --j;
            }
            _adapterIds[uint256(j + 1)] = key;
        }
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
