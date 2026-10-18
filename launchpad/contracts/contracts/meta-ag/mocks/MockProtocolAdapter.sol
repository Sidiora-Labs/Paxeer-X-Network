// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.27;

import {IProtocolAdapter} from "../interfaces/IProtocolAdapter.sol";

/**
 * @title MockProtocolAdapter
 * @notice Minimal, deterministic IProtocolAdapter spy for unit-testing
 *         MetaAGRouter. Quotes and swaps are fully script-driven by the
 *         test setter API — no real liquidity, oracle, or price discovery
 *         logic. Output tokens are pulled from this mock's own balance
 *         (pre-fund the mock with MockStandardERC20.mint before swap tests).
 * @dev Scope-isolated under `contracts/meta-ag/mocks/`. Production code
 *      never touches this contract.
 *
 * Behaviour highlights:
 *   - getQuote returns the configured `QuoteResult` (or the zero default).
 *   - executeSwap pulls `amountIn` tokenIn from `from` and transfers
 *     `swapAmountOut` tokenOut to `recipient`. Optionally reverts.
 *   - Records call-history fields (lastFrom / lastRecipient / etc.) so tests
 *     can assert the router passed the right args through.
 *   - `setRevertOnQuote(true)` makes getQuote revert — used to verify the
 *     router treats failures as available=false (safety guard).
 *   - `requireExpectedAdapterData(bytes)` lets tests prove that adapterData
 *     round-trips from getQuote → executeSwap unchanged.
 */
interface IMockMintableERC20 {
    function transfer(address to, uint256 amount) external returns (bool);
    function transferFrom(address from, address to, uint256 amount) external returns (bool);
    function balanceOf(address who) external view returns (uint256);
}

contract MockProtocolAdapter is IProtocolAdapter {
    error MockForcedRevert();
    error MockUnexpectedAdapterData();

    // ---- Identity (immutable per spec §7.4) ----
    bytes32 private immutable _adapterId;
    string private _adapterName;
    string private _adapterVersion;

    // ---- Configurable quote ----
    bool public quoteAvailable = true;
    uint256 public quoteAmountOut;
    uint256 public quotePriceImpactBps;
    uint256 public quoteFeeBps;
    uint256 public quoteFeeAmount;
    bytes public quoteAdapterData;
    bool public revertOnQuote;

    // ---- Configurable swap ----
    uint256 public swapAmountOut;
    uint256 public swapFeeAmount;
    bytes public swapResultAdapterData;
    bool public revertOnSwap;

    // ---- Optional adapterData round-trip witness (if set, executeSwap
    //      reverts with MockUnexpectedAdapterData when calldata mismatches)
    bytes public expectedAdapterData;
    bool public expectedAdapterDataSet;

    // ---- Call-history capture ----
    struct LastSwap {
        address tokenIn;
        address tokenOut;
        uint256 amountIn;
        uint256 amountOutMin;
        address from;
        address recipient;
        uint256 deadline;
        bytes adapterData;
    }

    LastSwap public lastSwap;
    uint256 public swapCallCount;

    // ---- supportsSwap toggle (defaults to true) ----
    bool public _supportsSwap = true;

    constructor(bytes32 id, string memory name, string memory version) {
        _adapterId = id;
        _adapterName = name;
        _adapterVersion = version;
    }

    // ============ Setters (test API) ============

    function setQuoteResult(
        bool available,
        uint256 amountOut,
        uint256 priceImpactBps,
        uint256 feeBps,
        uint256 feeAmount,
        bytes calldata adapterData
    ) external {
        quoteAvailable = available;
        quoteAmountOut = amountOut;
        quotePriceImpactBps = priceImpactBps;
        quoteFeeBps = feeBps;
        quoteFeeAmount = feeAmount;
        quoteAdapterData = adapterData;
    }

    function setSwapResult(
        uint256 amountOut,
        uint256 feeAmount,
        bytes calldata adapterData
    ) external {
        swapAmountOut = amountOut;
        swapFeeAmount = feeAmount;
        swapResultAdapterData = adapterData;
    }

    function setRevertOnQuote(bool flag) external {
        revertOnQuote = flag;
    }

    function setRevertOnSwap(bool flag) external {
        revertOnSwap = flag;
    }

    function setSupportsSwap(bool flag) external {
        _supportsSwap = flag;
    }

    function expectAdapterData(bytes calldata data) external {
        expectedAdapterData = data;
        expectedAdapterDataSet = true;
    }

    function clearExpectedAdapterData() external {
        delete expectedAdapterData;
        expectedAdapterDataSet = false;
    }

    // ============ IProtocolAdapter ============

    function getQuote(
        address /* tokenIn */,
        address /* tokenOut */,
        uint256 /* amountIn */
    ) external view override returns (QuoteResult memory result) {
        if (revertOnQuote) revert MockForcedRevert();
        result = QuoteResult({
            amountOut: quoteAmountOut,
            priceImpactBps: quotePriceImpactBps,
            feeBps: quoteFeeBps,
            feeAmount: quoteFeeAmount,
            available: quoteAvailable,
            adapterData: quoteAdapterData
        });
    }

    function executeSwap(
        address tokenIn,
        address tokenOut,
        uint256 amountIn,
        uint256 amountOutMin,
        address from,
        address recipient,
        uint256 deadline,
        bytes calldata adapterData
    ) external override returns (SwapResult memory result) {
        if (revertOnSwap) revert MockForcedRevert();

        if (expectedAdapterDataSet) {
            if (
                adapterData.length != expectedAdapterData.length ||
                keccak256(adapterData) != keccak256(expectedAdapterData)
            ) {
                revert MockUnexpectedAdapterData();
            }
        }

        // Pull tokenIn from `from`.
        if (amountIn > 0) {
            IMockMintableERC20(tokenIn).transferFrom(from, address(this), amountIn);
        }

        // Transfer pre-funded tokenOut from this mock to `recipient`.
        if (swapAmountOut > 0) {
            IMockMintableERC20(tokenOut).transfer(recipient, swapAmountOut);
        }

        lastSwap = LastSwap({
            tokenIn: tokenIn,
            tokenOut: tokenOut,
            amountIn: amountIn,
            amountOutMin: amountOutMin,
            from: from,
            recipient: recipient,
            deadline: deadline,
            adapterData: adapterData
        });
        swapCallCount += 1;

        result = SwapResult({
            amountOut: swapAmountOut,
            feeAmount: swapFeeAmount,
            adapterData: swapResultAdapterData
        });
    }

    function supportsSwap(
        address /* tokenIn */,
        address /* tokenOut */
    ) external view override returns (bool) {
        return _supportsSwap;
    }

    function getSupportedPairs()
        external
        pure
        override
        returns (address[] memory tokenIns, address[] memory tokenOuts)
    {
        tokenIns = new address[](0);
        tokenOuts = new address[](0);
    }

    function getMaxInput(
        address /* tokenIn */,
        address /* tokenOut */
    ) external pure override returns (uint256) {
        return type(uint256).max;
    }

    function adapterId() external view override returns (bytes32) {
        return _adapterId;
    }

    function adapterName() external view override returns (string memory) {
        return _adapterName;
    }

    function adapterVersion() external view override returns (string memory) {
        return _adapterVersion;
    }
}
