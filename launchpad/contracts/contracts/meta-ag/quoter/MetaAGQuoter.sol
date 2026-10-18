// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.27;

import {IPECOR} from "../interfaces/IPECOR.sol";
import {IMetaAGQuoter} from "../interfaces/IMetaAGQuoter.sol";
import {IPECORVault} from "../interfaces/IPECORVault.sol";
import {IPriceOracle} from "../interfaces/IPriceOracle.sol";
import {Initializable} from "../../base/Initializable.sol";
import {UUPSUpgradeable} from "../../base/UUPSUpgradeable.sol";
import {AccessControl} from "../../base/AccessControl.sol";
import {SidioraMath} from "../../libraries/SidioraMath.sol";

/// @title MetaAGQuoter — Read-only vault-side quote layer for frontends
/// @notice Rich quote view over the oracle-priced PECORVault swap path. All
///         functions are pure views — never mutates state. Frontends can build
///         a full swap UI from a single `batchQuote` round-trip (amounts, fee
///         breakdown, liquidity, price freshness).
/// @dev Spec reference: `docs/architecture/pecor-sidiora-aggregator-spec.md` §7.11
///      (FROZEN 2026-04-24). Interface:
///      `contracts/meta-ag/interfaces/IMetaAGQuoter.sol`. Port of
///      `dev/PECORQuoter.sol` with the following frozen-surface divergences:
///        - Constructor-based replaced with UUPS `initialize(priceOracle, vault,
///          weth, pecor, admin)` (spec §7.11).
///        - Ownable admin implicitly via AccessControl.DEFAULT_ADMIN_ROLE on
///          Timelock (S1). No public admin surface; evolution goes through UUPS.
///        - OpenZeppelin Math.mulDiv replaced with `SidioraMath.mulDiv`.
///
/// Scope note (spec §7.11):
///   MetaAGQuoter quotes ONLY vault-side (oracle-priced) swaps. Cross-adapter
///   aggregation (Sidiora AMM + future adapters) lives on
///   `MetaAGRouter.getBestQuote() / getAllQuotes()`.
///
/// Inheritance (spec §7.11):
///   IMetaAGQuoter, Initializable, UUPSUpgradeable, AccessControl
///
/// Roles:
///   - DEFAULT_ADMIN_ROLE → Timelock at deploy (invariant S1)
///
/// Storage layout (append-only per S12):
///   slot 0:  AccessControl._roles   (mapping)
///   slot 1:  priceOracle            (IPriceOracle)
///   slot 2:  vault                  (IPECORVault)
///   slot 3:  weth                   (address; semantically immutable after init)
///   slot 4:  pecor                  (address; swapFeeBps read via staticcall)
///   slot 5..54: __gap[50]
contract MetaAGQuoter is
    IMetaAGQuoter,
    Initializable,
    UUPSUpgradeable,
    AccessControl
{

    uint256 public constant BPS_DENOMINATOR = 10_000;

    error ZeroAddress();

    IPriceOracle public priceOracle;
    IPECORVault public vault;

    /// @notice Canonical wrapped-native token. Semantically immutable — assigned
    ///         exactly once in {initialize} and never mutated afterwards.
    address public weth;

    /// @notice PECOR engine address. `swapFeeBps()` is read via staticcall so
    ///         the quoter doesn't depend on IPECOR's impl evolving.
    address public pecor;

    /// @dev Reserved storage for future upgrades (S12: append-only, 50 * 32 bytes).
    uint256[50] private __gap;

    /// @custom:oz-upgrades-unsafe-allow constructor
    constructor() {
        _disableInitializers();
    }

    /// @inheritdoc IMetaAGQuoter
    function initialize(
        address priceOracle_,
        address vault_,
        address weth_,
        address pecor_,
        address admin
    ) external override initializer {
        if (priceOracle_ == address(0)) revert ZeroAddress();
        if (vault_ == address(0)) revert ZeroAddress();
        if (weth_ == address(0)) revert ZeroAddress();
        if (admin == address(0)) revert ZeroAddress();

        priceOracle = IPriceOracle(priceOracle_);
        vault = IPECORVault(vault_);
        weth = weth_;
        pecor = pecor_;
        _grantRole(DEFAULT_ADMIN_ROLE, admin);
    }

    function _authorizeUpgrade(address) internal override onlyRole(DEFAULT_ADMIN_ROLE) {}

    /// @inheritdoc IMetaAGQuoter
    function quoteExactIn(
        address tokenIn,
        address tokenOut,
        uint256 amountIn
    ) external view override returns (QuoteResult memory) {
        return _buildQuote(tokenIn, tokenOut, amountIn, true);
    }

    /// @inheritdoc IMetaAGQuoter
    function quoteExactOut(
        address tokenIn,
        address tokenOut,
        uint256 amountOut
    ) external view override returns (QuoteResult memory) {
        return _buildQuote(tokenIn, tokenOut, amountOut, false);
    }

    /// @inheritdoc IMetaAGQuoter
    function quoteExactInNative(
        address tokenOut,
        uint256 nativeAmountIn
    ) external view override returns (QuoteResult memory) {
        return _buildQuote(weth, tokenOut, nativeAmountIn, true);
    }

    /// @inheritdoc IMetaAGQuoter
    function quoteExactInToNative(
        address tokenIn,
        uint256 amountIn
    ) external view override returns (QuoteResult memory) {
        return _buildQuote(tokenIn, weth, amountIn, true);
    }

    /// @inheritdoc IMetaAGQuoter
    function quoteMarketBuy(
        address stablecoin,
        address token,
        uint256 stablecoinAmount
    ) external view override returns (QuoteResult memory) {
        return _buildQuote(stablecoin, token, stablecoinAmount, true);
    }

    /// @inheritdoc IMetaAGQuoter
    function quoteMarketSell(
        address token,
        address stablecoin,
        uint256 tokenAmount
    ) external view override returns (QuoteResult memory) {
        return _buildQuote(token, stablecoin, tokenAmount, true);
    }

    /// @inheritdoc IMetaAGQuoter
    function batchQuote(
        QuoteRequest[] calldata requests
    ) external view override returns (QuoteResult[] memory results) {
        uint256 len = requests.length;
        results = new QuoteResult[](len);
        address weth_ = weth;
        for (uint256 i = 0; i < len; ++i) {
            address tokenIn = requests[i].tokenIn;
            address tokenOut = requests[i].tokenOut;
            if (tokenIn == address(0)) tokenIn = weth_;
            if (tokenOut == address(0)) tokenOut = weth_;
            results[i] = _buildQuote(
                tokenIn,
                tokenOut,
                requests[i].amount,
                requests[i].isExactIn
            );
        }
    }

    /// @inheritdoc IMetaAGQuoter
    function getLiquidityInfo(
        address token
    )
        external
        view
        override
        returns (uint256 available, uint256 tokenPrice, bool isStale)
    {
        available = vault.getReserves(token);
        try priceOracle.getPrice(token) returns (uint256 price) {
            tokenPrice = price;
            isStale = false;
        } catch {
            tokenPrice = 0;
            isStale = true;
        }
    }

    /// @inheritdoc IMetaAGQuoter
    function getAllLiquidityInfo()
        external
        view
        override
        returns (
            address[] memory tokens,
            uint256[] memory reserves,
            uint256[] memory prices,
            bool[] memory stale
        )
    {
        tokens = vault.getRegisteredTokens();
        uint256 len = tokens.length;
        reserves = new uint256[](len);
        prices = new uint256[](len);
        stale = new bool[](len);

        for (uint256 i = 0; i < len; ++i) {
            reserves[i] = vault.getReserves(tokens[i]);
            try priceOracle.getPrice(tokens[i]) returns (uint256 price) {
                prices[i] = price;
                stale[i] = false;
            } catch {
                prices[i] = 0;
                stale[i] = true;
            }
        }
    }

    /// @inheritdoc IMetaAGQuoter
    function getTokenPrice(
        address token
    ) external view override returns (uint256 price, uint256 timestamp, bool isStale) {
        try priceOracle.getPriceWithTimestamp(token) returns (
            uint256 p,
            uint256 t,
            uint256
        ) {
            return (p, t, false);
        } catch {
            return (0, 0, true);
        }
    }

    /// @inheritdoc IMetaAGQuoter
    function getTokenPrices(
        address[] calldata tokens
    )
        external
        view
        override
        returns (uint256[] memory prices, uint256[] memory timestamps, bool[] memory stale)
    {
        (prices, timestamps, stale) = priceOracle.getPrices(tokens);
    }

    /// @inheritdoc IMetaAGQuoter
    function getTWAP(
        address token,
        uint256 period
    ) external view override returns (uint256) {
        return priceOracle.getTWAP(token, period);
    }

    /// @notice Builds a full QuoteResult for a vault-side swap.
    /// @dev Mirrors `PECOR._calculateSwapOutput/_calculateSwapInput` so the
    ///      quoter returns the same amounts the engine would execute. Never
    ///      reverts — unavailable prices return a partial result with
    ///      `sufficientLiquidity=false` and the `priceStale*` flags set.
    function _buildQuote(
        address tokenIn,
        address tokenOut,
        uint256 amount,
        bool isExactIn
    ) internal view returns (QuoteResult memory result) {
        try this.quoteChecked(tokenIn, tokenOut, amount, isExactIn) returns (QuoteResult memory q) { return q; }
        catch { result.priceStaleIn = true; result.priceStaleOut = true; return result; }
    }

    /// @dev External view boundary isolates vault, oracle and arithmetic failures per request.
    function quoteChecked(address tokenIn, address tokenOut, uint256 amount, bool isExactIn)
        external view returns (QuoteResult memory result)
    {
        (uint256 priceIn, uint256 tsIn, bool staleIn) = _safeGetPrice(tokenIn);
        (uint256 priceOut, uint256 tsOut, bool staleOut) = _safeGetPrice(tokenOut);

        result.spotPriceIn = priceIn;
        result.spotPriceOut = priceOut;
        result.priceTimestampIn = tsIn;
        result.priceTimestampOut = tsOut;
        result.priceStaleIn = staleIn;
        result.priceStaleOut = staleOut;
        if (priceIn == 0 || priceOut == 0) return result;
        uint256 decimalsIn = uint256(vault.getTokenDecimals(tokenIn));
        uint256 decimalsOut = uint256(vault.getTokenDecimals(tokenOut));
        if (decimalsIn > 36 || decimalsOut > 36) return result;

        if (isExactIn) {
            result.amountIn = amount;
            if (pecor != address(0)) {
                (result.grossAmountOut, result.amountOut, , result.feeBps, result.feeAmount) =
                    IPECOR(pecor).getDetailedQuote(tokenIn, tokenOut, amount);
            } else {
                // Bootstrap mode is explicitly independent, fee-free oracle pricing.
                result.amountOut = _calculateOutput(amount, priceIn, priceOut, decimalsIn, decimalsOut);
                result.grossAmountOut = result.amountOut;
            }
        } else {
            result.amountOut = amount;
            result.grossAmountOut = amount;
            uint256 baseInput = _calculateInput(amount, priceIn, priceOut, decimalsIn, decimalsOut);
            if (pecor != address(0)) {
                result.amountIn = IPECOR(pecor).getQuoteExactOut(tokenIn, tokenOut, amount);
                result.feeAmount = result.amountIn > baseInput ? result.amountIn - baseInput : 0;
                result.feeBps = baseInput == 0 ? 0 : SidioraMath.mulDiv(result.feeAmount, BPS_DENOMINATOR, baseInput);
            } else {
                result.amountIn = baseInput;
            }
        }
        result.executionPrice = result.amountIn == 0 ? 0 : SidioraMath.mulDiv(result.amountOut, 10 ** decimalsIn, result.amountIn);

        uint256 outputAmount = isExactIn ? result.amountOut : amount;
        result.availableLiquidity = vault.getAvailableReserves(tokenOut);
        result.sufficientLiquidity = result.availableLiquidity >= outputAmount;
    }

    /// @dev Non-reverting price fetch. Returns (0, 0, true) if the oracle
    ///      call reverts (missing token, stale past threshold, paused...).
    function _safeGetPrice(
        address token
    ) internal view returns (uint256 price, uint256 timestamp, bool stale) {
        try priceOracle.getPriceWithTimestamp(token) returns (
            uint256 p,
            uint256 t,
            uint256
        ) {
            return (p, t, false);
        } catch {
            return (0, 0, true);
        }
    }

    /// @dev Mirrors PECOR's oracle-priced output calculation (decimals-aware).
    function _calculateOutput(
        uint256 amountIn,
        uint256 priceIn,
        uint256 priceOut,
        uint256 decimalsIn,
        uint256 decimalsOut
    ) internal pure returns (uint256) {
        if (priceOut == 0) return 0;
        if (decimalsOut >= decimalsIn) return SidioraMath.mulDiv(amountIn, priceIn * 10 ** (decimalsOut - decimalsIn), priceOut);
        return SidioraMath.mulDiv(amountIn, priceIn, priceOut * 10 ** (decimalsIn - decimalsOut));
    }

    /// @dev Inverse of `_calculateOutput` — how much input is required to
    ///      realize `amountOut` of tokenOut at the given oracle prices.
    function _calculateInput(
        uint256 amountOut,
        uint256 priceIn,
        uint256 priceOut,
        uint256 decimalsIn,
        uint256 decimalsOut
    ) internal pure returns (uint256) {
        if (priceIn == 0) return 0;
        uint256 numeratorPrice = priceOut;
        uint256 denominator = priceIn;
        if (decimalsIn >= decimalsOut) numeratorPrice *= 10 ** (decimalsIn - decimalsOut);
        else denominator *= 10 ** (decimalsOut - decimalsIn);
        uint256 quotient = SidioraMath.mulDiv(amountOut, numeratorPrice, denominator);
        return quotient + (mulmod(amountOut, numeratorPrice, denominator) == 0 ? 0 : 1);
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
