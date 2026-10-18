import { path } from "ramda";
import { MarginMode } from "@paxeer-network/types";
import { commify } from "@paxeer-network/utils";
import { BasePaint, DrawOptions, layoutInfo } from "./basePaint";
import { qrPaint } from "./qrPaint";

export class DataPaint extends BasePaint {
  private positionInfoCellWidth = 90;

  private DEFAULT_PROFIT_COLOR = "rgb(0,181,159)";
  private DEFAULT_LOSS_COLOR = "rgb(255,103,194)";

  private transformTop = 0;

  private QRCODE_SIZE = 56;
  private BROKER_BADGE_HEIGHT = 18;
  private BROKER_BADGE_PADDING_X = 8;
  private BROKER_BADGE_RADIUS = 4;
  private ITEM_GAP = 7;

  private formatMarginMode(marginMode: MarginMode) {
    return marginMode === MarginMode.ISOLATED ? "Isolated" : "Cross";
  }

  async draw(options: DrawOptions) {
    const needDrawDetails =
      Array.isArray(options.data?.position?.informations) &&
      (options.data?.position?.informations?.length ?? 0) > 0;

    const hasReferral = this.hasReferral(options);

    // const hasMessage = !!options.data?.message;
    const hasMessage = true;

    this.transformTop = hasMessage ? 0 : needDrawDetails ? -40 : -150;

    // If position details are not displayed, the position PNL information will be margin
    // const offsetTop = hasMessage ? 50 : 100;
    const offsetTop = 0; // 100;
    // const offsetMessage = hasMessage ? 0 : -50;

    if (!!options.data?.message) {
      this.drawMessage(options);
    }

    if (!!options.data?.position) {
      this.drawPosition(
        options,
        needDrawDetails || hasReferral ? 0 : offsetTop,
      );
    }

    if (needDrawDetails) {
      this.drawInformations(options);
    }

    this.drawUnrealizedPnL(
      options,
      needDrawDetails || hasReferral ? 0 : offsetTop,
    );

    if (!hasReferral) {
      if (!!options.data?.domain) {
        this.drawDomainUrl(options);
      }

      if (typeof options.data?.updateTime !== "undefined") {
        this.drawPositionTime(options);
      }
    }

    if (typeof options.data?.referral !== "undefined") {
      this.drawReferralCode(options);
    }
  }

  private drawMessage(options: DrawOptions) {
    // console.log("draw message", options);

    const layout = path<layoutInfo>(
      ["layout", "message"],
      options,
    ) as layoutInfo;
    const { position } = layout;

    this._drawText(`"${options.data?.message!}"`, {
      color: layout.color,
      fontSize: this._ratio(layout.fontSize as number),
      top: this._ratio(position.top!),
      left: this.inlineStart(options, position.left!),
      textAlign: this.inlineStartAlign(options),
      direction: this.textDirection(options),
      textBaseline: "top",
      fontFamily: options.fontFamily,
    });
  }
  private drawPosition(options: DrawOptions, offsetTop: number = 0) {
    const layout = path<layoutInfo>(
      ["layout", "position"],
      options,
    ) as layoutInfo;
    const { position, fontSize = 14 } = layout;
    let cursor = this.inlineStart(options, position.left!);

    const top = layout.position.top! + offsetTop + this.transformTop;
    const items: Array<{
      kind: "text" | "badge";
      text: string;
      color?: string;
      fontSize: number;
      fontWeight?: number;
    }> = [];

    if (typeof options.data?.position.side !== "undefined") {
      items.push({
        kind: "text",
        text: options.data.position.side,
        color:
          options.data?.position.side.toUpperCase() === "LONG"
            ? options.profitColor || this.DEFAULT_PROFIT_COLOR
            : options.lossColor || this.DEFAULT_LOSS_COLOR,
        fontSize: this._ratio(fontSize),
      });
    }

    if (typeof options.data?.position.symbol !== "undefined") {
      items.push({
        kind: "text",
        text: options.data.position.symbol,
        color: layout.color,
        fontSize: this._ratio(fontSize),
      });
    }

    const brokerName = options.data?.position.brokerName?.trim();
    if (brokerName) {
      items.push({
        kind: "badge",
        text: brokerName,
        fontSize: this._ratio(12),
        fontWeight: 600,
      });
    }

    const marginMode = options.data?.position.marginMode;
    if (marginMode) {
      items.push({
        kind: "text",
        text: this.formatMarginMode(marginMode),
        color: layout.color,
        fontSize: this._ratio(fontSize),
      });
    }

    if (typeof options.data?.position.leverage !== "undefined") {
      items.push({
        kind: "text",
        text: `${options.data.position.leverage}X`,
        color: layout.color,
        fontSize: this._ratio(fontSize),
      });
    }

    const topPx = this._ratio(top);
    items.forEach((item, index) => {
      cursor = this.drawInlineItem(options, item, cursor, topPx);
      if (index < items.length - 1) {
        cursor = this.drawInlineSeparator(
          options,
          cursor,
          topPx,
          this._ratio(fontSize),
        );
      }
    });
  }

