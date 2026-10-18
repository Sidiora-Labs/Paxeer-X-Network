// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.27;
import {IPECOROrders} from "../interfaces/IPECOROrders.sol";
import {IPECORVault} from "../interfaces/IPECORVault.sol";
import {IERC20Minimal} from "../interfaces/IERC20Minimal.sol";
import {IPriceOracle} from "../interfaces/IPriceOracle.sol";
import {SidioraMath} from "../../libraries/SidioraMath.sol";
import {PECORMath} from "./PECORMath.sol";

struct PECOROrderPricing {
    uint256 fee;
    uint256 tier1;
    uint256 tier2;
    uint256 scalar;
    bool impact;
}

interface IPECOROrderViewState is IPECOROrders {
    function priceOracle() external view returns (address);
    function vault() external view returns (address);
    function getPricingConfig() external view returns (PECOROrderPricing memory);
    function orderExecutionPaused() external view returns (bool);
    function orderMinOutput(uint256 id) external view returns (uint256);
    function activeLimitOrderIds(uint256 index) external view returns (uint256);
    function activeStopLimitOrderIds(uint256 index) external view returns (uint256);
    function getActiveLimitOrderCount() external view returns (uint256);
    function getActiveStopLimitOrderCount() external view returns (uint256);
}

/// @notice Immutable read-only companion deployed by each Orders implementation.
/// No delegatecalls, custody or mutable state. Factoring views keeps the trading
/// implementation deployable under EIP-170 without changing compiler safety.
contract PECOROrderViews {
    error OrderNotFound();
    error InvalidDiscoveryKind();

    function price(address orders, address input, address output, uint256 amount)
        public view returns (uint256 gross, uint256 net)
    {
        IPECOROrderViewState state = IPECOROrderViewState(orders);
        IPriceOracle oracle = IPriceOracle(state.priceOracle());
        IPECORVault vault = IPECORVault(state.vault());
        PECOROrderPricing memory config = state.getPricingConfig();
        uint256 priceIn = oracle.getPrice(input);
        uint256 priceOut = oracle.getPrice(output);
        gross = PECORMath.convert(amount, priceIn, priceOut, vault.getTokenDecimals(input), vault.getTokenDecimals(output), false);
        uint256 volume = SidioraMath.mulDiv(amount, priceIn, 10 ** vault.getTokenDecimals(input));
        uint256 fees = config.fee;
        if (volume >= 10_000e18) fees += config.tier1;
        if (volume >= 100_000e18) fees += config.tier2;
        net = gross - SidioraMath.mulDiv(gross, fees, 10_000);
        uint256 reserveUSD = SidioraMath.mulDiv(vault.getAvailableReserves(output), priceOut, 10 ** vault.getTokenDecimals(output));
        if (config.impact && reserveUSD != 0) {
            uint256 impact = SidioraMath.mulDiv(volume, config.scalar, reserveUSD);
            if (impact > 500) impact = 500;
            net -= SidioraMath.mulDiv(net, impact, 10_000);
        }
    }

    function quoteOrder(address orders, uint256 id, bool stop) external view returns (address output, uint256 gross, uint256 net) {
        IPECOROrderViewState state = IPECOROrderViewState(orders);
        address input;
        uint256 amount;
        if (stop) {
            IPECOROrders.StopLimitOrder memory o = state.getStopLimitOrder(id);
            if (o.id == 0) revert OrderNotFound();
            bool buy = o.orderType == IPECOROrders.OrderType.STOP_LIMIT_BUY;
            input = buy ? o.stablecoin : o.token; output = buy ? o.token : o.stablecoin; amount = o.amount;
        } else {
            IPECOROrders.LimitOrder memory o = state.getLimitOrder(id);
            if (o.id == 0) revert OrderNotFound();
            bool buy = o.orderType == IPECOROrders.OrderType.LIMIT_BUY;
            input = buy ? o.stablecoin : o.token; output = buy ? o.token : o.stablecoin; amount = o.amount;
        }
        (gross, net) = price(orders, input, output, amount);
    }

    function canExecuteLimitOrder(address orders, uint256 id) public view returns (bool, string memory) {
        IPECOROrderViewState state = IPECOROrderViewState(orders);
        IPECOROrders.LimitOrder memory order = state.getLimitOrder(id);
        if (order.id == 0) return (false, "Order not found");
        if (order.status != IPECOROrders.OrderStatus.PENDING) return (false, "Order not pending");
        if (state.orderExecutionPaused()) return (false, "Paused");
        if (order.expiresAt != 0 && order.expiresAt <= block.timestamp) return (false, "Order expired");
        uint256 currentPrice;
        try IPriceOracle(state.priceOracle()).getPrice(order.token) returns (uint256 p) { currentPrice = p; }
        catch { return (false, "Price unavailable"); }
        if (order.orderType == IPECOROrders.OrderType.LIMIT_BUY && currentPrice > order.targetPrice) return (false, "Price above target");
        if (order.orderType == IPECOROrders.OrderType.LIMIT_SELL && currentPrice < order.targetPrice) return (false, "Price below target");
        if (order.orderType == IPECOROrders.OrderType.STOP_LOSS && currentPrice > order.targetPrice) return (false, "Price above trigger");
        return _fillFeasibility(state, id, false);
    }

    function _fillFeasibility(IPECOROrderViewState state, uint256 id, bool stop) internal view returns (bool, string memory) {
        try this.quoteOrder(address(state), id, stop) returns (address output, uint256 gross, uint256 net) {
            IPECORVault vault = IPECORVault(state.vault());
            address input;
            if (stop) {
                IPECOROrders.StopLimitOrder memory o = state.getStopLimitOrder(id);
                input = o.orderType == IPECOROrders.OrderType.STOP_LIMIT_BUY ? o.stablecoin : o.token;
            } else {
                IPECOROrders.LimitOrder memory o = state.getLimitOrder(id);
                input = o.orderType == IPECOROrders.OrderType.LIMIT_BUY ? o.stablecoin : o.token;
            }
            try IERC20Minimal(input).balanceOf(address(vault)) returns (uint256 custody) {
                if (custody < vault.getReservedBalance(input)) return (false, "Escrow deficit");
            } catch { return (false, "Escrow unavailable"); }
            if (net == 0 || net < state.orderMinOutput(id)) return (false, "Insufficient output");
            if (!IPECORVault(state.vault()).hasLiquidity(output, gross)) return (false, "Insufficient liquidity");
            return (true, "Executable");
        } catch { return (false, "Price unavailable"); }
    }

    /// @notice kind=0 executable limit, 1 activatable stop, 2 executable stop.
    /// Scan/allocation caps 100. Pin a block across pages: fills swap-and-pop.
    function getOrdersPage(address orders, uint256 cursor, uint256 maxScanned, uint256 maxCount, uint8 kind)
        external view returns (uint256[] memory ids, uint256 next)
    {
        if (kind > 2) revert InvalidDiscoveryKind();
        if (maxScanned > 100) maxScanned = 100;
        if (maxCount > 100) maxCount = 100;
        IPECOROrderViewState state = IPECOROrderViewState(orders);
        uint256 length = kind == 0 ? state.getActiveLimitOrderCount() : state.getActiveStopLimitOrderCount();
        if (cursor > length) cursor = length;
        uint256 end = cursor + (maxScanned < length - cursor ? maxScanned : length - cursor);
        uint256[] memory temp = new uint256[](maxCount);
        uint256 found;
        next = cursor;
        for (; next < end && found < maxCount; ++next) {
            uint256 id = kind == 0 ? state.activeLimitOrderIds(next) : state.activeStopLimitOrderIds(next);
            bool eligible;
            if (kind == 0) (eligible,) = canExecuteLimitOrder(orders, id);
            else {
                IPECOROrders.StopLimitOrder memory o = state.getStopLimitOrder(id);
                if (state.orderExecutionPaused() || o.expiresAt != 0 && o.expiresAt <= block.timestamp) continue;
                if (kind == 1 ? o.status != IPECOROrders.OrderStatus.PENDING : o.status != IPECOROrders.OrderStatus.ACTIVATED) continue;
                try IPriceOracle(state.priceOracle()).getPrice(o.token) returns (uint256 p) {
                    bool buy = o.orderType == IPECOROrders.OrderType.STOP_LIMIT_BUY;
                    eligible = kind == 1 ? (buy ? p >= o.stopPrice : p <= o.stopPrice) : (buy ? p <= o.limitPrice : p >= o.limitPrice);
                } catch { continue; }
                if (eligible && kind == 2) (eligible,) = _fillFeasibility(state, id, true);
            }
            if (eligible) temp[found++] = id;
        }
        ids = new uint256[](found);
        for (uint256 i; i < found; ++i) ids[i] = temp[i];
    }
}
