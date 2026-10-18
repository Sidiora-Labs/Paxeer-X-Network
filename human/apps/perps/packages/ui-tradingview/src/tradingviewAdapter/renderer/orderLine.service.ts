import { i18n } from "@paxeer-network/i18n";
import { isEntirePositionTPSL, isTPSLTriggered } from "@paxeer-network/utils";
import {
  Decimal,
  commify,
  getTrailingStopPrice,
  getPrecisionByNumber,
} from "@paxeer-network/utils";
import { IChartingLibraryWidget, IOrderLineAdapter } from "../charting_library";
import useBroker from "../hooks/useBroker";
import {
  SideType,
  AlgoType,
  OrderCombinationType,
  OrderType,
  OrderInterface,
  ChartPosition,
  ChartMode,
} from "../type";
import { CHART_QTY_DECIMAL, getOrderId } from "./order.util";
import {
  getTpslTag,
  isActivatedPositionTpsl,
  isActivatedQuantityTpsl,
  isPositionTpsl,
  isTpslOrder,
  isTpOrder,
  isSlOrder,
  formatPnl,
  textDash,
} from "./tpsl.util";
import { TpslCalService } from "./tpslCal.service";

export class OrderLineService {
  private instance: IChartingLibraryWidget;
  private pendingOrderLineMap: Map<number, IOrderLineAdapter>;
  private pendingOrders: any[];
  private broker: ReturnType<typeof useBroker>;
  private tpslCalService: TpslCalService;

  constructor(
    instance: IChartingLibraryWidget,
    broker: ReturnType<typeof useBroker>,
  ) {
    this.instance = instance;
    this.pendingOrderLineMap = new Map();
    this.pendingOrders = [];
    this.broker = broker;
    this.tpslCalService = new TpslCalService();
  }

  renderPendingOrders(newPendingOrders: any[]) {
    if (newPendingOrders) {
      this.pendingOrders = newPendingOrders;
    }
    this.cleanOldPendingOrders(this.pendingOrders);
    this.tpslCalService.prepareTpslPnlMap(this.pendingOrders);
    this.tpslCalService.prepareQuantityTpslNoMap(this.pendingOrders);
    const needDrawMarginMode = this.pendingOrders.length > 1;
    this.pendingOrders.forEach((order) =>
      this.renderPendingOrder(order, needDrawMarginMode),
    );
  }

  updatePositions(positions: ChartPosition[] | null) {
    const changed = this.tpslCalService.recalculatePnl(
      positions,
      this.pendingOrders,
    );

    this.pendingOrders
      .filter((order) => changed.includes(getOrderId(order)!))
      .forEach((order) => this.renderPendingOrder(order));
  }

  renderPendingOrder(order: any, needDrawMarginMode: boolean = false) {
    const orderId = OrderLineService.getOrderId(order);
    if (!orderId) {
      return;
    }
    const orderLine = this.drawOrderLine(orderId, order, needDrawMarginMode);

    if (orderLine) {
      this.pendingOrderLineMap.set(orderId, orderLine);
    }
  }

  cleanOldPendingOrders(newPendingOrders: any[]) {
    const newOrderIdSet = new Set(
      newPendingOrders.map((order) => OrderLineService.getOrderId(order)),
    );

    this.pendingOrderLineMap.forEach(
      (_, orderId) =>
        !newOrderIdSet.has(orderId) && this.removePendingOrder(orderId),
    );
  }

  removePendingOrder(orderId: number | undefined) {
    if (orderId === undefined) {
      return;
    }

    const orderLine = this.pendingOrderLineMap.get(orderId);

    if (orderLine) {
      this.pendingOrderLineMap.delete(orderId);
      orderLine.remove();
    }
  }
  /**
   * Creates a base order line with default styling.
   * Returns null if the chart is not ready (e.g., during initialization, hot reload, or chart switching).
   *
   * @returns IOrderLineAdapter instance or null if chart is not available
   */
  getBaseOrderLine() {
    try {
      const activeChart = this.instance.activeChart();

      // Check if activeChart() returned null (can happen during chart initialization, switching, or hot reload)
      if (!activeChart) {
        return null;
      }

      const colorConfig = this.broker.colorConfig;
      const orderLine = activeChart.createOrderLine();

      // Check if createOrderLine() returned null
      if (!orderLine) {
        return null;
      }

      return orderLine
        .setCancelTooltip(i18n.t("orders.cancelOrder"))
        .setQuantityTextColor(colorConfig.qtyTextColor!)
        .setQuantityBackgroundColor(colorConfig.chartBG!)
        .setBodyBackgroundColor(colorConfig.chartBG!)
        .setCancelButtonBackgroundColor(colorConfig.chartBG!)
        .setLineStyle(1)
        .setBodyFont(colorConfig.font!)
        .setQuantityFont(colorConfig.font!);
    } catch (e: unknown) {
      // Handle errors related to null reference (hot reload scenario, chart not ready, etc.)
      // This can happen when activeChart() or createOrderLine() returns null
      // The error message may contain "Value is null" or "Cannot read properties of null"
      const errorString = e?.toString() || String(e);
      if (
        errorString.includes("Value is null") ||
        errorString.includes("tradingViewApi") ||
        errorString.includes("Cannot read properties of null") ||
        errorString.includes("Cannot read property") ||
        (e instanceof TypeError && errorString.includes("null"))
      ) {
        // Return null when chart is not ready - this is expected in some scenarios
        return null;
      }
      // Re-throw unexpected errors
      throw e;
    }
  }

