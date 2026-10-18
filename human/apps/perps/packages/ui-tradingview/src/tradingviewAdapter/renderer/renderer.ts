import {
  IChartingLibraryWidget,
  IBrokerConnectionAdapterHost,
} from "../charting_library";
import useBroker from "../hooks/useBroker";
import { ChartPosition } from "../type";
import { ExecutionService } from "./execution.service";
import {
  LiquidationLineService,
  type LiquidationLineRenderParams,
} from "./liquidationLine.service";
import { OrderLineService } from "./orderLine.service";
import { PositionLineService } from "./positionLine.service";
import { TPSLService } from "./tpsl.service";

export class Renderer {
  private instance: IChartingLibraryWidget;
  private positionLineService: PositionLineService;
  private orderLineService: OrderLineService;
  private executionService: ExecutionService;
  private tpslService: TPSLService;
  private liquidationLineService: LiquidationLineService;

  constructor(
    instance: IChartingLibraryWidget,
    host: IBrokerConnectionAdapterHost,
    broker: ReturnType<typeof useBroker>,
  ) {
    this.instance = instance;
    this.positionLineService = new PositionLineService(instance, broker);
    this.orderLineService = new OrderLineService(instance, broker);
    this.executionService = new ExecutionService(instance, broker);
    this.tpslService = new TPSLService(instance, broker);
    this.liquidationLineService = new LiquidationLineService(instance, broker);
  }

  async renderPositions(positions: ChartPosition[] | null) {
    await this.chartReady();
    await this.onDataLoaded();
    this.positionLineService.renderPositions(positions);
    this.orderLineService.updatePositions(positions);
    this.tpslService.updatePositions(positions);
  }

  /** Update liquidation price lines (positions + optional estimated from Order Entry). */
  renderLiquidationLine(params: LiquidationLineRenderParams): void {
    this.liquidationLineService.renderLiquidationLine(params);
  }

  async renderPendingOrders(pendingOrders: any) {
    await this.chartReady();
    this.orderLineService.renderPendingOrders(pendingOrders);
  }

  async renderFilledOrders(filledOrders: any, basePriceDecimal: number) {
    await this.chartReady();
    await this.onDataLoaded();

    this.executionService.renderExecutions(filledOrders, basePriceDecimal);
  }

  remove() {
    this.orderLineService.removeAll();
    this.positionLineService.removePositions();
    this.liquidationLineService.remove();
    this.executionService.destroy();
  }

  onDataLoaded(): Promise<void> {
    if (this.instance.activeChart().symbolExt()) {
      return Promise.resolve();
    }

    return new Promise((resolve) =>
      this.instance
        .activeChart()
        .onDataLoaded()
        .subscribe(
          null,
          () => {
            resolve();
          },
          true,
        ),
    );
  }

  chartReady(): Promise<void> {
    return new Promise((resolve) =>
      this.instance.onChartReady(() => {
        try {
          this.instance.activeChart().dataReady(() => resolve());
        } catch (err: any) {
          if (err!.toString().includes("tradingViewApi")) {
            // hot reload error will be silent
          } else {
            console.log("-- chartReady error", err);
            // throw err;
          }
        }
      }),
    );
  }
}