  private _fillRoundedRect(
    x: number,
    y: number,
    width: number,
    height: number,
    radius: number,
    color: string,
  ) {
    const r = Math.min(radius, width / 2, height / 2);
    this.ctx.save();
    this.ctx.fillStyle = color;
    this.ctx.beginPath();
    this.ctx.moveTo(x + r, y);
    this.ctx.arcTo(x + width, y, x + width, y + height, r);
    this.ctx.arcTo(x + width, y + height, x, y + height, r);
    this.ctx.arcTo(x, y + height, x, y, r);
    this.ctx.arcTo(x, y, x + width, y, r);
    this.ctx.closePath();
    this.ctx.fill();
    this.ctx.restore();
  }

  private drawUnrealizedPnL(options: DrawOptions, offsetTop: number = 0) {
    // reset left value;
    const layout = path<layoutInfo>(
      ["layout", "unrealizedPnl"],
      options,
    ) as layoutInfo & {
      secondaryColor: string;
      secondaryFontSize: number;
    };
    const { position } = layout;
    let cursor = this.inlineStart(options, position.left!);
    let prevElementBoundingBox: TextMetrics = {} as TextMetrics;

    const top = (position.top ?? 0) + offsetTop + this.transformTop;

    // ROI
    if (typeof options.data?.position.ROI !== "undefined") {
      const prefix = options.data?.position.ROI! > 0 ? "+" : "";
      prevElementBoundingBox = this._drawText(
        `${prefix}${commify(options.data?.position.ROI)}%`,
        {
          color:
            prefix === "+"
              ? options.profitColor || this.DEFAULT_PROFIT_COLOR
              : options.lossColor || this.DEFAULT_LOSS_COLOR,
          left: cursor,
          top: this._ratio(top),

          fontSize: this._ratio(layout.fontSize as number),
          fontWeight: 700,
          fontFamily: options.fontFamily,
          textAlign: this.inlineStartAlign(options),
          direction: this.textDirection(options),
        },
      );
    }
    // unrelPnL
    if (typeof options.data?.position.pnl !== "undefined") {
      const prefix = options.data?.position.pnl! >= 0 ? "+" : "";
      let text = `${prefix}${commify(options.data?.position.pnl)} ${
        options.data?.position.currency
      }`;
      let fontWeight = 600;

      if (prevElementBoundingBox.width) {
        cursor = this.advanceCursor(
          options,
          cursor,
          (prevElementBoundingBox.width ?? 0) + this._ratio(8),
        );
        text = `(${text})`;
      } else {
        cursor = this.inlineStart(options, position.left!);
        fontWeight = 700;
      }

      const color =
        typeof options.data.position.ROI === "undefined"
          ? prefix === "+"
            ? options.profitColor || this.DEFAULT_PROFIT_COLOR
            : options.lossColor || this.DEFAULT_LOSS_COLOR
          : layout.secondaryColor;

      const fontSize =
        typeof options.data.position.ROI === "undefined"
          ? this._ratio(layout.fontSize as number)
          : this._ratio(layout.secondaryFontSize as number);

      prevElementBoundingBox = this._drawText(text, {
        color,
        left: cursor,
        top: this._ratio(top),
        fontSize,
        fontWeight,
        fontFamily: options.fontFamily,
        textAlign: this.inlineStartAlign(options),
        direction: this.textDirection(options),
      });
    }
  }

