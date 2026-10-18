import { useEffect, useState } from "react";
import { ERROR_MSG_CODES } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { useOrderEntryFormErrorMsg } from "@paxeer-network/react-app";
import {
  PaxeerNetworkOrder,
  OrderSide,
  OrderType,
  PositionType,
} from "@paxeer-network/types";
import {
  cn,
  Divider,
  DotStatus,
  Flex,
  ScrollArea,
  ThrottledButton,
  useScreen,
} from "@paxeer-network/ui";
import { OrderInfo } from "../components/orderInfo";
import { PnlInfo } from "../components/pnlInfo";
import { TPSLInputRowWidget } from "../components/tpslInputRow";
import { TPSLPositionTypeWidget } from "../components/tpslPostionType";
import { useEditBracketOrder } from "./editBracketOrder.script";

type Props = ReturnType<typeof useEditBracketOrder>;
export const EditBracketOrderUI = (props: Props & { onClose?: () => void }) => {
  const { t } = useTranslation();
  const { errors, validated } = props.metaState;
  const { isMobile } = useScreen();
  // console.log('errors', errors, validated);

  const isSlPriceWarning =
    props.slPriceError?.sl_trigger_price?.type ===
    ERROR_MSG_CODES.SL_PRICE_WARNING;

  const isSlPriceError =
    props.slPriceError?.sl_trigger_price?.type ===
    ERROR_MSG_CODES.SL_PRICE_ERROR;

  const { getErrorMsg } = useOrderEntryFormErrorMsg(props.slPriceError);

  const {
    formattedOrder,
    setValue: setOrderValue,
    symbol,
    symbolInfo,
    setValues,
  } = props;

  const [tpValues, setTpValuse] = useState<{
    // enable: boolean;
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
    // enable: false,
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
    // enable: boolean;
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
    // enable: false,
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
  return (
    <div>
      <ScrollArea
        className={cn("oui-flex-1", isMobile && "oui-h-[calc(100vh-200px)]")}
      >
        <div className="">
          <OrderInfo
            order={{
              symbol,
              order_quantity: formattedOrder.order_quantity,
              order_price: formattedOrder.order_price,
            }}
            estLiqPrice={props.estLiqPrice ?? undefined}
            baseDP={symbolInfo.base_dp}
            quoteDP={symbolInfo.quote_dp}
          />
        </div>
        <Divider className="oui-my-3" />
        <div className="">
          <div className="oui-py-3">
            <TPSLPositionTypeWidget
              value={formattedOrder.position_type ?? PositionType.PARTIAL}
              disableSelector
              onChange={(key, value) => {
                setOrderValue("position_type", value);
              }}
            />
          </div>
          <Flex direction={"column"} gap={6}>
            {/* {formattedOrder.tp_enable && ( */}
            <TPSLInputRowWidget
              disableEnableCheckbox
              disableOrderTypeSelector={props.disableTPOrderTypeSelector}
              rootOrderPrice={formattedOrder.order_price}
              symbol={symbolInfo.symbol}
              type="tp"
              side={formattedOrder.side as OrderSide}
              values={tpValues}
              errors={validated ? errors : null}
              quote_dp={symbolInfo.quote_dp}
              onChange={(key, value) => {
                setOrderValue(key as keyof PaxeerNetworkOrder, value);
              }}
              positionType={
                formattedOrder.position_type ?? PositionType.PARTIAL
              }
              marginMode={formattedOrder.margin_mode}
            />
            {/* )} */}
            {/* {formattedOrder.sl_enable && formattedOrder.tp_enable && ( */}
            <Divider className="oui-w-full" />
            {/* )} */}
            {/* {formattedOrder.sl_enable && ( */}
            <TPSLInputRowWidget
              disableEnableCheckbox
              disableOrderTypeSelector={props.disableSLOrderTypeSelector}
              rootOrderPrice={formattedOrder.order_price}
              symbol={symbolInfo.symbol}
              type="sl"
              side={formattedOrder.side as OrderSide}
              values={slValues}
              inputWarnNode={
                isSlPriceWarning && (
                  <DotStatus
                    color="warning"
                    size="xs"
                    label={getErrorMsg("sl_trigger_price")}
                  />
                )
              }
              errors={
                validated ? errors : isSlPriceError ? props.slPriceError : null
              }
              quote_dp={symbolInfo.quote_dp}
              positionType={
                formattedOrder.position_type ?? PositionType.PARTIAL
              }
              onChange={(key, value) => {
                setOrderValue(key as keyof PaxeerNetworkOrder, value);
              }}
              marginMode={formattedOrder.margin_mode}
            />
            {/* )} */}
          </Flex>

          <PnlInfo
            tp_pnl={formattedOrder.tp_pnl}
            sl_pnl={formattedOrder.sl_pnl}
            className="oui-mt-6"
          />
        </div>
      </ScrollArea>
      <Flex
        itemAlign={"center"}
        gap={3}
        mt={5}
        width={"100%"}
        justify={"center"}
      >
        <ThrottledButton
          className="oui-w-[184px]"
          data-testid={"tpsl-confirm"}
          disabled={!props.isPriceChanged}
          loading={props.isMutating}
          onClick={() => {
            props
              .onSubmit()
              .then(() => {
                props.onClose?.();
              })
              .catch((err) => {
                console.log("--->>>cancel order", err);
              });
          }}
        >
          {t("common.confirm")}
        </ThrottledButton>
      </Flex>
    </div>
  );
};
