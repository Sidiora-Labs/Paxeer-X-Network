// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.27;

import {IERC20Minimal} from "../interfaces/IERC20Minimal.sol";

/// @title MockSidioraRouter
/// @notice Spy/stub for the live Sidiora IRouter surface consumed by SidioraAdapter.
/// @dev Only the three methods SidioraAdapter calls are implemented:
///       - buy(pool, usdlAmountIn, minTokensOut, deadline)
///       - sell(pool, tokenAmountIn, minUsdlOut, deadline)
///       - swapTokenForToken(tokenIn, tokenOut, amountIn, minAmountOut, deadline)
///            returns (amountOut, intermediateUsdl)  ← 2-tuple — regression surface
///
///      Not inheriting the full IRouter keeps this mock lightweight and avoids
///      dragging unrelated signatures (createMarket, permit variants, events) into
///      the adapter unit test scope.
///
///      The mock captures:
///        - call args (pool, amountIn, minOut, deadline, caller)
///        - call counts
///        - allowance observed at call entry (witnesses S9 zero→amountIn state)
///
///      The mock performs real ERC20 transfers to match the live Router's
///      transferFrom → transfer flow: pulls tokenIn from msg.sender using the
///      allowance the adapter just set, then sends a pre-funded amount of tokenOut
///      back. Pre-fund this mock with the required tokenOut balance before testing.
contract MockSidioraRouter {
    // ============ RECORDED STATE ============

    struct LastBuy {
        address pool;
        uint256 usdlAmountIn;
        uint256 minTokensOut;
        uint256 deadline;
        address caller;
        uint256 observedAllowance;
    }

    struct LastSell {
        address pool;
        uint256 tokenAmountIn;
        uint256 minUsdlOut;
        uint256 deadline;
        address caller;
        uint256 observedAllowance;
    }

    struct LastMultihop {
        address tokenIn;
        address tokenOut;
        uint256 amountIn;
        uint256 minAmountOut;
        uint256 deadline;
        address caller;
        uint256 observedAllowance;
    }

    LastBuy public lastBuy;
    LastSell public lastSell;
    LastMultihop public lastMultihop;

    uint256 public buyCallCount;
    uint256 public sellCallCount;
    uint256 public multihopCallCount;

    // ============ CONFIGURABLE RETURNS ============

    uint256 public buyReturn;
    uint256 public sellReturn;
    uint256 public multihopAmountOut;
    uint256 public multihopIntermediateUsdl;

    // ============ BEHAVIOR SWITCHES ============

    bool public revertOnBuy;
    bool public revertOnSell;
    bool public revertOnMultihop;
    string public revertReason;

    /// @notice If true, skip the inbound transferFrom. Useful for edge-case tests
    ///         where the adapter's approval flow should be observed without real
    ///         token movement.
    bool public skipTransfers;

    // ============ TOKEN ROUTING ============

    address public usdl;
    /// @dev For `buy(pool, ...)`: which token to send back. Set once per test.
    address public buyTokenOut;
    /// @dev For `sell(pool, ...)`: always returns USDL — no separate field.

    // ============ CONFIG ============

    function setUsdl(address usdl_) external {
        usdl = usdl_;
    }

    function setBuyTokenOut(address token) external {
        buyTokenOut = token;
    }

    function setBuyReturn(uint256 v) external {
        buyReturn = v;
    }

    function setSellReturn(uint256 v) external {
        sellReturn = v;
    }

    function setMultihopReturn(uint256 amountOut_, uint256 intermediateUsdl_) external {
        multihopAmountOut = amountOut_;
        multihopIntermediateUsdl = intermediateUsdl_;
    }

    function setRevertOnBuy(bool v, string calldata reason) external {
        revertOnBuy = v;
        revertReason = reason;
    }

    function setRevertOnSell(bool v, string calldata reason) external {
        revertOnSell = v;
        revertReason = reason;
    }

    function setRevertOnMultihop(bool v, string calldata reason) external {
        revertOnMultihop = v;
        revertReason = reason;
    }

    function setSkipTransfers(bool v) external {
        skipTransfers = v;
    }

    function resetCounts() external {
        buyCallCount = 0;
        sellCallCount = 0;
        multihopCallCount = 0;
    }

    // ============ IRouter.buy ============

    function buy(
        address pool,
        uint256 usdlAmountIn,
        uint256 minTokensOut,
        uint256 deadline
    ) external returns (uint256 amountOut) {
        if (revertOnBuy) revert(revertReason);

        uint256 allow = IERC20Minimal(usdl).allowance(msg.sender, address(this));

        lastBuy = LastBuy({
            pool: pool,
            usdlAmountIn: usdlAmountIn,
            minTokensOut: minTokensOut,
            deadline: deadline,
            caller: msg.sender,
            observedAllowance: allow
        });
        buyCallCount++;

        if (!skipTransfers) {
            // Pull USDL from caller (exercises adapter's S9 approval).
            IERC20Minimal(usdl).transferFrom(msg.sender, address(this), usdlAmountIn);
            // Send tokenOut back.
            if (buyReturn > 0) {
                IERC20Minimal(buyTokenOut).transfer(msg.sender, buyReturn);
            }
        }

        return buyReturn;
    }

    // ============ IRouter.sell ============

    function sell(
        address pool,
        uint256 tokenAmountIn,
        uint256 minUsdlOut,
        uint256 deadline
    ) external returns (uint256 amountOut) {
        if (revertOnSell) revert(revertReason);

        // tokenIn for a sell is the pool's token — the SidioraAdapter's caller
        // already transferred it into the adapter, which then approved this mock.
        // We don't track tokenIn by name here; the adapter passes it via its own
        // state. We observe allowance via the caller (adapter) directly.
        //
        // For witness purposes: we need to pull tokenAmountIn of *some* token.
        // The adapter passes a concrete tokenIn at call site; we fetch its
        // allowance via a helper the test sets up. To keep this mock simple,
        // the test must set `lastTokenInForSell` via a separate setter, OR
        // skip transfers and verify approval events externally.

        // Simplest approach: skip inbound transfer in sell; test asserts
        // allowance on the tokenIn contract directly.
        lastSell = LastSell({
            pool: pool,
            tokenAmountIn: tokenAmountIn,
            minUsdlOut: minUsdlOut,
            deadline: deadline,
            caller: msg.sender,
            observedAllowance: 0 // see below — captured via sellTokenInSnapshot
        });
        sellCallCount++;

        if (!skipTransfers) {
            // The adapter passes the pool's token in via approval; we honor
            // that by pulling via the tokenIn address set in sellTokenIn.
            if (sellTokenIn != address(0)) {
                lastSell.observedAllowance = IERC20Minimal(sellTokenIn).allowance(
                    msg.sender,
                    address(this)
                );
                IERC20Minimal(sellTokenIn).transferFrom(
                    msg.sender,
                    address(this),
                    tokenAmountIn
                );
            }
            // Send USDL back.
            if (sellReturn > 0) {
                IERC20Minimal(usdl).transfer(msg.sender, sellReturn);
            }
        }

        return sellReturn;
    }

    /// @dev Test sets this BEFORE calling the adapter's executeSwap for a sell,
    ///      so the mock knows which ERC20 to pull from the adapter.
    address public sellTokenIn;

    function setSellTokenIn(address token) external {
        sellTokenIn = token;
    }

    // ============ IRouter.swapTokenForToken ============
    // ⚠ REGRESSION SURFACE: returns (amountOut, intermediateUsdl) 2-tuple.
    //   SidioraAdapter must unpack only the first element — port adaptation
    //   vs the dev/ version's assumed 1-tuple return.

    function swapTokenForToken(
        address tokenIn,
        address tokenOut,
        uint256 amountIn,
        uint256 minAmountOut,
        uint256 deadline
    ) external returns (uint256 amountOut, uint256 intermediateUsdl) {
        if (revertOnMultihop) revert(revertReason);

        uint256 allow = IERC20Minimal(tokenIn).allowance(msg.sender, address(this));

        lastMultihop = LastMultihop({
            tokenIn: tokenIn,
            tokenOut: tokenOut,
            amountIn: amountIn,
            minAmountOut: minAmountOut,
            deadline: deadline,
            caller: msg.sender,
            observedAllowance: allow
        });
        multihopCallCount++;

        if (!skipTransfers) {
            IERC20Minimal(tokenIn).transferFrom(msg.sender, address(this), amountIn);
            if (multihopAmountOut > 0) {
                IERC20Minimal(tokenOut).transfer(msg.sender, multihopAmountOut);
            }
        }

        return (multihopAmountOut, multihopIntermediateUsdl);
    }
}
