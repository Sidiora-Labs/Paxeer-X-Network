import {
  ComputedAlgoOrder,
  useLocalStorage,
  useSymbolWithBroker,
} from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { OrderSide, PositionType } from "@paxeer-network/types";
import {
  Badge,
  Box,
  Checkbox,
  cn,
  Divider,
  Flex,
  Text,
  textVariants,
} from "@paxeer-network/ui";
import { SymbolBadge } from "../components/symbolBadge";

export type PositionTPSLConfirmProps = {
  symbol: string;
  qty: number;
  tpPrice?: number;
  slPrice?: number;
  maxQty: number;
  side: OrderSide;
  // symbolConfig:API.SymbolExt
  baseDP: number;
  quoteDP: number;
  isEditing?: boolean;
  isPositionTPSL?: boolean;
  orderInfo: ComputedAlgoOrder;
};

const TPSLOrderType = (props: { tpPrice?: number; slPrice?: number }) => {
  const { tpPrice, slPrice } = props;
  const { t } = useTranslation();

  if (!!tpPrice && !!slPrice) {
    return (
      <Badge size="xs" color="neutral">
        {t("common.tpsl")}
      </Badge>
    );
  }

  if (!!tpPrice) {
    return (
      <Badge size="xs" color="neutral">
        {t("tpsl.tp")}
      </Badge>
    );
  }

  if (!!slPrice) {
    return (
      <Badge size="xs" color="neutral">
        {t("tpsl.sl")}
      </Badge>
    );
  }

  return null;
};

