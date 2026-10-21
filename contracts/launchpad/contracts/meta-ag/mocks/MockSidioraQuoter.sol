// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.27;

/// @title MockSidioraQuoter
/// @notice Spy/stub for the live Sidiora IQuoter surface consumed by SidioraAdapter.
/// @dev Only the two methods SidioraAdapter calls are implemented:
///       - quoteExactInput(pool, amountIn, isBuy) returns (QuoteResult)
///       - quoteMultihop(tokenIn, tokenOut, amountIn) returns (MultihopQuoteResult)
///
///      Used to exercise SidioraAdapter.getQuote() never-revert guarantees (I1) and
///      correctness of BUY / SELL / MULTIHOP quote paths.
contract MockSidioraQuoter {
    // ============ LIVE IQuoter STRUCT MIRRORS ============

    struct QuoteResult {
        uint256 amountOut;
        uint256 feeAmount;
        uint256 priceImpactBps;
    }

    struct MultihopQuoteResult {
        uint256 amountOut;
        uint256 intermediateUsdl;
        uint256 sellFeeAmount;
        uint256 buyFeeAmount;
        uint256 sellPriceImpactBps;
        uint256 buyPriceImpactBps;
        uint256 combinedPriceImpactBps;
        address poolA;
        address poolB;
    }

    // ============ CONFIGURABLE RETURNS ============

    QuoteResult private _buyQuote;
    QuoteResult private _sellQuote;
    MultihopQuoteResult private _multihopQuote;

    // ============ BEHAVIOR SWITCHES ============

    bool public revertOnBuyQuote;
    bool public revertOnSellQuote;
    bool public revertOnMultihopQuote;

    // ============ CONFIG ============

    function setBuyQuote(
        uint256 amountOut_,
        uint256 feeAmount_,
        uint256 priceImpactBps_
    ) external {
        _buyQuote = QuoteResult(amountOut_, feeAmount_, priceImpactBps_);
    }

    function setSellQuote(
        uint256 amountOut_,
        uint256 feeAmount_,
        uint256 priceImpactBps_
    ) external {
        _sellQuote = QuoteResult(amountOut_, feeAmount_, priceImpactBps_);
    }

    function setMultihopQuote(
        uint256 amountOut_,
        uint256 intermediateUsdl_,
        uint256 sellFee_,
        uint256 buyFee_,
        uint256 combinedImpactBps_,
        address poolA_,
        address poolB_
    ) external {
        _multihopQuote = MultihopQuoteResult({
            amountOut: amountOut_,
            intermediateUsdl: intermediateUsdl_,
            sellFeeAmount: sellFee_,
            buyFeeAmount: buyFee_,
            sellPriceImpactBps: 0,
            buyPriceImpactBps: 0,
            combinedPriceImpactBps: combinedImpactBps_,
            poolA: poolA_,
            poolB: poolB_
        });
    }

    function setRevertOnBuyQuote(bool v) external {
        revertOnBuyQuote = v;
    }

    function setRevertOnSellQuote(bool v) external {
        revertOnSellQuote = v;
    }

    function setRevertOnMultihopQuote(bool v) external {
        revertOnMultihopQuote = v;
    }

    // ============ IQuoter.quoteExactInput ============

    /// @dev isBuy=true returns the buy-side quote, false returns sell-side.
    function quoteExactInput(
        address /* pool */,
        uint256 /* amountIn */,
        bool isBuy
    ) external view returns (QuoteResult memory result) {
        if (isBuy) {
            if (revertOnBuyQuote) revert("MockSidioraQuoter: buy quote forced revert");
            return _buyQuote;
        } else {
            if (revertOnSellQuote) revert("MockSidioraQuoter: sell quote forced revert");
            return _sellQuote;
        }
    }

    // ============ IQuoter.quoteMultihop ============

    function quoteMultihop(
        address /* tokenIn */,
        address /* tokenOut */,
        uint256 /* amountIn */
    ) external view returns (MultihopQuoteResult memory result) {
        if (revertOnMultihopQuote) revert("MockSidioraQuoter: multihop quote forced revert");
        return _multihopQuote;
    }
}
