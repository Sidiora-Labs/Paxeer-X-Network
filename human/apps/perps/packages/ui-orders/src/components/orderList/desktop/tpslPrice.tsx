import {
  findTPSLOrderPriceFromOrder,
  findTPSLFromOrder,
  useSymbolsInfo,
} from "@paxeer-network/hooks";
import { i18n, useTranslation } from "@paxeer-network/i18n";
import { OrderType } from "@paxeer-network/types";
import { Text } from "@paxeer-network/ui";
import { useTPSLOrderRowContext } from "../tpslOrderRowContext";

export const TPSLOrderPrice = () => {
  const {
    sl_trigger_price,
    tp_trigger_price,
    sl_order_price,
    tp_order_price,
    order,
  } = useTPSLOrderRowContext();
  const { t } = useTranslation();
  const symbolInfo = useSymbolsInfo()[order.symbol]();
  return (
    <div>
      {!!tp_trigger_price ? (
        <div className={"oui-text-base-contrast-80 oui-td-bg-transparent"}>
          <Text intensity={54}>{`${t("tpsl.tp")} -`}&nbsp;</Text>
          <TPSLOrderPriceItem
            price={tp_order_price}
            quoteDP={symbolInfo.quote_dp}
          />
        </div>
      ) : null}
      {!!sl_trigger_price ? (
        <div className={"oui-text-base-contrast-80 oui-td-bg-transparent"}>
          <span className={"oui-text-base-contrast-54"}>
            {`${t("tpsl.sl")} -`}&nbsp;
          </span>
          <TPSLOrderPriceItem
            price={sl_order_price}
            quoteDP={symbolInfo.quote_dp}
          />
        </div>
      ) : null}
    </div>
  );
};

const TPSLOrderPriceItem = ({
  price,
  quoteDP,
}: {
  price?: number | OrderType;
  quoteDP: number;
}) => {
  const { t } = useTranslation();
  if (!price) {
    return null;
  }
  if (price === OrderType.MARKET) {
    return <span>{t("common.marketPrice")}</span>;
  }

  return (
    <Text.numeral
      className="oui-text-base-contrast-80"
      rule={"price"}
      dp={quoteDP}
    >
      {price}
    </Text.numeral>
  );
};

export function getTPSLOrderPrice(order: any) {
  // @ts-ignore
  const { sl_trigger_price, tp_trigger_price } =
    !("algo_type" in order) || !Array.isArray(order.child_orders)
      ? {}
      : findTPSLFromOrder(order);

  const prices = findTPSLOrderPriceFromOrder(order);
  const displayPrice = (price: number | OrderType | undefined) =>
    price === OrderType.MARKET ? i18n.t("common.marketPrice") : (price ?? "--");
  const tpTriggerPrice = tp_trigger_price
    ? `${i18n.t("tpsl.tp")} - ${displayPrice(prices.tp_order_price)}`
    : undefined;
  const slTriggerPrice = sl_trigger_price
    ? `${i18n.t("tpsl.sl")} - ${displayPrice(prices.sl_order_price)}`
    : undefined;

  return { tpTriggerPrice, slTriggerPrice };
}
