// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.27;

import {IERC20Minimal} from "../interfaces/IERC20Minimal.sol";
import {IMetaAGRouter} from "../interfaces/IMetaAGRouter.sol";
import {IProtocolAdapter} from "../interfaces/IProtocolAdapter.sol";
import {IOracleHub} from "../interfaces/IOracleHub.sol";
import {Initializable} from "../../base/Initializable.sol";
import {UUPSUpgradeable} from "../../base/UUPSUpgradeable.sol";
import {AccessControl} from "../../base/AccessControl.sol";
import {ReentrancyGuard} from "../../base/ReentrancyGuard.sol";
import {Pausable} from "../../base/Pausable.sol";
import {Multicall} from "../../base/Multicall.sol";
import {TransferHelper} from "../../libraries/TransferHelper.sol";
import {SidioraMath} from "../../libraries/SidioraMath.sol";

/// @title MetaAGRouter — Canonical user-facing meta-router
/// @notice Polls every registered `IProtocolAdapter` for the best output, executes
///         the winning quote, oracle-sanity-checks the result, and supports
///         multi-hop routing across heterogeneous adapters (Vault + Sidiora + ...).
/// @dev Spec reference: `docs/architecture/pecor-sidiora-aggregator-spec.md` §7.10
///      (FROZEN 2026-04-24). Interface:
///      `contracts/meta-ag/interfaces/IMetaAGRouter.sol`. Port of
///      `dev/PECORRouter.sol` with the following frozen-surface divergences:
///        - Ownable to AccessControl (DEFAULT_ADMIN_ROLE held by Timelock, S1).
///        - OpenZeppelin SafeERC20.forceApprove replaced with in-house
///          `TransferHelper.safeApprove(token, spender, 0)` then `safeApprove
///          (token, spender, amount)`: the S9 zero-first reset pattern. Final
///          cleanup resets to zero after the adapter call (defense-in-depth).
///        - OpenZeppelin Math.mulDiv replaced with `SidioraMath.mulDiv`.
///        - Require-string reverts replaced with custom errors registered
///          under `ERRORS.router.*` in the test helper.
///
/// Inheritance (spec §7.10):
///   IMetaAGRouter, Initializable, UUPSUpgradeable, AccessControl,
///   ReentrancyGuard, Pausable, Multicall
///
/// Roles:
///   - DEFAULT_ADMIN_ROLE → Timelock at deploy (invariant S1)
///
/// Storage layout (append-only per S12):
///   slot 0:  AccessControl._roles          (mapping)
///   slot 1:  _adapterList                  (AdapterEntry[])
///   slot 2:  _adapterById                  (mapping: adapterId => address)
///   slot 3:  _adapterAddresses             (mapping: address => bool dedupe)
///   slot 4:  oracleHub                     (address stored as IOracleHub)
///   slot 5:  maxOracleSanityDeviation      (uint256)
///   slot 6:  oracleSanityEnabled           (bool)
///   slot 7..56: __gap[50]
///
/// Invariants enforced by this contract:
///   - S1  — UUPS `_authorizeUpgrade` gated on DEFAULT_ADMIN_ROLE (Timelock).
///   - S3  — `swapMultiHop` re-queries `getQuote` with the actual intermediate
///           amount before each hop; slippage guard fires on per-hop output.
///   - S4  — `_oracleSanityCheck` skips when either price is unavailable and
///           reverts when deviation exceeds `maxOracleSanityDeviation`.
///   - S9  — `TransferHelper.safeApprove(token, adapter, 0)` fires before AND
///           after every adapter call (no dangling allowances).
///   - S12 — `__gap[50]` at the tail.
contract MetaAGRouter is
    IMetaAGRouter,
    Initializable,
    UUPSUpgradeable,
    AccessControl,
    ReentrancyGuard,
    Pausable,
    Multicall
{

    uint256 public constant BPS_DENOMINATOR = 10_000;
    uint256 public constant MAX_ADAPTERS = 20;
    uint256 public constant MAX_HOPS = 5;

    error InvalidPath();
    error TransferAmountMismatch();
    event AdapterRemoved(bytes32 indexed adapterId, address adapter);
    error ZeroAddress();
    error ZeroAmount();
    error SameToken();
    error DeadlineExpired();
    error InvalidBps();
    error MaxAdaptersReached();
    error AdapterAlreadyRegistered();
    error AdapterNotFound();
    error AdapterInactive();
    error NoAdaptersAvailable();
    error BestQuoteUnavailable();
    error QuoteUnavailable();
    error SlippageTooHigh();
    error MaxHopsExceeded();
    error TooFewHops();
    error OracleSanityFailed();

    AdapterEntry[] private _adapterList;

    mapping(bytes32 => address) private _adapterById;
    mapping(address => bool) private _adapterAddresses;

    /// @notice OracleHub consulted by the sanity check (spec §7.10 / S4).
    /// @dev Stored as `address` to satisfy the frozen interface return type
    ///      (`IMetaAGRouter.oracleHub()` returns `address`). Cast to
    ///      {IOracleHub} inside {_oracleSanityCheck}.
    address public override oracleHub;

    /// @notice Max deviation (BPS) before `_oracleSanityCheck` reverts.
    uint256 public override maxOracleSanityDeviation;

    /// @notice Master toggle for the oracle sanity check.
    bool public override oracleSanityEnabled;

    /// @dev Reserved storage for future upgrades (S12: append-only, 50 * 32 bytes).
    uint256[50] private __gap;

    modifier ensure(uint256 deadline) {
        if (deadline != 0 && block.timestamp > deadline) revert DeadlineExpired();
        _;
    }

    /// @custom:oz-upgrades-unsafe-allow constructor
    constructor() {
        _disableInitializers();
    }

    /// @inheritdoc IMetaAGRouter
    function initialize(
        address oracleHub_,
        uint256 maxSanityDeviation,
        address admin
    ) external override initializer {
        if (oracleHub_ == address(0)) revert ZeroAddress();
        if (admin == address(0)) revert ZeroAddress();
        if (maxSanityDeviation > BPS_DENOMINATOR) revert InvalidBps();

        _initReentrancyGuard();
        oracleHub = oracleHub_;
        maxOracleSanityDeviation = maxSanityDeviation;
        oracleSanityEnabled = true;
        _grantRole(DEFAULT_ADMIN_ROLE, admin);
    }

    function _authorizeUpgrade(address) internal override onlyRole(DEFAULT_ADMIN_ROLE) {}

    /// @inheritdoc IMetaAGRouter
    function pause() external override {
        if (!hasRole(DEFAULT_ADMIN_ROLE, msg.sender)) _checkRole(PAUSER_ROLE, msg.sender);
        _pause();
    }

    /// @inheritdoc IMetaAGRouter
    function unpause() external override onlyRole(DEFAULT_ADMIN_ROLE) {
        _unpause();
    }

    /// @inheritdoc IMetaAGRouter
    function setOracleHub(address hub) external override onlyRole(DEFAULT_ADMIN_ROLE) {
        if (hub == address(0)) revert ZeroAddress();
        oracleHub = hub;
        emit OracleHubUpdated(hub);
    }

    /// @inheritdoc IMetaAGRouter
    function setOracleSanityDeviation(
        uint256 bps
    ) external override onlyRole(DEFAULT_ADMIN_ROLE) {
        if (bps > BPS_DENOMINATOR) revert InvalidBps();
        maxOracleSanityDeviation = bps;
        emit OracleSanityDeviationUpdated(bps);
    }

    /// @inheritdoc IMetaAGRouter
    function setOracleSanityEnabled(
        bool enabled
    ) external override onlyRole(DEFAULT_ADMIN_ROLE) {
        oracleSanityEnabled = enabled;
        emit OracleSanityEnabledUpdated(enabled);
    }

    /// @inheritdoc IMetaAGRouter
    function registerAdapter(address adapter) external override onlyRole(DEFAULT_ADMIN_ROLE) {
        if (adapter == address(0)) revert ZeroAddress();
        if (_adapterAddresses[adapter]) revert AdapterAlreadyRegistered();
        if (_adapterList.length >= MAX_ADAPTERS) revert MaxAdaptersReached();

        bytes32 aid = IProtocolAdapter(adapter).adapterId();
        if (_adapterById[aid] != address(0)) revert AdapterAlreadyRegistered();

        string memory name = IProtocolAdapter(adapter).adapterName();

        _adapterList.push(
            AdapterEntry({adapterId: aid, adapter: adapter, active: true, name: name})
        );
        _adapterById[aid] = adapter;
        _adapterAddresses[adapter] = true;

        emit AdapterRegistered(aid, adapter, name);
    }

    /// @notice Retire an inactive adapter and reclaim its ID and capacity.
    function removeAdapter(bytes32 id) external override onlyRole(DEFAULT_ADMIN_ROLE) {
        uint256 idx = _findAdapterIndex(id);
        if (_adapterList[idx].active) revert AdapterInactive();
        address old = _adapterList[idx].adapter;
        delete _adapterById[id];
        delete _adapterAddresses[old];
        _adapterList[idx] = _adapterList[_adapterList.length - 1];
        _adapterList.pop();
        emit AdapterRemoved(id, old);
    }

    /// @inheritdoc IMetaAGRouter
    function deactivateAdapter(
        bytes32 adapterId_
    ) external override onlyRole(DEFAULT_ADMIN_ROLE) {
        uint256 idx = _findAdapterIndex(adapterId_);
        _adapterList[idx].active = false;
        emit AdapterDeactivated(adapterId_);
    }

    /// @inheritdoc IMetaAGRouter
    function activateAdapter(
        bytes32 adapterId_
    ) external override onlyRole(DEFAULT_ADMIN_ROLE) {
        uint256 idx = _findAdapterIndex(adapterId_);
        _adapterList[idx].active = true;
        emit AdapterActivated(adapterId_);
    }

    /// @inheritdoc IMetaAGRouter
    function getBestQuote(
        address tokenIn,
        address tokenOut,
        uint256 amountIn
    ) public view override returns (BestQuote memory best) {
        uint256 len = _adapterList.length;
        for (uint256 i = 0; i < len; ++i) {
            AdapterEntry storage entry = _adapterList[i];
            if (!entry.active) continue;

            IProtocolAdapter.QuoteResult memory q = _safeQuote(entry.adapter, tokenIn, tokenOut, amountIn);
            if (!q.available || q.amountOut == 0) continue;

            if (!best.found || q.amountOut > best.amountOut) {
                best = BestQuote({
                    amountOut: q.amountOut,
                    priceImpactBps: q.priceImpactBps,
                    feeBps: q.feeBps,
                    feeAmount: q.feeAmount,
                    adapterId: entry.adapterId,
                    adapter: entry.adapter,
                    adapterData: q.adapterData,
                    found: true
                });
            }
        }
    }

    /// @inheritdoc IMetaAGRouter
    function getAllQuotes(
        address tokenIn,
        address tokenOut,
        uint256 amountIn
    )
        external
        view
        override
        returns (
            IProtocolAdapter.QuoteResult[] memory quotes,
            bytes32[] memory adapterIds,
            string[] memory names
        )
    {
        uint256 len = _adapterList.length;
        quotes = new IProtocolAdapter.QuoteResult[](len);
        adapterIds = new bytes32[](len);
        names = new string[](len);

        for (uint256 i = 0; i < len; ++i) {
            AdapterEntry storage entry = _adapterList[i];
            adapterIds[i] = entry.adapterId;
            names[i] = entry.name;
            if (!entry.active) continue;
            quotes[i] = _safeQuote(entry.adapter, tokenIn, tokenOut, amountIn);
        }
    }

    /// @inheritdoc IMetaAGRouter
    function swapBestRoute(
        address tokenIn,
        address tokenOut,
        uint256 amountIn,
        uint256 amountOutMin,
        uint256 deadline
    )
        external
        override
        nonReentrant
        whenNotPaused
        ensure(deadline)
        returns (uint256 amountOut)
    {
        if (tokenIn == tokenOut) revert SameToken();
        if (amountIn == 0) revert ZeroAmount();
        if (_adapterList.length == 0) revert NoAdaptersAvailable();

        BestQuote memory best = getBestQuote(tokenIn, tokenOut, amountIn);
        if (!best.found) revert BestQuoteUnavailable();
        if (best.amountOut < amountOutMin) revert SlippageTooHigh();

        amountOut = _executeAdapterSwap(
            best.adapter,
            tokenIn,
            tokenOut,
            amountIn,
            amountOutMin,
            msg.sender,
            msg.sender,
            deadline,
            best.adapterData
        );

        if (oracleSanityEnabled) {
            _oracleSanityCheck(tokenIn, tokenOut, amountIn, amountOut);
        }

        emit BestRouteSwap(
            msg.sender,
            tokenIn,
            tokenOut,
            amountIn,
            amountOut,
            best.adapterId
        );
    }

    /// @inheritdoc IMetaAGRouter
    function swapViaAdapter(
        bytes32 adapterId_,
        address tokenIn,
        address tokenOut,
        uint256 amountIn,
        uint256 amountOutMin,
        uint256 deadline
    )
        external
        override
        nonReentrant
        whenNotPaused
        ensure(deadline)
        returns (uint256 amountOut)
    {
        if (tokenIn == tokenOut) revert SameToken();
        if (amountIn == 0) revert ZeroAmount();

        address adapter = _adapterById[adapterId_];
        if (adapter == address(0)) revert AdapterNotFound();
        {
            uint256 idx = _findAdapterIndex(adapterId_);
            if (!_adapterList[idx].active) revert AdapterInactive();
        }

        IProtocolAdapter.QuoteResult memory q = IProtocolAdapter(adapter).getQuote(
            tokenIn,
            tokenOut,
            amountIn
        );
        if (!q.available || q.amountOut == 0) revert QuoteUnavailable();
        if (q.amountOut < amountOutMin) revert SlippageTooHigh();

        amountOut = _executeAdapterSwap(
            adapter,
            tokenIn,
            tokenOut,
            amountIn,
            amountOutMin,
            msg.sender,
            msg.sender,
            deadline,
            q.adapterData
        );

        if (oracleSanityEnabled) {
            _oracleSanityCheck(tokenIn, tokenOut, amountIn, amountOut);
        }

        emit BestRouteSwap(msg.sender, tokenIn, tokenOut, amountIn, amountOut, adapterId_);
    }

    /// @inheritdoc IMetaAGRouter
    function swapMultiHop(
        HopParams[] calldata hops,
        uint256 amountIn,
        uint256 amountOutMin,
        uint256 deadline
    )
        external
        override
        nonReentrant
        whenNotPaused
        ensure(deadline)
        returns (uint256 amountOut)
    {
        uint256 hopCount = hops.length;
        if (hopCount < 2) revert TooFewHops();
        if (hopCount > MAX_HOPS) revert MaxHopsExceeded();
        if (amountIn == 0) revert ZeroAmount();

        for (uint256 i; i < hopCount; ++i) {
            HopParams calldata h = hops[i];
            if (h.tokenIn == address(0) || h.tokenOut == address(0)) revert ZeroAddress();
            if (h.tokenIn == h.tokenOut || (i > 0 && hops[i - 1].tokenOut != h.tokenIn)) revert InvalidPath();
            uint256 idx = _findAdapterIndex(h.adapterId);
            if (!_adapterList[idx].active) revert AdapterInactive();
            if (!IProtocolAdapter(_adapterList[idx].adapter).supportsSwap(h.tokenIn, h.tokenOut)) revert InvalidPath();
        }
        uint256 beforeInput = IERC20Minimal(hops[0].tokenIn).balanceOf(address(this));
        TransferHelper.safeTransferFrom(
            hops[0].tokenIn,
            msg.sender,
            address(this),
            amountIn
        );

        if (IERC20Minimal(hops[0].tokenIn).balanceOf(address(this)) - beforeInput != amountIn) revert TransferAmountMismatch();
        uint256 currentAmount = amountIn;

        for (uint256 i = 0; i < hopCount; ++i) {
            HopParams calldata hop = hops[i];
            address adapter = _adapterById[hop.adapterId];
            if (adapter == address(0)) revert AdapterNotFound();

            {
                uint256 idx = _findAdapterIndex(hop.adapterId);
                if (!_adapterList[idx].active) revert AdapterInactive();
            }

            bool isLast = i == hopCount - 1;
            address recipient = isLast ? msg.sender : address(this);
            uint256 hopMinOut = isLast && amountOutMin > hop.minAmountOut ? amountOutMin : hop.minAmountOut;

            IProtocolAdapter.QuoteResult memory q = IProtocolAdapter(adapter).getQuote(
                hop.tokenIn,
                hop.tokenOut,
                currentAmount
            );
            if (!q.available || q.amountOut == 0) revert QuoteUnavailable();
            if (q.amountOut < hopMinOut) revert SlippageTooHigh();

            TransferHelper.safeApprove(hop.tokenIn, adapter, 0);
            TransferHelper.safeApprove(hop.tokenIn, adapter, currentAmount);

            uint256 beforeOutput = IERC20Minimal(hop.tokenOut).balanceOf(recipient);
            uint256 beforeSpent = IERC20Minimal(hop.tokenIn).balanceOf(address(this));
            IProtocolAdapter(adapter).executeSwap(
                hop.tokenIn,
                hop.tokenOut,
                currentAmount,
                hopMinOut,
                address(this),
                recipient,
                deadline,
                q.adapterData
            );

            TransferHelper.safeApprove(hop.tokenIn, adapter, 0);

            if (beforeSpent - IERC20Minimal(hop.tokenIn).balanceOf(address(this)) != currentAmount) revert TransferAmountMismatch();
            currentAmount = IERC20Minimal(hop.tokenOut).balanceOf(recipient) - beforeOutput;
            if (currentAmount < hopMinOut) revert SlippageTooHigh();
        }

        amountOut = currentAmount;

        if (oracleSanityEnabled) {
            _oracleSanityCheck(
                hops[0].tokenIn,
                hops[hopCount - 1].tokenOut,
                amountIn,
                amountOut
            );
        }

        emit MultiHopSwap(
            msg.sender,
            hops[0].tokenIn,
            hops[hopCount - 1].tokenOut,
            amountIn,
            amountOut,
            hopCount
        );
    }

    /// @inheritdoc IMetaAGRouter
    function getAdapters() external view override returns (AdapterEntry[] memory) {
        return _adapterList;
    }

    /// @inheritdoc IMetaAGRouter
    function getAdapter(
        bytes32 adapterId_
    ) external view override returns (AdapterEntry memory) {
        if (_adapterById[adapterId_] == address(0)) revert AdapterNotFound();
        uint256 idx = _findAdapterIndex(adapterId_);
        return _adapterList[idx];
    }

    /// @inheritdoc IMetaAGRouter
    function adapterCount() external view override returns (uint256) {
        return _adapterList.length;
    }

    /// @inheritdoc IMetaAGRouter
    function isAdapterActive(bytes32 adapterId_) external view override returns (bool) {
        uint256 len = _adapterList.length;
        for (uint256 i = 0; i < len; ++i) {
            if (_adapterList[i].adapterId == adapterId_) return _adapterList[i].active;
        }
        return false;
    }

    /// @notice Runs the S9 approval dance around a single `executeSwap` leg.
    /// @dev Transfers `amountIn` of `tokenIn` from `payer` into this router,
    ///      approves the adapter with zero-first reset, executes, then resets
    ///      approval back to zero. Slippage is enforced against `amountOutMin`.
    function _executeAdapterSwap(
        address adapter,
        address tokenIn,
        address tokenOut,
        uint256 amountIn,
        uint256 amountOutMin,
        address payer,
        address recipient,
        uint256 deadline,
        bytes memory adapterData
    ) internal returns (uint256 amountOut) {
        if (tokenIn == address(0) || tokenOut == address(0)) revert ZeroAddress();
        uint256 beforeInput = IERC20Minimal(tokenIn).balanceOf(address(this));
        TransferHelper.safeTransferFrom(tokenIn, payer, address(this), amountIn);
        if (IERC20Minimal(tokenIn).balanceOf(address(this)) - beforeInput != amountIn) revert TransferAmountMismatch();
        uint256 beforeOutput = IERC20Minimal(tokenOut).balanceOf(recipient);

        TransferHelper.safeApprove(tokenIn, adapter, 0);
        TransferHelper.safeApprove(tokenIn, adapter, amountIn);

        IProtocolAdapter(adapter).executeSwap(
            tokenIn,
            tokenOut,
            amountIn,
            amountOutMin,
            address(this),
            recipient,
            deadline,
            adapterData
        );

        TransferHelper.safeApprove(tokenIn, adapter, 0);

        if (IERC20Minimal(tokenIn).balanceOf(address(this)) != beforeInput) revert TransferAmountMismatch();
        amountOut = IERC20Minimal(tokenOut).balanceOf(recipient) - beforeOutput;
        if (amountOut < amountOutMin) revert SlippageTooHigh();
    }

    /// @notice Reverts if the realized `amountOut` deviates from oracle-implied
    ///         expectation by more than `maxOracleSanityDeviation` bps.
    /// @dev Port of `dev/PECORRouter._oracleSanityCheck` with the dev's intent
    ///      preserved with decimal-normalized oracle value comparison.
    ///      Skips silently when either oracle price is unavailable.
    function _oracleSanityCheck(
        address tokenIn,
        address tokenOut,
        uint256 amountIn,
        uint256 amountOut
    ) internal view {
        IOracleHub hub = IOracleHub(oracleHub);
        (bool outAvailable, ) = hub.isPriceAvailable(tokenOut);
        if (!outAvailable) return;
        (bool inAvailable, ) = hub.isPriceAvailable(tokenIn);
        if (!inAvailable) return;

        uint256 priceIn = hub.getPrice(tokenIn);
        uint256 priceOut = hub.getPrice(tokenOut);
        if (priceIn == 0 || priceOut == 0) return;

        uint8 dIn = IERC20Minimal(tokenIn).decimals();
        uint8 dOut = IERC20Minimal(tokenOut).decimals();
        if (dIn > 36 || dOut > 36) revert OracleSanityFailed();
        uint256 value = SidioraMath.mulDiv(amountIn, priceIn, 10 ** dIn);
        uint256 expectedOut = SidioraMath.mulDiv(value, 10 ** dOut, priceOut);
        if (expectedOut == 0) return;

        uint256 deviation = amountOut > expectedOut
            ? SidioraMath.mulDiv(amountOut - expectedOut, BPS_DENOMINATOR, expectedOut)
            : SidioraMath.mulDiv(expectedOut - amountOut, BPS_DENOMINATOR, expectedOut);

        if (deviation > maxOracleSanityDeviation) revert OracleSanityFailed();
    }

    function _safeQuote(address adapter, address tokenIn, address tokenOut, uint256 amountIn)
        internal view returns (IProtocolAdapter.QuoteResult memory result)
    {
        try IProtocolAdapter(adapter).getQuote{gas: 500_000}(tokenIn, tokenOut, amountIn)
            returns (IProtocolAdapter.QuoteResult memory q) { return q; } catch { return result; }
    }

    /// @dev Finds the index of an adapter by ID. Reverts `AdapterNotFound` if
    ///      no entry matches — cheaper than a separate existence map because
    ///      the list is capped at `MAX_ADAPTERS = 20`.
    function _findAdapterIndex(bytes32 adapterId_) internal view returns (uint256) {
        uint256 len = _adapterList.length;
        for (uint256 i = 0; i < len; ++i) {
            if (_adapterList[i].adapterId == adapterId_) return i;
        }
        revert AdapterNotFound();
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
