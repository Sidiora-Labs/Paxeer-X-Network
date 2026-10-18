/* eslint-disable @typescript-eslint/no-unused-vars */
/* eslint-disable no-console */
import React, { useRef } from "react";
import { useSymbolsInfo, utils } from "@paxeer-network/hooks";
import { i18n, useTranslation } from "@paxeer-network/i18n";
import { useOrderEntryFormErrorMsg } from "@paxeer-network/react-app";
import { MarginMode } from "@paxeer-network/types";
import {
  Box,
  cn,
  convertValueToPercentage,
  Flex,
  Input,
  inputFormatter,
  modal,
  Slider,
  Text,
} from "@paxeer-network/ui";
import { Decimal } from "@paxeer-network/utils";
import type {
  CrossHairMovedEventParams,
  EntityId,
  IChartingLibraryWidget,
  ILineDataSourceApi,
  IOrderLineAdapter,
} from "../charting_library";
import useBroker from "../hooks/useBroker";
import type { ChartPosition } from "../type";
import { determineTpslType, getPositionSide } from "./tpsl.util";

const DEFAULT_THRESHOLD = 10;

enum MouseInteractiveMode {
  NONE,
  START_TP_SL,
  TP_SL_DRAGGING,
  // END_TP_SL,
}

const maskLayerStore = new WeakMap<
  IChartingLibraryWidget,
  { maskLayer: HTMLDivElement }
>();

export class TPSLService {
  private instance: IChartingLibraryWidget;
  private broker: ReturnType<typeof useBroker>;
  private lastPositions: ChartPosition[] | null;

  private interactiveMode: MouseInteractiveMode = MouseInteractiveMode.NONE;

  private tpslElRemoveTimer: ReturnType<typeof setTimeout> | null = null;

  private currentPosition: ChartPosition | null = null;
  private tpslOrderLine: IOrderLineAdapter | null = null;
  private tpslPnLVerticalLineEntityId: EntityId | null = null;
  private tpslVerticalLineTime: number | null = null;
  private tpslStartCircleEntityId: EntityId | null = null;
  private tpslEndCircleEntityId: EntityId | null = null;
  private threshold: number = 10;
  private lastArgs: CrossHairMovedEventParams | null = null;

  constructor(
    instance: IChartingLibraryWidget,
    broker: ReturnType<typeof useBroker>,
    // onUpdate
  ) {
    this.instance = instance;
    this.broker = broker;
    this.lastPositions = null;
    // this.positionLineService = positionLineService;
    this.tpslElRemoveTimer = null;
    this.currentPosition = null;

    this.bindEvent();
  }

  private bindEvent() {
    // console.log("******* TPSL bindEvent *******", this.container);
    // this.chart.setScrollEnabled(false);

    // this.instance.subscribe()

    this.chart.crossHairMoved().subscribe(null, (args) => {
      this.lastArgs = args;

      if (this.interactiveMode === MouseInteractiveMode.TP_SL_DRAGGING) {
        this.clearTpslElRemoveTimer();
        return;
      }
      const position = this.getIntersectantPosition(args);

      if (
        this.currentPosition &&
        position &&
        this.currentPosition.symbol === position.symbol
      ) {
        // console.log("no change");
        return;
      }
      if (!position) {
        this.clearTPSLElements();
        return;
      }

      if (position) {
        this.clearTpslElRemoveTimer();
        this.currentPosition = position;

        this.createTPSLTriggerButton(args);
      }
    });
  }

  private clearTpslElRemoveTimer() {
    if (this.tpslElRemoveTimer) {
      clearTimeout(this.tpslElRemoveTimer);
      this.tpslElRemoveTimer = null;
    }
  }

  private showTPSLDialog(params: { price: number }) {
    const markPrice =
      this.currentPosition?.markPrice ?? this.currentPosition!.open;
    const positionSide = getPositionSide(this.currentPosition!.balance ?? 0);
    const type = determineTpslType(params.price, markPrice, positionSide);

    const marginMode = this.currentPosition?.marginMode ?? MarginMode.CROSS;

    modal
      .show("TPSLSimpleDialogId", {
        title:
          type === "tp"
            ? i18n.t("tpsl.TPOrderConfirm")
            : i18n.t("tpsl.SLOrderConfirm"),
        triggerPrice: params.price,
        type: type,
        marginMode,
        symbol: this.currentPosition!.symbol,
        onComplete: () => {
          this.clearTPSLElements();
          this.chart.setScrollEnabled(true);
          this.chart.setZoomEnabled(true);
          this.interactiveMode = MouseInteractiveMode.NONE;
        },
        showAdvancedTPSLDialog: (options: { qty: number }) => {
          this.showAdvancedTPSLDialog({
            type: type,
            triggerPrice: params.price,
            qty: options.qty,
          });
        },
      })
      .then(
        () => {
          console.log("completate");
        },
        (err) => {
          console.log(err);
        },
      )
      .finally(() => {
        this.clearTPSLElements();
        this.chart.setScrollEnabled(true);
        this.chart.setZoomEnabled(true);
        this.interactiveMode = MouseInteractiveMode.NONE;
      });
  }

