import { FC, useMemo } from "react";
import {
  OrderValidationResult,
  useSymbolsInfo,
  useTpslPriceChecker,
  utils,
} from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { API, AlgoOrderType } from "@paxeer-network/types";
import { OrderSide } from "@paxeer-network/types";
import { cn, Text, Tooltip } from "@paxeer-network/ui";
import { CloseToLiqPriceIcon } from "@paxeer-network/ui-tpsl";
import {
  getTPSLLeg,
  getTPSLEstimatePrice,
  getTPSLQuantity,
  matchesTPSLPosition,
} from "@paxeer-network/utils";
import { useTPSLOrderRowContext } from "../tpslOrderRowContext";

export const OrderTriggerPrice = () => {
  const { sl_trigger_price, tp_trigger_price, order, position } =
    useTPSLOrderRowContext();

  const positionSide =
    position?.position_qty && position.position_qty > 0
      ? OrderSide.BUY
      : OrderSide.SELL;
  const slPriceError = useTpslPriceChecker({
    slPrice: sl_trigger_price?.toString() ?? undefined,
    liqPrice: position?.est_liq_price ?? null,
    side: positionSide,
  });

  return (
    <TPSLTriggerPrice
      stopLossPrice={sl_trigger_price}
      takeProfitPrice={tp_trigger_price}
      direction={"column"}
      order={order}
      position={position}
      slPriceError={slPriceError}
      tooltip
    />
  );
};

export const TPSLTriggerPrice: FC<{
  takeProfitPrice: number | undefined;
  stopLossPrice: number | undefined;
  className?: string;
  direction?: "row" | "column";
  tooltip?: boolean;
  order?: API.AlgoOrder;
  position?: API.PositionTPSLExt;
  slPriceError: OrderValidationResult | null;
}> = (props) => {
  const { direction = "row", order, position } = props;
  // const symbolInfo = useSymbolsInfo()[position?.symbol ?? ""]();
  const symbolInfo = useSymbolsInfo();
  const { t } = useTranslation();

  const pnl = useMemo(() => {
    const msgs = [];

    if (
      !props.tooltip ||
      !order ||
      !position ||
      !matchesTPSLPosition(order, position)
    )
      return;

    const tp = getTPSLLeg(order, "tp");
    const sl = getTPSLLeg(order, "sl");
    const tpQty = tp ? getTPSLQuantity(tp, position.position_qty) : undefined;
    const slQty = sl ? getTPSLQuantity(sl, position.position_qty) : undefined;
    const tpPrice = tp ? getTPSLEstimatePrice(tp) : undefined;
    const slPrice = sl ? getTPSLEstimatePrice(sl) : undefined;

    if (!!props.takeProfitPrice) {
      msgs.push(
        <TriggerPriceItem
          key={"tp"}
          qty={
            tpQty == null ? undefined : tpQty * (tp?.side === "BUY" ? -1 : 1)
          }
          price={tpPrice}
          entryPrice={position.average_open_price}
          orderSide={order.side as OrderSide}
          orderType={AlgoOrderType.TAKE_PROFIT}
          symbolInfo={symbolInfo[order.symbol]()}
        />,
      );
    }

    if (!!props.stopLossPrice) {
      msgs.push(
        <TriggerPriceItem
          key={"sl"}
          qty={
            slQty == null ? undefined : slQty * (sl?.side === "BUY" ? -1 : 1)
          }
          price={slPrice}
          entryPrice={position.average_open_price}
          orderSide={order.side as OrderSide}
          orderType={AlgoOrderType.STOP_LOSS}
          symbolInfo={symbolInfo[order.symbol]()}
        />,
      );
    }

    return <div>{msgs}</div>;
  }, [
    props.takeProfitPrice,
    props.stopLossPrice,
    position,
    order,
    position?.average_open_price,
    order?.side,
    order?.quantity,
    order?.algo_type,
  ]);

  const child = useMemo(() => {
    const children = [];

    if (!order?.symbol) return <span>-</span>;
    if (props.takeProfitPrice) {
      children.push(
        <Text.numeral
          className={cn(
            "oui-text-trade-profit oui-gap-0  oui-decoration-white/20 oui-border-b oui-border-dashed oui-border-base-contrast-12",
          )}
          key={"tp"}
          rule="price"
          dp={symbolInfo[order!.symbol]("quote_dp", 2)}
          children={props.takeProfitPrice}
          // @ts-ignore
          prefix={
            !props.stopLossPrice || direction === "column" ? (
              <span className={"oui-text-base-contrast-54"}>
                {`${t("tpsl.tp")} -`}&nbsp;
              </span>
            ) : (
              ""
            )
          }
        />,
      );
    }
    if (props.stopLossPrice) {
      children.push(
        <Text.numeral
          key={"sl"}
          className={cn(
            "oui-text-trade-loss oui-gap-0 oui-decoration-white/20 oui-border-b oui-border-dashed oui-border-base-contrast-12",
            "oui-flex oui-items-center",
          )}
          as="div"
          rule={"price"}
          dp={symbolInfo[order!.symbol]("quote_dp", 2)}
          children={props.stopLossPrice}
          // @ts-ignore
          prefix={
            !props.takeProfitPrice || direction === "column" ? (
              <span className={"oui-text-base-contrast-54"}>
                {`${t("tpsl.sl")} -`}&nbsp;
              </span>
            ) : (
              ""
            )
          }
          suffix={
            <CloseToLiqPriceIcon
              slPriceError={props.slPriceError}
              className="oui-ms-1"
            />
          }
        />,
      );
    }

    if (children.length === 0) return <span>-</span>;

    if (children.length === 2 && direction === "row") {
      children.splice(1, 0, <span key={"split"}>/</span>);
    }

    return children;
  }, [
    props.takeProfitPrice,
    props.stopLossPrice,
    order?.symbol,
    t,
    props.slPriceError,
  ]);

  const content = (
    <div
      className={cn(
        "oui-inline-flex oui-text-base-contrast-36",
        props.direction === "column"
          ? "oui-flex-col"
          : "oui-flex-row oui-gap-1",
        props.className,
      )}
    >
      {child}
    </div>
  );

  if (props.tooltip) {
    // @ts-ignore
    return (
      <Tooltip
        // @ts-ignore
        content={pnl}
        className="oui-bg-base-5"
        arrow={{ className: "oui-fill-base-5" }}
      >
        {content}
      </Tooltip>
    );
  }

  return content;
};