  static getCombinationType(order: any): OrderCombinationType {
    const { algo_type: algoType, type } = order;

    if (
      (algoType === AlgoType.STOP_LOSS ||
        algoType === AlgoType.TAKE_PROFIT ||
        algoType === AlgoType.STOP) &&
      type === OrderType.LIMIT
    ) {
      return OrderCombinationType.STOP_LIMIT;
    }
    if (
      (algoType === AlgoType.STOP_LOSS ||
        algoType === AlgoType.TAKE_PROFIT ||
        algoType === AlgoType.STOP) &&
      type === OrderType.MARKET
    ) {
      return OrderCombinationType.STOP_MARKET;
    }
    if (algoType === AlgoType.BRACKET) {
      if (type === OrderType.LIMIT) {
        return OrderCombinationType.BRACKET_LIMIT;
      }
      if (type === OrderType.MARKET) {
        return OrderCombinationType.BRACKET_MARKET;
      }
    }

    if (algoType === AlgoType.TRAILING_STOP) {
      return OrderCombinationType.TRAILING_STOP;
    }

    return OrderCombinationType.LIMIT;
  }

  static getText(pendingOrder: any) {
    const orderCombinationType =
      OrderLineService.getCombinationType(pendingOrder);
    if (
      orderCombinationType === OrderCombinationType.STOP_LIMIT ||
      orderCombinationType === OrderCombinationType.STOP_MARKET ||
      orderCombinationType === OrderCombinationType.STOP_BRACKET_LIMIT ||
      orderCombinationType === OrderCombinationType.STOP_BRACKET_MARKET
    ) {
      if (pendingOrder.type === OrderType.LIMIT) {
        return `${i18n.t("orderEntry.orderType.stopLimit")} ${commify(
          pendingOrder.price,
        )}`;
      }
      return i18n.t("orderEntry.orderType.stopMarket");
    }
    if (orderCombinationType === OrderCombinationType.TRAILING_STOP) {
      return i18n.t("orderEntry.trailing");
    }
    return i18n.t("orderEntry.orderType.limit");
  }

  static getOrderPrice(pendingOrder: any) {
    if (pendingOrder.algo_type === AlgoType.TRAILING_STOP) {
      return getTrailingStopPrice(pendingOrder);
    }

    return pendingOrder.trigger_price || pendingOrder.price;
  }

  static getOrderId = (order: any) => {
    if (order === null || order === undefined) {
      return undefined;
    }
    return order.algo_order_id || order.order_id;
  };

  /**
   * Builds TP/SL label body: type + PnL (e.g. "Take profit | 123.45 USDC").
   * Falls back to "--" when PnL unavailable.
   */
  getTPSLTextWithTpsl(text: string, pendingOrder: OrderInterface) {
    const orderId = getOrderId(pendingOrder);
    if (!orderId) {
      return text;
    }
    const pnlStr = this.tpslCalService.getFormattedEstPnl(pendingOrder);
    const displayPnl = pnlStr ? pnlStr : formatPnl(undefined);
    return `${text} | ${displayPnl} USDC`;
  }

  getTPSLText(pendingOrder: any) {
    const tpslTypeText = getTpslTag(
      pendingOrder,
      this.tpslCalService.getQuantityTpslNoMap(),
    );

    if (tpslTypeText) {
      return this.getTPSLTextWithTpsl(tpslTypeText, pendingOrder);
    }

    return null;
  }

  /**
   * Returns quantity string for order line. For TP/SL: "qty (percent%)".
   * Uses baseDp from broker.getSymbolInfo; falls back to CHART_QTY_DECIMAL.
   */
  getOrderQuantity(pendingOrder: OrderInterface) {
    if (pendingOrder.algo_order_id) {
      const isTpsl =
        isActivatedPositionTpsl(pendingOrder) ||
        isPositionTpsl(pendingOrder) ||
        isActivatedQuantityTpsl(pendingOrder);
      if (isTpsl) {
        const percentStr = isEntirePositionTPSL(pendingOrder)
          ? "100%"
          : isTPSLTriggered(pendingOrder) || !pendingOrder.position_qty
            ? ""
            : (() => {
                const qty = new Decimal(pendingOrder.quantity).minus(
                  pendingOrder.executed ?? 0,
                );
                const per = qty
                  .div(new Decimal(pendingOrder.position_qty!))
                  .mul(100)
                  .todp(0)
                  .toNumber();
                return `${Math.min(Math.abs(per), 100).toString()}%`;
              })();
        const qty = this.tpslCalService.getTpslQuantity(pendingOrder);
        const symbolInfo = this.broker.getSymbolInfo?.(pendingOrder.symbol);
        const baseDp =
          symbolInfo?.baseTick != null
            ? getPrecisionByNumber(symbolInfo.baseTick)
            : CHART_QTY_DECIMAL;
        const qtyStr =
          qty != null
            ? commify(new Decimal(qty).todp(baseDp).toString())
            : textDash;
        return percentStr ? `${qtyStr} (${percentStr})` : qtyStr;
      }
    }
    return commify(new Decimal(pendingOrder.quantity).toString());
  }

