import { FC, useMemo } from "react";
import {
  ERROR_MSG_CODES,
  useSymbolsInfo,
  useTpslPriceChecker,
} from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { OrderSide, PositionType } from "@paxeer-network/types";
import { cn, ExclamationFillIcon, Flex, Text } from "@paxeer-network/ui";
import { CloseToLiqPriceIcon } from "@paxeer-network/ui-tpsl";
import { usePositionsRowContext } from "../positionsRowContext";
import { AddIcon, TPSLEditIcon } from "./components";

export const PartialTPSL: FC<{
  orderNum?: number;
  tpTriggerPrice?: number;
  slTriggerPrice?: number;
  direction?: "column" | "row";
}> = (props) => {
  const {
    orderNum,
    tpTriggerPrice,
    slTriggerPrice,
    direction = "column",
  } = props;
  const {
    partialTPSLOrder: order,
    quoteDp,
    baseDp,
    position,
  } = usePositionsRowContext();
  const symbolInfo = useSymbolsInfo();
  const { t } = useTranslation();
  const side = position.position_qty > 0 ? OrderSide.BUY : OrderSide.SELL;
  const slPriceError = useTpslPriceChecker({
    slPrice: slTriggerPrice?.toString() ?? undefined,
    liqPrice: position.est_liq_price ?? null,
    side: side,
  });

  const child = useMemo(() => {
    const children = [];

    if (!order?.symbol) return <AddIcon positionType={PositionType.PARTIAL} />;

    if (tpTriggerPrice) {
      children.push(
        <Text.formatted
          className={cn(
            "oui-text-trade-profit oui-gap-0 oui-decoration-white/20",
          )}
          key={"tp"}
          rule="price"
          dp={symbolInfo[order!.symbol]("quote_dp", 2)}
          children={tpTriggerPrice}
          prefix={
            !slTriggerPrice || direction === "column" ? (
              <Text intensity={54}>{`${t("tpsl.tp")} - `}</Text>
            ) : (
              ""
            )
          }
        />,
      );
    }
    if (slTriggerPrice) {
      children.push(
        <Text.formatted
          key={"sl"}
          className={cn(
            "oui-text-trade-loss oui-gap-0 oui-decoration-white/20 ",
          )}
          rule={"price"}
          dp={symbolInfo[order!.symbol]("quote_dp", 2)}
          prefix={<Text intensity={54}>{`${t("tpsl.sl")} - `}</Text>}
          suffix={<CloseToLiqPriceIcon slPriceError={slPriceError} />}
        >
          {slTriggerPrice}
        </Text.formatted>,
      );
    }
    if (children.length === 0)
      return <AddIcon positionType={PositionType.PARTIAL} />;

    if (children.length === 2 && direction === "row") {
      children.splice(1, 0, <Text key={"split"}>/</Text>);
    }

    return children;
  }, [tpTriggerPrice, slTriggerPrice, order?.symbol, t, slPriceError]);
  const hasTPSL = Array.isArray(child) ? !!child.length : !child;
  return (
    <Flex className="oui-gap-[6px]">
      <div
        className={cn(
          "oui-inline-flex oui-text-base-contrast-36",
          direction === "column" ? "oui-flex-col" : "oui-flex-row oui-gap-1",
        )}
      >
        {child}
      </div>
      {hasTPSL && (
        <>
          <Text className="oui-text-base-contrast-54">({orderNum})</Text>
          <TPSLEditIcon />
        </>
      )}
    </Flex>
  );
};