  private showAdvancedTPSLDialog({
    type,
    triggerPrice,
    qty,
  }: {
    type: "tp" | "sl";
    triggerPrice: number;
    qty: number;
  }) {
    // console.log("showAdvancedTPSLDialog", type, triggerPrice, qty);
    const onCloseHandler = () => {
      this.clearTPSLElements();
      this.chart.setScrollEnabled(true);
      this.chart.setZoomEnabled(true);
      this.interactiveMode = MouseInteractiveMode.NONE;
    };
    modal
      .show("TPSLDialogId", {
        withTriggerPrice: true,
        type,
        triggerPrice,
        symbol: this.currentPosition?.symbol,
        qty,
        position: this.currentPosition,

        onComplete: onCloseHandler,
        onCancel: onCloseHandler,
      })
      .then(
        () => {
          console.log("completate");
        },
        (err) => {
          console.log("show advanced tpsl dialog error", err);
        },
      )
      .finally(onCloseHandler);
  }

  updatePositions(positions: ChartPosition[] | null) {
    this.lastPositions = positions;
    this.threshold = this.generateThreshold();
  }

  /**
   * Generates a threshold value for position intersection detection.
   * The threshold is calculated as 2% of the visible price range but is capped
   * to ensure it never exceeds the actual price range (highest - lowest price).
   *
   * @returns The calculated threshold value, or DEFAULT_THRESHOLD if calculation fails
   */
  private generateThreshold() {
    // return new Decimal(position.open).mul(0.01).abs().toNumber();
    // const priceRange = this.chart.get
    const priceScale = this.chart.getPanes()[0]?.getRightPriceScales()[0];
    if (!priceScale) {
      return DEFAULT_THRESHOLD;
    }

    try {
      // TradingView's getVisiblePriceRange() can throw "Value is null" error
      // when the price scale is initializing or recalculating
      const priceRange = priceScale.getVisiblePriceRange();
      if (!priceRange) {
        return DEFAULT_THRESHOLD;
      }

      // Calculate the visible price range
      const priceWidth = priceRange.to - priceRange.from;

      // Calculate threshold as 2% of the visible price range
      const calculatedThreshold = priceWidth * 0.02;

      // Ensure threshold doesn't exceed the actual price range
      // This prevents threshold from being larger than the chart's price range
      const threshold = Math.min(calculatedThreshold, priceWidth);

      return threshold;
    } catch (error) {
      // Handle TradingView internal errors gracefully
      // This can occur during chart initialization, symbol switching, or price scale updates
      // console.error('Error getting visible price range:', error);
      return DEFAULT_THRESHOLD;
    }
  }

  private drawTPSL(params: { price: number }) {
    const { price } = params;
    const markPrice =
      this.currentPosition?.markPrice ?? this.currentPosition!.open;
    const positionSide = getPositionSide(this.currentPosition!.balance ?? 0);
    const type = determineTpslType(price, markPrice, positionSide);

    const pnl = new Decimal(price)
      .minus(markPrice)
      .mul(this.currentPosition?.balance ?? 0);

    const { tpslOrderLine, verticalLine } = this.ensureTPSLElements({
      price,
      pnl,
    });
    const direction = type === "tp" ? i18n.t("tpsl.tp") : i18n.t("tpsl.sl");

    const color =
      type === "tp"
        ? this.broker.colorConfig.upColor
        : this.broker.colorConfig.downColor;
    tpslOrderLine
      ?.setText(`${direction} ${pnl.toDecimalPlaces(2).toNumber()}`)
      .setBodyTextColor(color!)
      .setBodyBorderColor(color!)
      .setLineColor(color!);

    if (this.tpslVerticalLineTime) {
      verticalLine?.setPoints([
        { price: this.currentPosition?.open, time: this.tpslVerticalLineTime },
        { time: this.tpslVerticalLineTime, price: price },
      ]);
    }
  }

  private ensureTPSLElements(params: { price: number; pnl: Decimal }) {
    const tpslOrderLine = this.tpslOrderLine;
    let verticalLine;
    let tpslStartCircle;
    let tpslEndCircle;
    // if (this.tpslPnLLabelEntityId) {
    //   tpslPnLLabel = this.chart.getShapeById(this.tpslPnLLabelEntityId);
    // }
    if (!tpslOrderLine) {
      this.tpslOrderLine = this.createTPSLOrderLine();
    }

    if (this.tpslPnLVerticalLineEntityId) {
      verticalLine = this.chart.getShapeById(this.tpslPnLVerticalLineEntityId);
    }

    if (!verticalLine) {
      if (!this.currentPosition || !this.tpslVerticalLineTime) {
        return {};
      }

      this.tpslPnLVerticalLineEntityId = this.chart.createMultipointShape(
        [
          {
            price: this.currentPosition.open,
            time: this.tpslVerticalLineTime,
          },
          { time: this.tpslVerticalLineTime, price: params.price },
        ],
        {
          shape: "trend_line",
          // shape: "parallel_channel",
          lock: true,
          disableSave: true,
          disableSelection: true,
          disableUndo: true,
          zOrder: "top",
          overrides: {
            linecolor: "rgba(var(--oui-color-base-foreground)/0.2)",
            linewidth: 1,
            rightEnd: 1,
            leftEnd: 1,
          },
        },
      );
    }
    verticalLine?.setProperties({
      linecolor: params.pnl.gt(0)
        ? this.broker.colorConfig.upColor
        : this.broker.colorConfig.downColor,
      linewidth: 1,
    });

    return {
      tpslOrderLine,
      verticalLine,
    };
  }