const TriggerPriceItem: FC<{
  qty?: number;
  price?: number;
  entryPrice: number;
  orderSide: OrderSide;
  orderType: AlgoOrderType;
  symbolInfo: API.SymbolExt;
}> = (props) => {
  const { qty, price, entryPrice, orderSide, orderType, symbolInfo } = props;
  const { t } = useTranslation();

  if (qty == null || price == null) return <span>{t("tpsl.estPnl")}: --</span>;

  const pnl = utils.priceToPnl(
    {
      qty,
      price,
      entryPrice,
      orderSide,
      orderType,
    },
    {
      symbol: symbolInfo,
    },
  );

  const type = orderType === AlgoOrderType.TAKE_PROFIT ? "TP" : "SL";
  const label = type === "TP" ? `${t("tpsl.tpPnl")}:` : `${t("tpsl.slPnl")}:`;

  // console.log("trigger price item", "dp", symbolInfo.quote_dp);

  return (
    <div className="oui-flex oui-items-center">
      <span className="oui-text-base-contrast-54 oui-me-1">{label}</span>
      <Text.numeral
        rule="price"
        dp={symbolInfo.quote_dp}
        padding={false}
        className={
          pnl === 0
            ? "oui-text-base-contrast-36"
            : pnl > 0
              ? "oui-text-trade-profit oui-gap-0"
              : "oui-text-trade-loss oui-gap-0"
        }
        // @ts-ignore
        prefix={
          // @ts-ignore
          <span>{pnl === 0 ? "" : pnl > 0 ? "+" : "-"}</span>
        }
        suffix={
          <span className="oui-text-base-contrast-36 oui-ms-1">USDC</span>
        }
      >{`${Math.abs(pnl)}`}</Text.numeral>
    </div>
  );
};
