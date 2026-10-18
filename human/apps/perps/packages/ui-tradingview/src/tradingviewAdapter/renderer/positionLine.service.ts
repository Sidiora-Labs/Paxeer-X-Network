import { i18n } from "@paxeer-network/i18n";
import { Decimal, commify } from "@paxeer-network/utils";
import {
  IChartingLibraryWidget,
  IPositionLineAdapter,
} from "../charting_library";
import useBroker from "../hooks/useBroker";
import { ChartMode, ChartPosition } from "../type";

export class PositionLineService {
  private instance: IChartingLibraryWidget;
  private positionLines: Record<number, IPositionLineAdapter>;
  private currentSymbol: string;
  private broker: ReturnType<typeof useBroker>;
  private lastPositions: ChartPosition[] | null;

  constructor(
    instance: IChartingLibraryWidget,
    broker: ReturnType<typeof useBroker>,
  ) {
    this.instance = instance;
    this.currentSymbol = "";
    this.broker = broker;
    this.positionLines = {};
    this.lastPositions = null;
  }

  renderPositions(positions: ChartPosition[] | null) {
    if (positions === null || positions.length === 0) {
      this.removePositions();
      return;
    }

    if (this.lastPositions?.length !== positions.length) {
      this.removePositions();
    }

    if (positions[0].symbol !== this.currentSymbol) {
      this.removePositions();
      this.currentSymbol = positions[0].symbol;
    }

    const needDrawMarginMode = positions.length > 1;

    positions.forEach((position, idx) =>
      this.drawPositionLine(position, idx, needDrawMarginMode),
    );
    this.lastPositions = positions;
  }

  getBasePositionLine() {
    const colorConfig = this.broker.colorConfig;
    let line = this.instance
      .activeChart()
      .createPositionLine()
      .setTooltip(i18n.t("positions.closePosition"))
      .setLineLength(100)
      .setLineStyle(1);

    if (colorConfig.chartBG) {
      line = line
        .setQuantityBackgroundColor(colorConfig.chartBG)
        .setCloseButtonBackgroundColor(colorConfig.chartBG);
    }
    if (colorConfig.textColor) {
      line = line.setBodyTextColor(colorConfig.textColor);
    }
    if (colorConfig.qtyTextColor) {
      line = line.setQuantityTextColor(colorConfig.qtyTextColor);
    }
    if (colorConfig.font) {
      line = line
        .setBodyFont(colorConfig.font)
        .setQuantityFont(colorConfig.font);
    }
    return line;
  }

  static getPositionQuantity(balance: number) {
    return commify(new Decimal(balance).todp(4, Decimal.ROUND_DOWN).toString());
  }

  static getPositionPnL(unrealPnl: number, decimal: number) {
    const text = i18n.t("tpsl.pnl");
    const pnl = new Decimal(unrealPnl).toFixed(decimal, Decimal.ROUND_DOWN);
    if (new Decimal(unrealPnl).eq(0)) {
      return `${text} 0`;
    }
    if (new Decimal(unrealPnl).greaterThan(0)) {
      return `${text} +${commify(pnl)}`;
    }
    return `${text} ${commify(pnl)}`;
  }

  removePositions() {
    Object.keys(this.positionLines).forEach((lineId) => {
      this.positionLines[Number(lineId)].remove();
      delete this.positionLines[Number(lineId)];
    });
  }

  drawPositionLine(
    position: ChartPosition,
    idx: number,
    needDrawMarginMode: boolean = false,
  ) {
    const colorConfig = this.broker.colorConfig;
    const isPositiveBalance = position.balance >= 0;
    const pnlDecimal = new Decimal(position.unrealPnl);

    let pnlColor: string | undefined;
    if (pnlDecimal.greaterThan(0)) {
      pnlColor = colorConfig.upColor;
    } else if (pnlDecimal.lessThan(0)) {
      pnlColor = colorConfig.downColor;
    } else {
      pnlColor = colorConfig.pnlZeroColor;
    }
    const sideColor = isPositiveBalance
      ? colorConfig.upColor
      : colorConfig.downColor;
    const price = new Decimal(position.open).toNumber();

    this.positionLines[idx] =
      this.positionLines[idx] ?? this.getBasePositionLine();

    let text = PositionLineService.getPositionPnL(
      position.unrealPnl,
      position.unrealPnlDecimal,
    );

    const quantity = PositionLineService.getPositionQuantity(position.balance);

    if (needDrawMarginMode) {
      // Reuse existing i18n keys for margin mode and append leverage when available.
      const marginModeLabel = i18n.t(
        position.marginMode === "ISOLATED"
          ? "marginMode.isolated"
          : "marginMode.cross",
      );
      const leverageLabel =
        position.leverage != null && Number.isFinite(position.leverage)
          ? ` ${position.leverage}x`
          : "";
      text += ` (${marginModeLabel}${leverageLabel})`;
    }

    const line = this.positionLines[idx]
      .setQuantity(quantity)
      .setPrice(price)
      .setCloseButtonIconColor(colorConfig.closeIconColor!)
      .setCloseButtonBorderColor(sideColor!)
      .setBodyBackgroundColor(pnlColor!)
      .setQuantityTextColor(sideColor!)
      .setBodyBorderColor(pnlColor!)
      .setLineColor(sideColor!)
      .setQuantityBorderColor(sideColor!)
      .setText(text);

    if (colorConfig.closeIconColor) {
      line.setCloseButtonIconColor(colorConfig.closeIconColor);
    }

    if (sideColor) {
      line
        .setCloseButtonBorderColor(sideColor)
        .setQuantityTextColor(sideColor)
        .setLineColor(sideColor)
        .setQuantityBorderColor(sideColor);
    }
    if (pnlColor) {
      line.setBodyBackgroundColor(pnlColor).setBodyBorderColor(pnlColor);
    }

    if (this.broker.mode !== ChartMode.MOBILE) {
      this.positionLines[idx].onClose(null, () => {
        this.broker.closePosition(position);
      });
    }
    // this.positionLines[idx].onClose(null, () => {
    //   this.broker.closePosition(position);
    // });
  }
}
