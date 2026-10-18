import { findTPSLOrderPriceFromOrder } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { API, OrderType } from "@paxeer-network/types";
import { Flex, Text } from "@paxeer-network/ui";
import { Decimal } from "@paxeer-network/utils";
import { FlexCell } from "../components/common";
import { useTPSLDetailContext } from "../tpslDetailProvider";

export const OrderPriceRender = ({ order }: { order: API.AlgoOrder }) => {
  const { quote_dp } = useTPSLDetailContext();
  const { tp_order_price, sl_order_price } = findTPSLOrderPriceFromOrder(order);
  const { t } = useTranslation();
  return (
    <Flex
      gap={2}
      direction={"column"}
      justify={"between"}
      itemAlign={"start"}
      className="oui-text-2xs"
    >
      {tp_order_price && (
        <FlexCell>
          {tp_order_price === OrderType.MARKET ? (
            <Text>{t("common.market")}</Text>
          ) : (
            <Text.numeral dp={quote_dp} rm={Decimal.ROUND_DOWN} padding={false}>
              {tp_order_price}
            </Text.numeral>
          )}
        </FlexCell>
      )}
      {sl_order_price && (
        <FlexCell>
          {sl_order_price === OrderType.MARKET ? (
            <Text>{t("common.market")}</Text>
          ) : (
            <Text.numeral dp={quote_dp} rm={Decimal.ROUND_DOWN} padding={false}>
              {sl_order_price}
            </Text.numeral>
          )}
        </FlexCell>
      )}
    </Flex>
  );
};
