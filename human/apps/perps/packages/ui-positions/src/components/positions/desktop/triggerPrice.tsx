import { FC, useMemo } from "react";
import {
  ERROR_MSG_CODES,
  OrderValidationResult,
  useSymbolsInfo,
  useTpslPriceChecker,
  utils,
} from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import {
  AlgoOrderType,
  API,
  OrderSide,
  PositionType,
} from "@paxeer-network/types";
import {
  cn,
  ExclamationFillIcon,
  Flex,
  Text,
  Tooltip,
} from "@paxeer-network/ui";
import { CloseToLiqPriceIcon } from "@paxeer-network/ui-tpsl";
import {
  getTPSLLeg,
  getTPSLEstimatePrice,
  getTPSLQuantity,
  matchesTPSLPosition,
} from "@paxeer-network/utils";
import { formatNum } from "@paxeer-network/utils";
import { usePositionsRowContext } from "../positionsRowContext";
import { TPSLEditIcon, AddIcon } from "./components";

export const TriggerPrice: FC<{
  stopLossPrice?: number;
  takeProfitPrice?: number;
}> = (props) => {
  const { stopLossPrice, takeProfitPrice } = props;
  const { tpslOrder, position } = usePositionsRowContext();
  const side = position.position_qty > 0 ? OrderSide.BUY : OrderSide.SELL;
  const slPriceError = useTpslPriceChecker({
    slPrice: stopLossPrice?.toString() ?? undefined,
    liqPrice: position.est_liq_price ?? null,
    side: side,
  });

  return (
    <TPSLTriggerPrice
      stopLossPrice={stopLossPrice}
      takeProfitPrice={takeProfitPrice}
      direction={"column"}
      order={tpslOrder}
      position={position}
      slPriceError={slPriceError}
      tooltip
    />
  );
};

export const TriggerPriceItem: FC<{
  qty?: number;
  price?: number;
  entryPrice: number;
  orderSide: OrderSide;
  orderType: AlgoOrderType;
  symbolInfo: API.SymbolExt;
}> = (props) => {
  const { qty, price, entryPrice, orderSide, orderType, symbolInfo } = props;
  if (qty == null || price == null) return <span>--</span>;
  const pnl = formatNum
    .pnl(
      utils.priceToPnl(
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
      ),
    )!
    .toNumber();

  const type = orderType === AlgoOrderType.TAKE_PROFIT ? "TP" : "SL";

  return (
    <Flex>
      <Text intensity={54} className="oui-me-1">{`${type} PnL:`}</Text>
      <Text.formatted
        rule="price"
        className={
          pnl === 0
            ? "oui-text-base-contrast-36"
            : pnl > 0
              ? "oui-text-trade-profit oui-gap-0"
              : "oui-text-trade-loss oui-gap-0"
        }
        prefix={<Text>{pnl === 0 ? "" : pnl > 0 ? "+" : "-"}</Text>}
        suffix={
          <Text intensity={36} className="oui-ms-1">
            USDC
          </Text>
        }
      >{`${Math.abs(pnl)}`}</Text.formatted>
    </Flex>
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

    if (!order?.symbol) return <AddIcon positionType={PositionType.FULL} />;

    const Label = ({ children }: { children?: string }) => {
      return !props.stopLossPrice || direction === "column" ? (
        <Text intensity={54}>{children}</Text>
      ) : (
        ""
      );
    };

    if (props.takeProfitPrice) {
      children.push(
        <Text.formatted
          className={cn(
            "oui-text-trade-profit oui-gap-0 oui-decoration-white/20",
          )}
          key={"tp"}
          rule="price"
          dp={symbolInfo[order!.symbol]("quote_dp", 2)}
          children={props.takeProfitPrice}
          prefix={
            !props.stopLossPrice || direction === "column" ? (
              <Text intensity={54}>{`${t("tpsl.tp")} - `}</Text>
            ) : (
              ""
            )
          }
        />,
      );
    }
    if (props.stopLossPrice) {
      children.push(
        <Text.formatted
          key={"sl"}
          className={cn(
            "oui-text-trade-loss oui-gap-0 oui-decoration-white/20 ",
          )}
          rule={"price"}
          dp={symbolInfo[order!.symbol]("quote_dp", 2)}
          children={props.stopLossPrice}
          prefix={
            !props.takeProfitPrice || direction === "column" ? (
              <Text intensity={54}>{`${t("tpsl.sl")} - `}</Text>
            ) : (
              ""
            )
          }
          suffix={<CloseToLiqPriceIcon slPriceError={props.slPriceError} />}
        />,
      );
    }

    if (children.length === 0)
      return <AddIcon positionType={PositionType.FULL} />;

    if (children.length === 2 && direction === "row") {
      children.splice(1, 0, <Text key={"split"}>/</Text>);
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

  const showIcon = Array.isArray(child) ? !!child.length : !child;

  if (props.tooltip) {
    return (
      <Flex gapX={2}>
        <Tooltip content={pnl}>{content}</Tooltip>
        {showIcon && <TPSLEditIcon />}
      </Flex>
    );
  }

  return content;
};