  private createTPSLTriggerButton(params: CrossHairMovedEventParams) {
    if (!this.tpslOrderLine) {
      this.tpslOrderLine = this.createTPSLOrderLine();
    }
    this.tpslOrderLine.onMove(() => {
      const price = this.tpslOrderLine?.getPrice();
      this.showTPSLDialog({ price: price ?? 0 });
    });
    this.tpslOrderLine.onMoving(() => {
      this.interactiveMode = MouseInteractiveMode.TP_SL_DRAGGING;
      const price = this.tpslOrderLine?.getPrice();
      this.verticalLineTime();
      this.drawTPSL({ price: price ?? 0 });
    });
  }

  private createTPSLOrderLine() {
    return (
      this.chart
        .createOrderLine()
        // .setEditable(false)
        .setCancellable(false)
        .setExtendLeft(true)
        .setTooltip(i18n.t("tpsl.dragToSet"))
        .setPrice(this.currentPosition!.open)
        .setLineLength(-200, "pixel")
        .setText(i18n.t("common.tpsl"))
        .setQuantity("")
        .setBodyTextColor(this.broker.colorConfig.textColor!)
        .setBodyBackgroundColor(this.broker.colorConfig.chartBG!)
        .setBodyBorderColor(this.broker.colorConfig.pnlZeroColor!)
        .setQuantityBackgroundColor(this.broker.colorConfig.chartBG!)
        .setQuantityBorderColor(this.broker.colorConfig.pnlZeroColor!)
        .setQuantityTextColor(this.broker.colorConfig.qtyTextColor!)
        .setBodyFont(this.broker.colorConfig.font!)
        .setQuantityFont(this.broker.colorConfig.font!)
        .setLineStyle(3)
      // .setLineColor("rgba(255,255,255,0)")
    );
  }

  private verticalLineTime() {
    const range = this.chart.getVisibleRange();

    // const timeScaler = this.chart.getTimeScale();
    // // console.log(timeScaler.width());
    // const width = timeScaler.width();
    // console.log(width);

    // const time = timeScaler.coordinateToTime(width - 100);
    this.tpslVerticalLineTime = this.getTimeAtPercentage(
      range.from,
      range.to,
      90,
    );
  }

  private clearTPSLElements() {
    if (this.tpslElRemoveTimer) {
      return;
    }
    // check area
    this.tpslElRemoveTimer = setTimeout(() => {
      this.currentPosition = null;
      if (this.tpslOrderLine) {
        this.tpslOrderLine.remove();
        this.tpslOrderLine = null;
      }

      if (this.tpslPnLVerticalLineEntityId) {
        this.chart.removeEntity(this.tpslPnLVerticalLineEntityId);
        this.tpslPnLVerticalLineEntityId = null;
      }
      this.tpslElRemoveTimer = null;
    }, 100);
    // this.tpslVerticalLineTime = null;
  }

  private getIntersectantPosition(params: CrossHairMovedEventParams) {
    if (!Array.isArray(this.lastPositions) || this.lastPositions.length === 0) {
      return null;
    }

    const { price, time } = params;
    let intersectantPosition: ChartPosition | null = null;

    for (const position of this.lastPositions) {
      if (position) {
        if (Math.abs(position.open - price) < this.threshold) {
          intersectantPosition = position;
          break;
        }
      }
    }

    return intersectantPosition;
  }

  private get chart() {
    return this.instance.activeChart();
  }

  private getTimeAtPercentage(
    startTime: number,
    endTime: number,
    percentage: number,
  ) {
    // --- 1. Input Validation (Very Important) ---
    if (
      typeof startTime !== "number" ||
      typeof endTime !== "number" ||
      typeof percentage !== "number"
    ) {
      console.error(
        "Input Error: startTime, endTime, and percentage must all be numbers.",
      );
      return null;
    }
    if (percentage < 0 || percentage > 100) {
      console.error(
        `Input Error: Percentage (${percentage}) must be between 0 and 100.`,
      );
      return null;
    }
    if (startTime > endTime) {
      console.error(
        `Input Error: Start time (${startTime}) cannot be later than end time (${endTime}).`,
      );
      return null;
    }

    // --- 2. Core Algorithm ---
    // Calculate total duration
    const duration = endTime - startTime;

    // Convert percentage to decimal (e.g., 50 -> 0.5)
    const factor = percentage / 100.0;

    // Calculate offset
    const offset = duration * factor;

    // Calculate final time
    const targetTime = startTime + offset;

    // --- 3. Return Result ---
    // Timestamps are usually integers, round to nearest integer
    return Math.round(targetTime);
  }
}