// ------------ Position TP/SL Confirm dialog start------------
export const PositionTPSLConfirm = (props: PositionTPSLConfirmProps) => {
  const {
    symbol,
    tpPrice,
    slPrice,
    qty,
    maxQty,
    side,
    quoteDP,
    baseDP,
    isEditing,
    isPositionTPSL: _isPositionTPSL,
    orderInfo: order,
  } = props;
  const { t } = useTranslation();

  const [needConfirm, setNeedConfirm] = useLocalStorage(
    "paxeerNetwork_order_confirm",
    true,
  );
  const renderPositionType = () => {
    if (order.position_type === PositionType.FULL) {
      return <Text>{t("tpsl.positionType.full")}</Text>;
    }
    return <Text>{t("tpsl.positionType.partial")}</Text>;
  };
  // console.log("PositionTPSLConfirm", qty, maxQty, quoteDP);

  const renderTPSLPrice = ({
    price,
    isOrderPrice,
    isEnable,
    colorType,
  }: {
    price: string | number;
    isOrderPrice?: boolean;
    isEnable?: boolean;
    colorType: "TP" | "SL";
  }) => {
    if (!isEnable) {
      return <Text className="oui-text-base-contrast-36">-- USDC</Text>;
    }
    if (!price) {
      if (isOrderPrice) {
        return (
          <Text className="oui-text-base-contrast-36">
            {t("common.market")}
          </Text>
        );
      }
    }
    return (
      <Text.numeral
        unit={"USDC"}
        rule={"price"}
        className={cn(
          "oui-text-base-contrast",
          colorType === "TP" ? "oui-text-trade-profit" : "oui-text-trade-loss",
        )}
        unitClassName={"oui-text-base-contrast-36 oui-ms-1"}
        dp={quoteDP}
        padding={false}
      >
        {price}
      </Text.numeral>
    );
  };

  const isPositionTPSL = _isPositionTPSL;

  const displaySymbol = useSymbolWithBroker(symbol);
  return (
    <>
      {isEditing && (
        <Text as="div" size="2xs" intensity={80} className="oui-mb-3">
          {t("tpsl.agreement", { symbol: displaySymbol })}
        </Text>
      )}

      <Flex pb={4}>
        <Flex direction="column" itemAlign="start" gap={1} grow>
          <Box grow>
            <Text.formatted
              rule={"symbol"}
              formatString="base"
              size="base"
              showIcon
              as="div"
              intensity={80}
            >
              {symbol}
            </Text.formatted>
          </Box>
          <SymbolBadge symbol={symbol} />
        </Flex>
        <Flex gap={1}>
          {isPositionTPSL && (
            <Badge size="xs" color={"primary"}>
              {t("common.position")}
            </Badge>
          )}

          {/* <Badge size="xs" color="neutral">
            TP/SL
          </Badge> */}
          <TPSLOrderType tpPrice={tpPrice} slPrice={slPrice} />
          {side === OrderSide.SELL ? (
            <Badge size="xs" color="success">
              {t("common.buy")}
            </Badge>
          ) : (
            <Badge size="xs" color="danger">
              {t("common.sell")}
            </Badge>
          )}
        </Flex>
      </Flex>
      <Divider />
      {order.tp_trigger_price || order.sl_trigger_price ? (
        <>
          <Divider className="oui-my-4" />
          <div
            className={textVariants({
              size: "sm",
              intensity: 54,
              className:
                "oui-space-y-1 oui-w-full oui-flex oui-flex-col oui-gap-3",
            })}
          >
            <Text className="oui-text-base-contrast">
              {renderPositionType()}
            </Text>
            <Flex justify={"between"}>
              <Text>{t("common.orderQty")}</Text>
              <Text.numeral
                placeholder={
                  order.position_type === PositionType.FULL
                    ? t("tpsl.entirePosition")
                    : "--"
                }
                rule={"price"}
                dp={baseDP}
                padding={false}
                className="oui-text-base-contrast"
              >
                {order.position_type === PositionType.FULL
                  ? t("tpsl.entirePosition")
                  : (order.quantity ?? "--")}
              </Text.numeral>
            </Flex>

            <Flex
              direction={"column"}
              justify={"between"}
              itemAlign={"start"}
              gap={1}
              className="oui-w-full"
            >
              <Flex justify={"between"} className="oui-w-full">
                <Text>{t("tpsl.tpTriggerPrice")}</Text>{" "}
                {renderTPSLPrice({
                  price: order.tp_trigger_price ?? "",
                  isOrderPrice: false,
                  isEnable: !!order.tp_trigger_price,
                  colorType: "TP",
                })}
              </Flex>
              <Flex justify={"between"} className="oui-w-full">
                <Text>{t("tpsl.tpOrderPrice")}</Text>
                {renderTPSLPrice({
                  price: order.tp_order_price ?? "",
                  isOrderPrice: true,
                  isEnable: !!order.tp_trigger_price,
                  colorType: "TP",
                })}
              </Flex>
            </Flex>

            <Flex
              direction={"column"}
              justify={"between"}
              itemAlign={"start"}
              gap={1}
              className="oui-w-full"
            >
              <Flex justify={"between"} className="oui-w-full">
                <Text>{t("tpsl.slTriggerPrice")}</Text>
                {renderTPSLPrice({
                  price: order.sl_trigger_price ?? "",
                  isOrderPrice: false,
                  isEnable: !!order.sl_trigger_price,
                  colorType: "SL",
                })}
              </Flex>
              <Flex justify={"between"} className="oui-w-full">
                <Text>{t("tpsl.slOrderPrice")}</Text>
                {renderTPSLPrice({
                  price: order.sl_order_price ?? "",
                  isOrderPrice: true,
                  isEnable: !!order.sl_trigger_price,
                  colorType: "SL",
                })}
              </Flex>
            </Flex>
          </div>
        </>
      ) : null}
      <Box pt={2}>
        <Flex gap={1}>
          <Checkbox
            id="disabledConfirm"
            color="white"
            checked={!needConfirm}
            onCheckedChange={(check) => {
              setNeedConfirm(!check);
            }}
          />
          <label
            htmlFor="disabledConfirm"
            className={textVariants({
              size: "xs",
              intensity: 54,
              className: "oui-ms-1",
            })}
          >
            {t("orderEntry.disableOrderConfirm")}
          </label>
        </Flex>
      </Box>
    </>
  );
};

//------------- Position TP/SL Confirm dialog end------------
