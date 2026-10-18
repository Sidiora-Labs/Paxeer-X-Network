import { i18n } from "@paxeer-network/i18n";
import { IChartingLibraryWidget, IOrderLineAdapter } from "../charting_library";
import useBroker from "../hooks/useBroker";

/** Supported position margin mode labels for liquidation lines. */
type LiquidationMarginMode = "ISOLATED" | "CROSS";

/** Position liquidation line item (price + margin mode). */
interface PositionLiqLineItem {
  price: number;
  marginMode?: LiquidationMarginMode;
  /** Optional leverage shown after margin mode (e.g. `CROSS 10x`). */
  leverage?: number | null;
}

/** Params for rendering liquidation price lines (position list + optional estimated). */
export interface LiquidationLineRenderParams {
  /** Position liquidation line items (same symbol, may contain cross + isolated). */
  positionLiqItems: PositionLiqLineItem[];
  /** Order Entry estimated liquidation price; null when invalid or "--". */
  estimatedLiqPrice: number | null;
}

/**
 * Maintains liquidation lines on the chart.
 * Estimated liq. price keeps higher priority: when estimated is valid, only estimated line is shown.
 * When estimated is unavailable, all valid position liq. prices are rendered as multiple lines.
 */
export class LiquidationLineService {
  private instance: IChartingLibraryWidget;
  private broker: ReturnType<typeof useBroker>;
  /** Single temporary line driven by Order Entry estimated liquidation price. */
  private estimatedLine: IOrderLineAdapter | null = null;
  /** Persistent lines driven by current positions (can be multiple, e.g. cross + isolated). */
  private positionLines: IOrderLineAdapter[] = [];

  constructor(
    instance: IChartingLibraryWidget,
    broker: ReturnType<typeof useBroker>,
  ) {
    this.instance = instance;
    this.broker = broker;
  }

  /**
   * Render or update liquidation lines. Call when position or estimated liq. price changes.
   */
  renderLiquidationLine(params: LiquidationLineRenderParams): void {
    const { positionLiqItems, estimatedLiqPrice } = params;

    const hasValidEst =
      estimatedLiqPrice != null && Number.isFinite(estimatedLiqPrice);
    const validPositionItems = (positionLiqItems ?? []).filter(
      (item): item is PositionLiqLineItem =>
        item != null && item.price != null && Number.isFinite(item.price),
    );

    if (hasValidEst) {
      this.removePositionLines();
      this.setEstimatedLinePrice(estimatedLiqPrice!);
      this.estimatedLine?.setLineStyle(1);
      return;
    }

    this.removeEstimatedLine();

    if (validPositionItems.length > 0) {
      this.renderPositionLines(validPositionItems);
      return;
    }

    this.removePositionLines();
  }

  /** Remove all liquidation lines (e.g. on destroy). */
  remove(): void {
    this.removeEstimatedLine();
    this.removePositionLines();
  }

  /** Build a TradingView order line and apply shared style. */
  private createBaseLine(): IOrderLineAdapter | null {
    try {
      const activeChart = this.instance.activeChart();
      if (!activeChart) return null;

      const orderLine = activeChart.createOrderLine();
      if (!orderLine) return null;

      const colorConfig = this.broker.colorConfig;
      /** Use liqLineColor from CSS var (--oui-color-warning-light); do not hardcode. */
      const lineColor = colorConfig.liqLineColor ?? colorConfig.textColor;
      if (!lineColor) return null;

      let line = orderLine
        .setCancellable(false)
        .setLineLength(100)
        .setEditable(false)
        .setExtendLeft(true)
        .setQuantity("")
        .setLineStyle(0)
        .setLineColor(lineColor)
        .setBodyBorderColor(lineColor);

      if (colorConfig.chartBG) {
        line = line
          .setBodyBackgroundColor(colorConfig.chartBG)
          .setQuantityBackgroundColor(colorConfig.chartBG);
      }
      if (colorConfig.textColor) {
        line = line.setBodyTextColor(colorConfig.textColor);
      }
      if (colorConfig.font) {
        line = line
          .setBodyFont(colorConfig.font)
          .setQuantityFont(colorConfig.font);
      }
      return line;
    } catch {
      return null;
    }
  }

  /** Get or create the single estimated liquidation line. */
  private getOrCreateEstimatedLine(): IOrderLineAdapter | null {
    if (this.estimatedLine != null) {
      return this.estimatedLine;
    }
    const line = this.createBaseLine();
    if (line) {
      this.estimatedLine = line;
      this.updateLineLabel(line, true);
    }
    return line;
  }

  /** Get or create one position liquidation line by index. */
  private getOrCreatePositionLine(index: number): IOrderLineAdapter | null {
    if (this.positionLines[index] != null) {
      return this.positionLines[index];
    }
    const line = this.createBaseLine();
    if (line) {
      this.positionLines[index] = line;
      this.updateLineLabel(line, false);
    }
    return line;
  }

  /** Update estimated line price and metadata. */
  private setEstimatedLinePrice(price: number): void {
    const line = this.getOrCreateEstimatedLine();
    if (!line) return;
    line.setPrice(price);
    this.updateLineLabel(line, true);
  }

  /** Sync position lines to input items (create/update/remove extra lines). */
  private renderPositionLines(items: PositionLiqLineItem[]): void {
    for (let i = 0; i < items.length; i += 1) {
      const line = this.getOrCreatePositionLine(i);
      if (!line) continue;
      line.setLineStyle(0);
      line.setPrice(items[i].price);
      this.updateLineLabel(line, false, items[i].marginMode, items[i].leverage);
    }

    // Remove stale extra lines when position count decreases.
    for (let i = items.length; i < this.positionLines.length; i += 1) {
      this.positionLines[i]?.remove();
    }
    this.positionLines = this.positionLines.slice(0, items.length);
  }

  /** Remove the estimated line if it exists. */
  private removeEstimatedLine(): void {
    if (this.estimatedLine) {
      this.estimatedLine.remove();
      this.estimatedLine = null;
    }
  }

  /** Remove all position lines if they exist. */
  private removePositionLines(): void {
    this.positionLines.forEach((line) => line.remove());
    this.positionLines = [];
  }

  /**
   * Ensure line tooltip/text reflect its source (estimated vs position + margin mode).
   * When showing margin mode, we also append leverage (if available) right after it.
   */
  private updateLineLabel(
    line: IOrderLineAdapter,
    isEstimated: boolean,
    marginMode?: LiquidationMarginMode,
    leverage?: number | null,
  ): void {
    const key = isEstimated
      ? "orderEntry.estLiqPrice"
      : "positions.column.liqPrice";
    let label = i18n.t(key);
    if (!isEstimated && marginMode) {
      const marginModeLabel = i18n.t(
        marginMode === "ISOLATED" ? "marginMode.isolated" : "marginMode.cross",
      );
      const leverageLabel =
        leverage != null && Number.isFinite(leverage) ? ` ${leverage}x` : "";
      // Keep formatting consistent with `PositionLineService`: margin mode first, then leverage.
      label += ` (${marginModeLabel}${leverageLabel})`;
    }
    line.setTooltip(label).setText(label);
  }
}