  private drawInformations(options: DrawOptions) {
    const layout = path<layoutInfo>(
      ["layout", "informations"],
      options,
    ) as layoutInfo & {
      labelColor?: string;
    };
    const { position } = layout;

    const informations = options.data?.position?.informations || [];

    const isVertical = (options.data?.position.informations.length ?? 0) === 2;
    const col = informations.length > 4 ? 3 : 2;

    informations.forEach((info, index) => {
      const colIndex = index % col;
      const inlineOffset =
        position.left! + colIndex * this.positionInfoCellWidth;

      // let top = (position.top as number) + (index / 2) * 38 + this.transformTop;
      const top =
        (position.top as number) +
        Math.floor(index / col) * 38 +
        this.transformTop;

      this._drawText(info.title, {
        left: this.inlineStart(options, inlineOffset),
        top: this._ratio(top),
        fontSize: this._ratio(10),
        color: layout.labelColor,
        fontFamily: options.fontFamily,
        textAlign: this.inlineStartAlign(options),
        direction: this.textDirection(options),
      });

      this._drawText(info.value, {
        left: this.inlineStart(options, inlineOffset),
        top: this._ratio(top + 17),
        fontSize: this._ratio(layout.fontSize as number),
        fontWeight: 500,
        color: layout.color as string,
        fontFamily: options.fontFamily,
        textAlign: this.inlineStartAlign(options),
        direction: this.textDirection(options),
      });
    });
  }

  private drawDomainUrl(options: DrawOptions, onlyMeasure: boolean = false) {
    const layout = path<layoutInfo>(
      ["layout", "domain"],
      options,
    ) as layoutInfo;

    const hasReferral = this.hasReferral(options);

    const { position } = layout;
    const top = this.painter.height - position.bottom!;

    return this._drawText(
      options.data?.domain!,
      {
        left: !hasReferral
          ? this.inlineStart(options, position.left!)
          : this.inlineEnd(options, 20),
        top: !hasReferral
          ? this._ratio(top)
          : this._ratio(this.painter.height - 16),
        fontSize: this._ratio(layout.fontSize as number),
        color: options.brandColor ?? this.DEFAULT_PROFIT_COLOR,
        fontFamily: options.fontFamily,
        textBaseline: layout.textBaseline,
        textAlign: !hasReferral
          ? this.inlineStartAlign(options)
          : this.inlineEndAlign(options),
        direction: this.textDirection(options),
        fontWeight: 600,
      },
      onlyMeasure,
    );
  }

  private drawPositionTime(options: DrawOptions) {
    const layout = path<layoutInfo>(
      ["layout", "updateTime"],
      options,
    ) as layoutInfo;
    const { position } = layout;
    const hasReferral = this.hasReferral(options);

    let top = this.painter.height - position.bottom!;
    let left = this.inlineStart(options, position.left!);

    if (hasReferral) {
      const metrics = this.drawDomainUrl(options, true);
      // console.log("metrics", metrics);
      left =
        this.inlineStart(options, position.left! + 8) +
        (this.isRTL(options) ? metrics.width : -metrics.width);
      top = this.painter.height - position.bottom!;
      // console.log("left", left, top, metrics.width, this._ratio(top));
    }

    this._drawText(
      !hasReferral
        ? options.data?.updateTime!
        : `Share on ${options.data?.updateTime}   |`,
      {
        left,
        top: this._ratio(top),
        // top: 536,
        fontSize: this._ratio(layout.fontSize as number),
        color: layout.color as string,
        // color: "red",
        textAlign: !hasReferral
          ? this.inlineStartAlign(options)
          : this.inlineEndAlign(options),
        fontFamily: options.fontFamily,
        textBaseline: layout.textBaseline,
        direction: this.textDirection(options),
      },
    );
  }

