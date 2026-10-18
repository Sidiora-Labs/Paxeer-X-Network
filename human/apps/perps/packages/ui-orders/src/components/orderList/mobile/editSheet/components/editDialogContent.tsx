import { FC } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { OrderSide } from "@paxeer-network/types";
import { Badge, Checkbox, Divider, Flex, Text } from "@paxeer-network/ui";
import { Decimal } from "@paxeer-network/utils";
import { parseBadgesFor } from "../../../../../utils/util";
import { SymbolBadge } from "../../../../symbolBadge";
import { EditSheetState } from "../editSheet.script";

export const ConfirmDialogContent: FC<EditSheetState> = (props) => {
  const { item } = props;
  const {
    formattedOrder,
    symbolInfo,
    isStopMarket,
    showTriggerPrice,
    isTrailingStop,
  } = props;
  const { t } = useTranslation();

  const showPrice = !isTrailingStop;
  const showActivatedPrice = isTrailingStop && formattedOrder.activated_price;
  const showTrailingCallback = isTrailingStop;

  const header = (
    <>
      <Text
        intensity={80}
      >{`You agree to edit your ${props.base}-PERP order.`}</Text>
      <Flex gap={2} mb={3} mt={2} justify={"between"}>
        <Flex direction="column" itemAlign="start" gap={1} grow>
          <Text.formatted
            rule="symbol"
            formatString="base"
            size="base"
            showIcon
          >
            {item.symbol}
          </Text.formatted>
          <SymbolBadge symbol={item.symbol} />
        </Flex>
        <Flex direction={"row"} gap={1}>
          {parseBadgesFor(item)?.map((e, index) => (
            <Badge
              key={index}
              color={
                e.toLocaleLowerCase() === "position" ? "primary" : "neutral"
              }
              size="xs"
            >
              {e}
            </Badge>
          ))}
          {item.side === OrderSide.BUY ? (
            <Badge color="success" size="xs">
              {t("common.buy")}
            </Badge>
          ) : (
            <Badge color="danger" size="xs">
              {t("common.sell")}
            </Badge>
          )}
        </Flex>
      </Flex>
    </>
  );

  const triggerPriceItem = showTriggerPrice && (
    <Flex justify={"between"} width={"100%"} gap={1}>
      <Text>{t("common.triggerPrice")}</Text>
      <Text.numeral
        intensity={80}
        dp={symbolInfo.quote_dp}
        padding={false}
        rm={Decimal.ROUND_DOWN}
        suffix={<Text intensity={54}>{" USDC"}</Text>}
      >
        {formattedOrder.trigger_price ?? "--"}
      </Text.numeral>
    </Flex>
  );

  const priceItem = showPrice && (
    <Flex justify={"between"} width={"100%"} gap={1}>
      <Text>{t("common.price")}</Text>
      <Text.numeral
        intensity={80}
        dp={symbolInfo.quote_dp}
        padding={false}
        rm={Decimal.ROUND_DOWN}
        suffix={<Text intensity={54}>{" USDC"}</Text>}
        placeholder={isStopMarket ? t("common.marketPrice") : "--"}
      >
        {isStopMarket
          ? t("common.marketPrice")
          : (formattedOrder.order_price ?? "--")}
      </Text.numeral>
    </Flex>
  );

  const activatedPriceItem = showActivatedPrice && (
    <Flex justify={"between"} width={"100%"} gap={1}>
      <Text>{t("common.triggerPrice")}</Text>
      {formattedOrder.activated_price ? (
        <Text.numeral
          intensity={80}
          dp={symbolInfo.quote_dp}
          padding={false}
          rm={Decimal.ROUND_DOWN}
          suffix={<Text intensity={54}>{" USDC"}</Text>}
        >
          {formattedOrder.activated_price}
        </Text.numeral>
      ) : (
        t("common.marketPrice")
      )}
    </Flex>
  );

  const trailingCallbackItem = showTrailingCallback && (
    <>
      {formattedOrder.callback_value && (
        <Flex justify={"between"} width={"100%"} gap={1}>
          <Text>{t("orderEntry.trailingValue")}</Text>
          <Text.numeral
            dp={symbolInfo.quote_dp}
            padding={false}
            rm={Decimal.ROUND_DOWN}
          >
            {formattedOrder.callback_value}
          </Text.numeral>
        </Flex>
      )}
      {formattedOrder.callback_rate && (
        <Flex justify={"between"} width={"100%"} gap={1}>
          <Text>{t("orderEntry.trailingRate")}</Text>
          <Text className="oui-text-base-contrast">
            {formattedOrder.callback_rate}%
          </Text>
        </Flex>
      )}
    </>
  );

  const quantityItem = (
    <Flex justify={"between"} width={"100%"} gap={1}>
      <Text>{t("common.qty")}</Text>
      <Text.numeral
        color={item.side === OrderSide.BUY ? "buy" : "sell"}
        dp={symbolInfo.base_dp}
        padding={false}
        rm={Decimal.ROUND_DOWN}
      >
        {formattedOrder.order_quantity}
      </Text.numeral>
    </Flex>
  );

  const orderConfirmCheckbox = (
    <Flex className="oui-gap-[2px]">
      <Checkbox
        color="white"
        id="oui-checkbox-disableOrderConfirmation"
        checked={!props.orderConfirm}
        onCheckedChange={(e: boolean) => {
          props.setOrderConfirm(!e);
        }}
      />
      <label
        className="oui-text-2xs oui-text-base-contrast-54"
        htmlFor="oui-checkbox-disableOrderConfirmation"
      >
        {t("orderEntry.disableOrderConfirm")}
      </label>
    </Flex>
  );

  return (
    <div className="oui-pt-2">
      {header}
      <Divider />
      <Flex
        direction={"column"}
        gap={1}
        width={"100%"}
        className="oui-text-sm oui-text-base-contrast-54"
        py={3}
      >
        {triggerPriceItem}
        {priceItem}
        {quantityItem}
        {activatedPriceItem}
        {trailingCallbackItem}
      </Flex>

      {orderConfirmCheckbox}
    </div>
  );
};
