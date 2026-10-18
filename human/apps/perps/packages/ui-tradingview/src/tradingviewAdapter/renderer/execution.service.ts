import { i18n } from "@paxeer-network/i18n";
import { commify, Decimal } from "@paxeer-network/utils";
import { limitOrdersByInterval } from "../broker/utils";
import {
  IChartingLibraryWidget,
  IExecutionLineAdapter,
  ResolutionString,
} from "../charting_library";
import useBroker from "../hooks/useBroker";
import { OrderInterface, SideType } from "../type";

export class ExecutionService {
  private instance: IChartingLibraryWidget;
  private interval = "1D";
  private executions: IExecutionLineAdapter[];
  private filledOrders: OrderInterface[];
  private basePriceDecimal: number;
  private broker: ReturnType<typeof useBroker>;

  constructor(
    instance: IChartingLibraryWidget,
    broker: ReturnType<typeof useBroker>,
  ) {
    this.instance = instance;
    this.executions = [];
    this.filledOrders = [];
    this.basePriceDecimal = 0;
    this.broker = broker;

    this.subscribeIntervalChange();
  }

  async subscribeIntervalChange() {
    this.interval = this.instance.symbolInterval().interval;

    const changeInterval = this.changeInterval;
    this.instance
      .activeChart()
      .onIntervalChanged()
      .subscribe(null, changeInterval);
  }

  changeInterval = (interval: ResolutionString) => {
    const rerenderExecutions = () => {
      this.renderExecutions(this.filledOrders, this.basePriceDecimal);
      this.instance
        .activeChart()
        .onDataLoaded()
        .unsubscribe(null, rerenderExecutions);
    };

    this.interval = interval;
    this.instance
      .activeChart()
      .onDataLoaded()
      .subscribe(null, rerenderExecutions);
  };

  renderExecutions(filledOrders: OrderInterface[], basePriceDecimal: number) {
    this.filledOrders = filledOrders;
    this.basePriceDecimal = basePriceDecimal;

    if (!this.interval) {
      return;
    }

    this.removeAll();

    limitOrdersByInterval(filledOrders, this.interval).forEach((order) => {
      this.executions.push(this.drawExecution(order, basePriceDecimal));
    });
  }
  removeAll() {
    this.executions.forEach((execution) => execution.remove());
    this.executions = [];
  }

  static getExecutionInfo(order: OrderInterface, basePriceDecimal: number) {
    const side = order.side;
    const avgExecPrice =
      order.average_executed_price ||
      order.child_orders?.find((child) => !!child.average_executed_price)
        ?.average_executed_price ||
      0;

    const excutedNumber = new Decimal(avgExecPrice)
      .todp(basePriceDecimal, Decimal.ROUND_FLOOR)
      .toString();
    return `${
      side === SideType.BUY ? i18n.t("common.buy") : i18n.t("common.sell")
    } ${order.total_executed_quantity} @${commify(excutedNumber)}`;
  }

  drawExecution(order: OrderInterface, basePriceDecimal: number) {
    const side = order.side;
    const avgExecPrice =
      order.average_executed_price ||
      order.child_orders?.find((child) => !!child.average_executed_price)
        ?.average_executed_price ||
      0;

    const timestamp = new Date(order.updated_time).getTime() / 1000;
    // const timestamp = new Date(order.updated_time).getTime();
    const colorConfig = this.broker.colorConfig;
    // console.log('-- avag exec price',{
    //   avgExecPrice,
    //   timestamp: new Date(order.updated_time).toLocaleString(),
    //   tooltips: ExecutionService.getExecutionInfo(order, basePriceDecimal),
    // });

    return this.instance
      .activeChart()
      .createExecutionShape()
      .setArrowHeight(9)
      .setTooltip(ExecutionService.getExecutionInfo(order, basePriceDecimal))
      .setTime(timestamp)
      .setPrice(avgExecPrice)
      .setArrowColor(
        side === SideType.BUY ? colorConfig.upColor! : colorConfig.downColor!,
      )
      .setDirection(side === SideType.BUY ? "buy" : "sell");
  }

  unsubscribeIntervalChange() {
    const changeInterval = this.changeInterval;

    try {
      this.instance
        .activeChart()
        .onIntervalChanged()
        .unsubscribe(null, changeInterval);
    } catch (e: unknown) {
      // Check if error is related to null reference (hot reload scenario)
      // This can happen when activeChart(), onIntervalChanged(), or unsubscribe() returns null
      // The error message may contain "tradingViewApi" or "Cannot read properties of null"
      const errorString = e?.toString() || String(e);
      if (
        errorString.includes("tradingViewApi") ||
        errorString.includes("Cannot read properties of null") ||
        errorString.includes("Cannot read property") ||
        (e instanceof TypeError && errorString.includes("null"))
      ) {
        // ignore when it's triggered by hot reload or null reference errors
      } else {
        console.error(e);
      }
    }
  }

  destroy() {
    this.removeAll();
    this.unsubscribeIntervalChange();
  }
}