  private drawReferralCode(options: DrawOptions) {
    if (!options.data?.referral) {
      return;
    }

    const layout = path<layoutInfo>(
      ["layout", "updateTime"],
      options,
    ) as layoutInfo;
    const { position } = layout;
    const top = this.painter.height - (position.bottom ?? 0);

    const messageLayout = path<layoutInfo>(
      ["layout", "message"],
      options,
    ) as layoutInfo;

    const url = new URL(options.data.referral.link);

    const searchParams = url.searchParams;
    searchParams.append("ref", options.data.referral.code);

    url.search = searchParams.toString();

    const qrSize = this._ratio(this.QRCODE_SIZE);
    const qrLeft = this.isRTL(options)
      ? this.inlineStart(options, position.left!) - qrSize
      : this.inlineStart(options, position.left!);

    qrPaint(this.ctx, {
      size: this._ratio(this.QRCODE_SIZE),
      padding: this._ratio(2),
      left: qrLeft,
      top: this._ratio(top - this.QRCODE_SIZE),
      data: `${url.toString()}`,
    });

    this._drawText(options.data.referral.slogan, {
      left: this.inlineStart(options, position.left! + 66),
      top: this._ratio(top - this.QRCODE_SIZE),
      fontSize: this._ratio(14),
      color: options.brandColor ?? this.DEFAULT_PROFIT_COLOR,
      fontFamily: options.fontFamily,
      textBaseline: "top",
      textAlign: this.inlineStartAlign(options),
      direction: this.textDirection(options),
    });

    this._drawText("Referral Code", {
      left: this.inlineStart(options, position.left! + 66),
      top: this._ratio(top - 29),
      fontSize: this._ratio(12),
      color: layout.color as string,
      fontFamily: options.fontFamily,
      textBaseline: "middle",
      textAlign: this.inlineStartAlign(options),
      direction: this.textDirection(options),
    });

    this._drawText(options.data.referral.code, {
      left: this.inlineStart(options, position.left! + 66),
      top: this._ratio(top),
      fontSize: this._ratio(16),
      color: messageLayout.color as string,
      fontFamily: options.fontFamily,
      textBaseline: "bottom",
      textAlign: this.inlineStartAlign(options),
      direction: this.textDirection(options),
    });
  }

  private _drawText(
    str: string,
    options?: {
      left?: number;
      top?: number;
      fontSize?: number;
      fontFamily?: string;
      fontWeight?: number;
      color?: string;
      textBaseline?: CanvasTextBaseline;
      textAlign?: CanvasTextAlign;
      direction?: CanvasDirection;
    },
    onlyMeasure: boolean = false,
  ): TextMetrics {
    let boundingBox: TextMetrics;
    const {
      left = 30,
      top = 30,
      fontSize = 13,
      fontWeight = 500,
      color = "black",
      textBaseline = "middle",
      textAlign = "start",
      direction = "ltr",
    } = options ?? {};

    this.ctx.save();
    // "Nunito Sans",-apple-system,"San Francisco",BlinkMacSystemFont,"Segoe UI","Helvetica Neue",Helvetica,Arial,sans-serif
    this.ctx.font = `${fontWeight} ${fontSize}px ${options?.fontFamily}`;

    this.ctx.fillStyle = color;
    this.ctx.textBaseline = textBaseline;
    this.ctx.textAlign = textAlign;
    this.ctx.direction = direction;
    boundingBox = this.ctx.measureText(str);

    if (!onlyMeasure) {
      this.ctx.fillText(str, left, top);
    }
    this.ctx.restore();

    return boundingBox;
  }

  private hasReferral(options: DrawOptions): boolean {
    return typeof options.data?.referral !== "undefined";
  }

  private _ratio(num: number) {
    return num * this.painter.ratio;
  }

