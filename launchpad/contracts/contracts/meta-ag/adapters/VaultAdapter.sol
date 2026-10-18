// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.27;

import {IERC20Minimal} from "../interfaces/IERC20Minimal.sol";
import {ReentrancyGuard} from "../../base/ReentrancyGuard.sol";
import {IProtocolAdapter} from "../interfaces/IProtocolAdapter.sol";
import {IPECORVault} from "../interfaces/IPECORVault.sol";
import {IPriceOracle} from "../interfaces/IPriceOracle.sol";
import {AccessControl} from "../../base/AccessControl.sol";
import {SidioraMath} from "../../libraries/SidioraMath.sol";
import {TransferHelper} from "../../libraries/TransferHelper.sol";

/// @title VaultAdapter — IProtocolAdapter wrapper for PECORVault oracle swaps
/// @notice Plug that exposes PECORVault inventory to MetaAGRouter through the
///         pluggable adapter contract. Priced via the canonical PriceOracle.
/// @dev Spec §7.9. Port of `dev/adapters/VaultAdapter.sol` with:
///       - Ownable → AccessControl (Timelock-admin)
///       - @ openzeppelin/Math → libraries/SidioraMath
///       - @ openzeppelin/SafeERC20 unused (vault already owns transfer mechanics)
///
/// Operator requirement: the vault MUST grant OPERATOR_ROLE to this adapter
/// before it can route swaps (vault.setOperator(adapter, true)).
///
/// Caller (typically MetaAGRouter, optionally a direct user) must approve THIS
/// adapter for `amountIn` of tokenIn before calling executeSwap. The adapter
/// pulls tokenIn into itself first, then funnels through `vault.deposit` —
/// this mirrors `SidioraAdapter`'s pattern and keeps the S9 approval dance
/// consistent across every adapter MetaAGRouter routes through. Vault
/// accounting is unchanged: `deposit` increments reserves + totalDeposited,
/// then `pushTokens` decrements reserves and credits totalWithdrawn for the
/// outbound legs (recipient + optional feeCollector).
contract VaultAdapter is IProtocolAdapter, AccessControl, ReentrancyGuard {

    bytes32 private constant _ADAPTER_ID = keccak256("PECORVault.v1");

    uint256 public constant MAX_FEE_BPS = 200;
    uint256 public constant BPS_DENOMINATOR = 10_000;

    error UnauthorizedPayer();
    error TransferAmountMismatch();
    event ConfigurationUpdated(bytes32 indexed field, address oldValue, address newValue);
    event FeeUpdated(uint256 oldValue, uint256 newValue);
    error ZeroAddress();
    error ZeroAmount();
    error SameToken();
    error Expired();
    error FeeTooHigh();
    error SlippageExceeded();
    error InsufficientLiquidity();

    IPECORVault public vault;
    IPriceOracle public priceOracle;
    uint256 public feeBps;
    address public feeCollector;

    constructor(
        address vault_,
        address priceOracle_,
        uint256 feeBps_,
        address feeCollector_,
        address admin_
    ) {
        if (vault_ == address(0)) revert ZeroAddress();
        if (priceOracle_ == address(0)) revert ZeroAddress();
        if (feeCollector_ == address(0)) revert ZeroAddress();
        if (admin_ == address(0)) revert ZeroAddress();
        if (feeBps_ > MAX_FEE_BPS) revert FeeTooHigh();

        _initReentrancyGuard();
        vault = IPECORVault(vault_);
        priceOracle = IPriceOracle(priceOracle_);
        feeBps = feeBps_;
        feeCollector = feeCollector_;

        _grantRole(DEFAULT_ADMIN_ROLE, admin_);
    }

    function setFee(uint256 feeBps_) external onlyRole(DEFAULT_ADMIN_ROLE) {
        if (feeBps_ > MAX_FEE_BPS) revert FeeTooHigh();
        emit FeeUpdated(feeBps, feeBps_);
        feeBps = feeBps_;
    }

    function setFeeCollector(address collector) external onlyRole(DEFAULT_ADMIN_ROLE) {
        if (collector == address(0)) revert ZeroAddress();
        emit ConfigurationUpdated("feeCollector", feeCollector, collector);
        feeCollector = collector;
    }

    function setPriceOracle(address oracle) external onlyRole(DEFAULT_ADMIN_ROLE) {
        if (oracle == address(0)) revert ZeroAddress();
        emit ConfigurationUpdated("priceOracle", address(priceOracle), oracle);
        priceOracle = IPriceOracle(oracle);
    }

    function adapterId() external pure override returns (bytes32) {
        return _ADAPTER_ID;
    }

    function adapterName() external pure override returns (string memory) {
        return "PECORVault.v1";
    }

    function adapterVersion() external pure override returns (string memory) {
        return "1.0.0";
    }

    /// @inheritdoc IProtocolAdapter
    function supportsSwap(address tokenIn, address tokenOut) external view override returns (bool) {
        if (tokenIn == tokenOut) return false;
        (bool inReg, , , , , ) = vault.getTokenInfo(tokenIn);
        (bool outReg, , , , , ) = vault.getTokenInfo(tokenOut);
        return inReg && outReg;
    }

    /// @inheritdoc IProtocolAdapter
    function getSupportedPairs()
        external
        view
        override
        returns (address[] memory tokenIns, address[] memory tokenOuts)
    {
        address[] memory tokens = vault.getRegisteredTokens();
        uint256 n = tokens.length;
        if (n < 2) return (new address[](0), new address[](0));

        uint256 pairCount = n * (n - 1);
        tokenIns = new address[](pairCount);
        tokenOuts = new address[](pairCount);
        uint256 idx;
        for (uint256 i = 0; i < n; ++i) {
            for (uint256 j = 0; j < n; ++j) {
                if (i == j) continue;
                tokenIns[idx] = tokens[i];
                tokenOuts[idx] = tokens[j];
                ++idx;
            }
        }
    }

    /// @inheritdoc IProtocolAdapter
    function getMaxInput(
        address tokenIn,
        address tokenOut
    ) external view override returns (uint256 maxIn) {
        if (tokenIn == tokenOut) return 0;
        uint256 reservesOut = vault.getAvailableReserves(tokenOut);
        if (reservesOut == 0) return 0;

        uint256 priceIn = _safeGetPrice(tokenIn);
        uint256 priceOut = _safeGetPrice(tokenOut);
        if (priceIn == 0 || priceOut == 0) return 0;

        uint8 dIn = vault.getTokenDecimals(tokenIn);
        uint8 dOut = vault.getTokenDecimals(tokenOut);
        return _calcAmountIn(reservesOut, priceIn, priceOut, dIn, dOut);
    }

    /// @inheritdoc IProtocolAdapter
    /// @dev Invariant I1 — never reverts. Returns `available=false` on any issue.
    function getQuote(
        address tokenIn,
        address tokenOut,
        uint256 amountIn
    ) external view override returns (QuoteResult memory result) {
        try this.quoteChecked(tokenIn, tokenOut, amountIn) returns (QuoteResult memory q) { return q; } catch { return result; }
    }

    function quoteChecked(address tokenIn, address tokenOut, uint256 amountIn) external view returns (QuoteResult memory result) {
        if (tokenIn == tokenOut || amountIn == 0) return result;

        (bool inReg, , , , , ) = vault.getTokenInfo(tokenIn);
        (bool outReg, , , uint256 reservesOut, , ) = vault.getTokenInfo(tokenOut);
        if (!inReg || !outReg) return result;
        reservesOut = vault.getAvailableReserves(tokenOut);

        uint256 priceIn = _safeGetPrice(tokenIn);
        uint256 priceOut = _safeGetPrice(tokenOut);
        if (priceIn == 0 || priceOut == 0) return result;

        uint8 dIn = vault.getTokenDecimals(tokenIn);
        uint8 dOut = vault.getTokenDecimals(tokenOut);

        uint256 grossOut = _calcAmountOut(amountIn, priceIn, priceOut, dIn, dOut);
        if (grossOut == 0 || grossOut > reservesOut) return result;

        uint256 feeAmount = SidioraMath.mulDiv(grossOut, feeBps, BPS_DENOMINATOR);
        uint256 netOut = grossOut - feeAmount;

        uint256 swapValueUSD = SidioraMath.mulDiv(amountIn, priceIn, 10 ** dIn);
        uint256 reserveValueUSD = SidioraMath.mulDiv(reservesOut, priceOut, 10 ** dOut);
        uint256 priceImpact = reserveValueUSD > 0
            ? SidioraMath.mulDiv(swapValueUSD, BPS_DENOMINATOR, reserveValueUSD)
            : BPS_DENOMINATOR;

        result = QuoteResult({
            amountOut: netOut,
            priceImpactBps: priceImpact,
            feeBps: feeBps,
            feeAmount: feeAmount,
            available: true,
            adapterData: abi.encode(tokenIn, tokenOut)
        });
    }

    /// @inheritdoc IProtocolAdapter
    /// @dev Caller (typically MetaAGRouter) must ensure `from` has approved
    ///      THIS adapter for at least `amountIn` of tokenIn.
    function executeSwap(
        address tokenIn,
        address tokenOut,
        uint256 amountIn,
        uint256 minAmountOut,
        address from,
        address recipient,
        uint256 deadline,
        bytes calldata
    ) external override nonReentrant returns (SwapResult memory result) {
        if (from != msg.sender) revert UnauthorizedPayer();
        if (recipient == address(0)) revert ZeroAddress();
        if (deadline != 0 && block.timestamp > deadline) revert Expired();
        if (tokenIn == tokenOut) revert SameToken();
        if (amountIn == 0) revert ZeroAmount();

        uint256 priceIn = priceOracle.getPrice(tokenIn);
        uint256 priceOut = priceOracle.getPrice(tokenOut);
        uint8 dIn = vault.getTokenDecimals(tokenIn);
        uint8 dOut = vault.getTokenDecimals(tokenOut);

        uint256 grossOut = _calcAmountOut(amountIn, priceIn, priceOut, dIn, dOut);
        uint256 feeAmount = SidioraMath.mulDiv(grossOut, feeBps, BPS_DENOMINATOR);
        uint256 netOut = grossOut - feeAmount;

        if (netOut < minAmountOut) revert SlippageExceeded();
        if (!vault.hasLiquidity(tokenOut, grossOut)) revert InsufficientLiquidity();

        uint256 beforeInput = IERC20Minimal(tokenIn).balanceOf(address(this));
        TransferHelper.safeTransferFrom(tokenIn, from, address(this), amountIn);
        if (IERC20Minimal(tokenIn).balanceOf(address(this)) - beforeInput != amountIn) revert TransferAmountMismatch();
        uint256 beforeVault = IERC20Minimal(tokenIn).balanceOf(address(vault));

        address vaultAddr = address(vault);
        TransferHelper.safeApprove(tokenIn, vaultAddr, 0);
        TransferHelper.safeApprove(tokenIn, vaultAddr, amountIn);
        vault.deposit(tokenIn, amountIn);
        if (IERC20Minimal(tokenIn).balanceOf(address(vault)) - beforeVault != amountIn) revert TransferAmountMismatch();
        TransferHelper.safeApprove(tokenIn, vaultAddr, 0);

        uint256 beforeOutput = IERC20Minimal(tokenOut).balanceOf(recipient);
        vault.pushTokens(tokenOut, recipient, netOut);
        netOut = IERC20Minimal(tokenOut).balanceOf(recipient) - beforeOutput;
        if (netOut < minAmountOut) revert SlippageExceeded();
        if (feeAmount > 0 && feeCollector != address(0)) {
            vault.pushTokens(tokenOut, feeCollector, feeAmount);
        }

        emit SwapExecuted(_ADAPTER_ID, tokenIn, tokenOut, amountIn, netOut, recipient);

        result = SwapResult({amountOut: netOut, feeAmount: feeAmount, adapterData: ""});
    }

    function _safeGetPrice(address token) internal view returns (uint256) {
        try priceOracle.getPrice(token) returns (uint256 p) {
            return p;
        } catch {
            return 0;
        }
    }

    function _calcAmountOut(
        uint256 amountIn,
        uint256 priceIn,
        uint256 priceOut,
        uint8 dIn,
        uint8 dOut
    ) internal pure returns (uint256) {
        if (dIn > 36 || dOut > 36 || priceOut == 0) revert SlippageExceeded();
        if (dOut >= dIn) return SidioraMath.mulDiv(amountIn, priceIn * 10 ** (dOut - dIn), priceOut);
        return SidioraMath.mulDiv(amountIn, priceIn, priceOut * 10 ** (dIn - dOut));
    }

    function _calcAmountIn(
        uint256 amountOut,
        uint256 priceIn,
        uint256 priceOut,
        uint8 dIn,
        uint8 dOut
    ) internal pure returns (uint256) {
        if (dIn > 36 || dOut > 36 || priceIn == 0) revert SlippageExceeded();
        uint256 numeratorPrice = priceOut;
        uint256 denominator = priceIn;
        if (dIn >= dOut) numeratorPrice *= 10 ** (dIn - dOut);
        else denominator *= 10 ** (dOut - dIn);
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