  drawOrderLine(
    orderId: number,
    pendingOrder: any,
    needDrawMarginMode: boolean = false,
  ) {
    // const text = OrderLineService.getText(pendingOrder);
    let text = isTpslOrder(pendingOrder)
      ? this.getTPSLText(pendingOrder)
      : OrderLineService.getText(pendingOrder);
    if (text === null) {
      return null;
    }

    const colorConfig = this.broker.colorConfig;
    const existingOrderLine = this.pendingOrderLineMap.get(orderId);
    const orderLine = existingOrderLine ?? this.getBaseOrderLine();

    // If getBaseOrderLine() returned null (chart not ready), skip rendering this order
    if (!orderLine) {
      return null;
    }

    // Base colors from side; for TP/SL orders override by algo_type (TP = profit/up, SL = loss/down)
    let color =
      pendingOrder.side === SideType.BUY
        ? colorConfig.upColor
        : colorConfig.downColor;
    let borderColor =
      pendingOrder.side === SideType.BUY
        ? colorConfig.pnlUpColor
        : colorConfig.pnlDownColor;
    if (isTpslOrder(pendingOrder)) {
      if (isTpOrder(pendingOrder)) {
        color = colorConfig.upColor;
        borderColor = colorConfig.pnlUpColor;
      } else if (isSlOrder(pendingOrder)) {
        color = colorConfig.downColor;
        borderColor = colorConfig.pnlDownColor;
      }
    }
    const price = OrderLineService.getOrderPrice(pendingOrder);
    const lineLength = 100;
    const quantity = this.getOrderQuantity(pendingOrder);
    const textColor = colorConfig.textColor;

    if (needDrawMarginMode) {
      text += ` (${pendingOrder.margin_mode === "ISOLATED" ? "Isolated" : "Cross"})`;
    }

    orderLine
      .setText(text)
      .setCancelButtonIconColor(colorConfig.closeIconColor!)
      .setCancelButtonBorderColor(color!)
      .setBodyTextColor(textColor!)
      .setBodyBorderColor(borderColor!)
      .setQuantityBorderColor(borderColor!)
      .setQuantityTextColor(color!)
      // .setBodyBackgroundColor(color)
      .setLineColor(color!)
      .setLineLength(lineLength)
      .setQuantity(quantity)
      .setPrice(price);

    if (this.broker.mode !== ChartMode.MOBILE) {
      orderLine.onCancel(null, () => this.broker.cancelOrder(pendingOrder));
      this.applyEditOnMove(orderLine, pendingOrder);
    } else {
      orderLine.setEditable(false).setCancellable(false);
    }
    // orderLine.onCancel(null, () => this.broker.cancelOrder(pendingOrder));
    // this.applyEditOnMove(orderLine, pendingOrder);

    return orderLine;
  }

  static getOrderEditKey(pendingOrder: any) {
    if (isTpslOrder(pendingOrder)) return "trigger_price";
    const orderCombinationType = this.getCombinationType(pendingOrder);

    if (
      [OrderCombinationType.LIMIT, OrderCombinationType.BRACKET_LIMIT].includes(
        orderCombinationType,
      )
    ) {
      return "price";
    }

    if (
      orderCombinationType === OrderCombinationType.STOP_LIMIT ||
      orderCombinationType === OrderCombinationType.STOP_MARKET ||
      pendingOrder.rootAlgoOrderAlgoType === AlgoType.POSITIONAL_TP_SL ||
      pendingOrder.rootAlgoOrderAlgoType === AlgoType.TP_SL ||
      pendingOrder.rootAlgoOrderAlgoType === AlgoType.BRACKET ||
      pendingOrder.rootAlgoOrderAlgoType === AlgoType.STOP_BRACKET
    ) {
      return "trigger_price";
    }
  }

  applyEditOnMove(orderLine: IOrderLineAdapter, pendingOrder: any) {
    const editKey = OrderLineService.getOrderEditKey(pendingOrder);
    if (!editKey) {
      return;
    }
    orderLine.onMove(() => {
      this.broker
        .editOrder(pendingOrder, {
          type: editKey,
          value: `${orderLine.getPrice()}`,
        })
        .then((res: any) => {
          if (!res.success) {
            this.renderPendingOrder(pendingOrder); // go back to the original price
          }
        })
        .catch(() => this.renderPendingOrder(pendingOrder));
    });
  }

  removeAll() {
    this.pendingOrderLineMap.forEach((orderLine) => orderLine.remove());
    this.pendingOrderLineMap.clear();
    this.pendingOrders = [];
    this.tpslCalService.clear();
  }
}