  private canvasWidth() {
    return this._ratio(this.painter.width);
  }

  private textDirection(options: DrawOptions): CanvasDirection {
    return this.isRTL(options) ? "rtl" : "ltr";
  }

  private isRTL(options: DrawOptions): boolean {
    return options.direction === "rtl";
  }

  private inlineStart(options: DrawOptions, value: number): number {
    return this.isRTL(options)
      ? this.canvasWidth() - this._ratio(value)
      : this._ratio(value);
  }

  private inlineEnd(options: DrawOptions, value: number): number {
    return this.isRTL(options)
      ? this._ratio(value)
      : this.canvasWidth() - this._ratio(value);
  }

  private inlineStartAlign(options: DrawOptions): CanvasTextAlign {
    return this.isRTL(options) ? "right" : "left";
  }

  private inlineEndAlign(options: DrawOptions): CanvasTextAlign {
    return this.isRTL(options) ? "left" : "right";
  }

  private advanceCursor(
    options: DrawOptions,
    cursor: number,
    width: number,
  ): number {
    return this.isRTL(options) ? cursor - width : cursor + width;
  }

  private drawInlineSeparator(
    options: DrawOptions,
    cursor: number,
    top: number,
    fontSize: number,
  ): number {
    const gap = this._ratio(this.ITEM_GAP);
    const separatorMetrics = this._drawText("|", {
      color: "rgba(255,255,255,0.2)",
      left: this.advanceCursor(options, cursor, gap),
      top,
      fontSize,
      fontFamily: options.fontFamily,
      textAlign: this.inlineStartAlign(options),
      direction: this.textDirection(options),
    });
    return this.advanceCursor(
      options,
      this.advanceCursor(options, cursor, gap),
      (separatorMetrics.width ?? 0) + gap,
    );
  }

  private drawInlineItem(
    options: DrawOptions,
    item: {
      kind: "text" | "badge";
      text: string;
      color?: string;
      fontSize: number;
      fontWeight?: number;
    },
    cursor: number,
    top: number,
  ): number {
    if (item.kind === "text") {
      const metrics = this._drawText(item.text, {
        color: item.color,
        left: cursor,
        top,
        fontSize: item.fontSize,
        fontWeight: item.fontWeight,
        fontFamily: options.fontFamily,
        textAlign: this.inlineStartAlign(options),
        direction: this.textDirection(options),
      });
      return this.advanceCursor(options, cursor, metrics.width ?? 0);
    }

    const badgeHeight = this._ratio(this.BROKER_BADGE_HEIGHT);
    const badgePaddingX = this._ratio(this.BROKER_BADGE_PADDING_X);
    const badgeRadius = this._ratio(this.BROKER_BADGE_RADIUS);
    const textMetrics = this._drawText(
      item.text,
      {
        left: 0,
        top: 0,
        fontSize: item.fontSize,
        fontWeight: item.fontWeight ?? 600,
        direction: this.textDirection(options),
      },
      true,
    );
    const badgeWidth = (textMetrics.width ?? 0) + badgePaddingX * 2;
    const badgeTop = top - badgeHeight / 2;
    const badgeLeft = this.isRTL(options) ? cursor - badgeWidth : cursor;
    this._fillRoundedRect(
      badgeLeft,
      badgeTop,
      badgeWidth,
      badgeHeight,
      badgeRadius,
      "rgba(255,255,255,0.06)",
    );
    this._drawText(item.text, {
      color: "rgba(255,255,255,0.36)",
      left: this.isRTL(options)
        ? badgeLeft + badgeWidth - badgePaddingX
        : badgeLeft + badgePaddingX,
      top: badgeTop + badgeHeight / 2,
      fontSize: item.fontSize,
      fontWeight: item.fontWeight ?? 600,
      textBaseline: "middle",
      textAlign: this.inlineStartAlign(options),
      direction: this.textDirection(options),
    });
    return this.advanceCursor(options, cursor, badgeWidth);
  }
}
