import { useEffect, useState, SVGProps } from "react";
import { ERROR_MSG_CODES, useGetEstLiqPrice } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { useOrderEntryFormErrorMsg } from "@paxeer-network/react-app";
import {
  PaxeerNetworkOrder,
  OrderSide,
  OrderType,
  PositionType,
} from "@paxeer-network/types";
import {
  Button,
  cn,
  Divider,
  DotStatus,
  Flex,
  ScrollArea,
  Text,
} from "@paxeer-network/ui";
import { CloseToLiqPriceIcon } from "../components/closeLiqPriceIcon";
import { OrderInfo } from "../components/orderInfo";
import { PnlInfo } from "../components/pnlInfo";
import { TPSLInputRowWidget } from "../components/tpslInputRow";
import { TPSLPositionTypeWidget } from "../components/tpslPostionType";
import { useTPSLAdvanced } from "./useTPSLAdvanced.script";

type Props = ReturnType<typeof useTPSLAdvanced>;

export const TPSLAdvancedUI = (props: Props) => {
  const { t } = useTranslation();
  const { errors, validated } = props.metaState;

  const { getErrorMsg } = useOrderEntryFormErrorMsg(props.slPriceError);
  const isSlPriceWarning =
    props.slPriceError?.sl_trigger_price?.type ===
    ERROR_MSG_CODES.SL_PRICE_WARNING;

  const isSlPriceError =
    props.slPriceError?.sl_trigger_price?.type ===
    ERROR_MSG_CODES.SL_PRICE_ERROR;

  const displayEstLiqPrice = useGetEstLiqPrice({
    estLiqPrice: props.estLiqPrice,
    symbol: props.symbolInfo.symbol,
    side: props.formattedOrder.side as OrderSide,
  });

  const {
    formattedOrder,
    setValue: setOrderValue,
    symbolInfo,
    setValues,
  } = props;
  const [tpValues, setTpValuse] = useState<{
    enable: boolean;
    trigger_price: string;
    PnL: string;
    Offset: string;
    "Offset%": string;
    OffsetFromMark: string;
    PercentageFromMark: string;
    ROI: string;
    order_price: string;
    order_type: OrderType;
  }>({
    enable: false,
    order_type: formattedOrder.tp_order_type ?? OrderType.MARKET,
    order_price: formattedOrder.tp_order_price ?? "",
    trigger_price: formattedOrder.tp_trigger_price ?? "",
    PnL: formattedOrder.tp_pnl ?? "",
    Offset: formattedOrder.tp_offset ?? "",
    "Offset%": formattedOrder.tp_offset_percentage ?? "",
    OffsetFromMark: formattedOrder.tp_offset_from_mark ?? "",
    PercentageFromMark: formattedOrder.tp_offset_percentage_from_mark ?? "",
    ROI: formattedOrder.tp_ROI ?? "",
  });

  const [slValues, setSlValues] = useState<{
    enable: boolean;
    trigger_price: string;
    PnL: string;
    Offset: string;
    "Offset%": string;
    OffsetFromMark: string;
    PercentageFromMark: string;
    ROI: string;
    order_price: string;
    order_type: OrderType;
  }>({
    enable: false,
    order_type: formattedOrder.sl_order_type ?? OrderType.MARKET,
    order_price: formattedOrder.sl_order_price ?? "",
    trigger_price: formattedOrder.sl_trigger_price ?? "",
    PnL: formattedOrder.sl_pnl ?? "",
    Offset: formattedOrder.sl_offset ?? "",
    "Offset%": formattedOrder.sl_offset_percentage ?? "",
    OffsetFromMark: formattedOrder.sl_offset_from_mark ?? "",
    PercentageFromMark: formattedOrder.sl_offset_percentage_from_mark ?? "",
    ROI: formattedOrder.sl_ROI ?? "",
  });

  // Update tpValues when formattedOrder changes
  useEffect(() => {
    setTpValuse((prev) => ({
      ...prev,
      // enable: formattedOrder.tp_enable ?? false,
      order_type: formattedOrder.tp_order_type ?? OrderType.MARKET,
      order_price: formattedOrder.tp_order_price ?? "",
      trigger_price: formattedOrder.tp_trigger_price ?? "",
      PnL: formattedOrder.tp_pnl ?? "",
      Offset: formattedOrder.tp_offset ?? "",
      "Offset%": formattedOrder.tp_offset_percentage ?? "",
      OffsetFromMark: formattedOrder.tp_offset_from_mark ?? "",
      PercentageFromMark: formattedOrder.tp_offset_percentage_from_mark ?? "",
      ROI: formattedOrder.tp_ROI ?? "",
    }));
  }, [formattedOrder]);

  useEffect(() => {
    setSlValues((prev) => ({
      ...prev,
      // enable: formattedOrder.sl_enable ?? false,
      order_type: formattedOrder.sl_order_type ?? OrderType.MARKET,
      order_price: formattedOrder.sl_order_price ?? "",
      trigger_price: formattedOrder.sl_trigger_price ?? "",
      PnL: formattedOrder.sl_pnl ?? "",
      Offset: formattedOrder.sl_offset ?? "",
      "Offset%": formattedOrder.sl_offset_percentage ?? "",
      OffsetFromMark: formattedOrder.sl_offset_from_mark ?? "",
      PercentageFromMark: formattedOrder.sl_offset_percentage_from_mark ?? "",
      ROI: formattedOrder.sl_ROI ?? "",
    }));
  }, [formattedOrder]);

  const tpslEnable =
    formattedOrder.tp_trigger_price || formattedOrder.sl_trigger_price;

  return (
    <div className="oui-flex oui-h-full oui-flex-col oui-justify-between oui-rounded-[16px] oui-py-3">
      <div className="oui-px-3">
        <Flex
          className="oui-mb-5 oui-cursor-pointer  oui-text-base oui-text-base-contrast-80"
          gap={1}
          itemAlign={"center"}
          onClick={props.onClose}
        >
          <ArrowRightIcon className=" oui-text-base-contrast-80" />
          <Text>{t("common.tpsl")}</Text>
        </Flex>
      </div>
      <ScrollArea className="oui-flex-1">
        <div className="oui-px-3">
          <OrderInfo
            order={formattedOrder as PaxeerNetworkOrder}
            baseDP={symbolInfo.base_dp}
            quoteDP={symbolInfo.quote_dp}
            estLiqPrice={displayEstLiqPrice ?? undefined}
            symbolLeverage={props.symbolLeverage}
          />
        </div>
        <Divider className="oui-my-3" />
        <div className="oui-px-3">
          <Flex className="oui-gap-[6px]">
            <Button
              onClick={() => {
                setOrderValue("side", OrderSide.BUY);
              }}
              size={"sm"}
              fullWidth
              data-type={OrderSide.BUY}
              // color={side === OrderSide.BUY ? "buy" : "secondary"}
              className={cn(
                formattedOrder.side === OrderSide.BUY
                  ? "oui-bg-success-darken hover:oui-bg-success-darken/80 active:oui-bg-success-darken/80"
                  : "oui-bg-base-7 oui-text-base-contrast-36 hover:oui-bg-base-6 active:oui-bg-base-6",
              )}
              data-testid="oui-testid-orderEntry-side-buy-button"
            >
              {t("common.buy")}
            </Button>
            <Button
              onClick={() => {
                setOrderValue("side", OrderSide.SELL);
              }}
              data-type={OrderSide.SELL}
              fullWidth
              size={"sm"}
              // color={side === OrderSide.SELL ? "sell" : "secondary"}
              className={cn(
                formattedOrder.side === OrderSide.SELL
                  ? "oui-bg-danger-darken hover:oui-bg-danger-darken/80 active:oui-bg-danger-darken/80"
                  : "oui-bg-base-7 oui-text-base-contrast-36 hover:oui-bg-base-6 active:oui-bg-base-6",
              )}
              data-testid="oui-testid-orderEntry-side-sell-button"
            >
              {t("common.sell")}
            </Button>
          </Flex>
          <div className="oui-py-3">
            <TPSLPositionTypeWidget
              value={formattedOrder.position_type ?? PositionType.PARTIAL}
              onChange={(key, value) => {
                setOrderValue("position_type", value);
              }}
            />
          </div>
          <Flex direction={"column"} gap={6}>
            <TPSLInputRowWidget
              rootOrderPrice={formattedOrder.order_price}
              symbol={symbolInfo.symbol}
              type="tp"
              side={formattedOrder.side as OrderSide}
              values={tpValues}
              errors={validated ? errors : null}
              quote_dp={symbolInfo.quote_dp}
              onChange={(key, value) => {
                // setTpValuse((prev) => ({ ...prev, [key]: value }));
                setOrderValue(key as keyof PaxeerNetworkOrder, value);
              }}
              positionType={
                formattedOrder.position_type ?? PositionType.PARTIAL
              }
              symbolLeverage={props.symbolLeverage}
            />
            <TPSLInputRowWidget
              rootOrderPrice={formattedOrder.order_price}
              symbol={symbolInfo.symbol}
              type="sl"
              side={formattedOrder.side as OrderSide}
              values={slValues}
              errors={
                validated ? errors : isSlPriceError ? props.slPriceError : null
              }
              inputWarnNode={
                isSlPriceWarning && (
                  <DotStatus
                    color="warning"
                    size="xs"
                    label={getErrorMsg("sl_trigger_price")}
                  />
                )
              }
              quote_dp={symbolInfo.quote_dp}
              positionType={
                formattedOrder.position_type ?? PositionType.PARTIAL
              }
              onChange={(key, value) => {
                setOrderValue(key as keyof PaxeerNetworkOrder, value);
              }}
              symbolLeverage={props.symbolLeverage}
            />
          </Flex>

          <PnlInfo
            tp_pnl={formattedOrder.tp_pnl}
            sl_pnl={formattedOrder.sl_pnl}
            className="oui-mt-6"
          />
        </div>
      </ScrollArea>
      <Flex className="oui-mt-6 oui-px-3" gap={2}>
        <Button
          size="md"
          fullWidth
          color="gray"
          variant="outlined"
          className="oui-text-base-contrast-36"
          onClick={props.onClose}
        >
          {t("common.cancel")}
        </Button>
        <Button
          size="md"
          fullWidth
          color="success"
          className={cn(
            formattedOrder.side === OrderSide.SELL
              ? "oui-bg-danger-darken hover:oui-bg-danger-darken/80 active:oui-bg-danger-darken/80"
              : "oui-bg-success-darken hover:oui-bg-success-darken/80 active:oui-bg-success-darken/80",
          )}
          onClick={props.onSubmit}
          disabled={!tpslEnable}
        >
          {t("tpsl.advanced.submit")}
        </Button>
      </Flex>
    </div>
  );
};

export const ArrowRightIcon = (props: SVGProps<SVGSVGElement>) => {
  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      width="14"
      height="14"
      viewBox="0 0 14 14"
      fill="currentColor"
      {...props}
    >
      <path d="M8.03752 2.9294C7.89169 2.95857 7.74527 3.03207 7.65661 3.16624L5.33145 6.66624C5.20137 6.86224 5.20137 7.12648 5.33145 7.32248L7.65661 10.8225C7.83452 11.0902 8.20669 11.1655 8.47385 10.9864C8.74044 10.8079 8.8151 10.434 8.63719 10.1662L6.53019 6.99408L8.63719 3.82249C8.8151 3.55416 8.74044 3.18082 8.47385 3.00232C8.34027 2.91249 8.18335 2.89965 8.03752 2.9294Z" />
    </svg>
  );
};
