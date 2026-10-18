import {
  ERROR_MSG_CODES,
  findTPSLFromOrder,
  useTpslPriceChecker,
} from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { API } from "@paxeer-network/types";
import { ExclamationFillIcon, Flex, Text } from "@paxeer-network/ui";
import { CloseToLiqPriceIcon } from "../../components/closeLiqPriceIcon";
import { FlexCell } from "../components/common";
import { useTPSLDetailContext } from "../tpslDetailProvider";

export const TypeRender = ({ order }: { order: API.AlgoOrder }) => {
  const { side, estLiqPrice } = useTPSLDetailContext();
  const { tp_trigger_price, sl_trigger_price } = findTPSLFromOrder(order);
  const { t } = useTranslation();

  const slPriceError = useTpslPriceChecker({
    slPrice: sl_trigger_price?.toString() ?? undefined,
    liqPrice: estLiqPrice ?? null,
    side: side,
  });
  const isSlPriceWarning =
    slPriceError?.sl_trigger_price?.type === ERROR_MSG_CODES.SL_PRICE_WARNING;
  return (
    <Flex
      direction={"column"}
      justify={"between"}
      itemAlign={"start"}
      className="oui-text-2xs"
    >
      {tp_trigger_price && (
        <FlexCell>
          <Text className="oui-text-trade-profit">{t("tpsl.tp")}</Text>
        </FlexCell>
      )}

      {sl_trigger_price && (
        <FlexCell className={"oui-flex-row oui-items-center oui-gap-1"}>
          <Text className="oui-text-trade-loss">{t("tpsl.sl")}</Text>
          {isSlPriceWarning && (
            <CloseToLiqPriceIcon slPriceError={slPriceError} />
          )}
        </FlexCell>
      )}
    </Flex>
  );
};
