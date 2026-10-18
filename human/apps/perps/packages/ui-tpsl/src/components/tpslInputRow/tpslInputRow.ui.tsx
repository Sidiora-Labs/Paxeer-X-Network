import { FC, Fragment, useEffect } from "react";
import { useMemoizedFn } from "@paxeer-network/hooks";
import { useTranslation, Trans } from "@paxeer-network/i18n";
import { useOrderEntryFormErrorMsg } from "@paxeer-network/react-app";
import { OrderType, PositionType } from "@paxeer-network/types";
import { Flex, Text, Grid, Checkbox, cn } from "@paxeer-network/ui";
import { PnlInputWidget } from "../../pnlInput/pnlInput.widget";
import { OrderPriceType } from "../orderPriceType";
import { PriceInput } from "./priceInput";
import { useTPSLInputRowScript } from "./tpslInputRow.script";

type TPSLInputRowProps = ReturnType<typeof useTPSLInputRowScript>;

export const TPSLInputRowUI: FC<TPSLInputRowProps> = (props) => {
  const { t } = useTranslation();
  const { getErrorMsg } = useOrderEntryFormErrorMsg(props.errors);
  const { values, positionType } = props;

  return (
    <Flex
      direction={"column"}
      itemAlign={"start"}
      justify={"start"}
      className="oui-w-full"
    >
      {/* <Flex className="oui-w-full" itemAlign={"center"} justify={"start"}>
        {!props.disableEnableCheckbox && (
          <Checkbox
            data-testid={`oui-testid-orderEntry-${props.type}-enable-checkBox`}
            id={`enable_${props.type}`}
            color={"white"}
            checked={values.enable}
            onCheckedChange={(checked: boolean) => {
              props.onChange(`${props.type}_enable`, !!checked);
            }}
          />
        )}
        <label
          htmlFor={`enable_${props.type}`}
          className={cn(
            "oui-text-sm",
            props.disableEnableCheckbox
              ? "oui-ms-0 oui-text-base-contrast"
              : "oui-ms-1  oui-text-base-contrast-36",
          )}
        >
          {props.type === "tp" ? t("tpsl.takeProfit") : t("tpsl.stopLoss")}
        </label>
      </Flex> */}
      <Text size="sm" intensity={98}>
        {props.type === "tp" ? t("tpsl.takeProfit") : t("tpsl.stopLoss")}
      </Text>
      <Flex
        direction={"column"}
        gap={2}
        itemAlign={"start"}
        className={"oui-w-full oui-pt-2"}
      >
        <Flex
          direction={"column"}
          itemAlign={"start"}
          className="oui-w-full oui-gap-0.5"
        >
          <Text className="oui-text-2xs oui-text-base-contrast-54">
            {t("common.triggerPrice")}
          </Text>
          <Grid cols={2} gap={2} className="oui-w-full oui-px-0.5">
            <PriceInput
              disabled={props.disableTriggerEditing}
              type={`${props.type} price`}
              value={values.trigger_price}
              error={getErrorMsg(`${props.type}_trigger_price`)}
              onValueChange={(value) => {
                if (!props.disableTriggerEditing) {
                  props.onChange(`${props.type}_trigger_price`, value);
                }
              }}
              quote_dp={props.quote_dp}
              classNames={{
                root: cn(
                  !getErrorMsg(`${props.type}_trigger_price`) &&
                    "oui-outline-line-12",
                  props.disableTriggerEditing &&
                    "oui-bg-base-6 oui-opacity-50 oui-cursor-not-allowed",
                  props.inputWarnNode &&
                    "oui-outline-warning-darken focus-within:oui-outline-warning-darken",
                ),
                input: props.disableTriggerEditing
                  ? "oui-text-base-contrast-54"
                  : undefined,
              }}
            />
            <PnlInputWidget
              disabled={
                !!props.disableTriggerEditing &&
                (values.order_type !== OrderType.LIMIT || !values.trigger_price)
              }
              type={props.type === "tp" ? "TP" : "SL"}
              onChange={(key, value) => {
                props.onChange(key, value as string);
              }}
              quote={"USDC"}
              quote_dp={2}
              values={values}
            />
          </Grid>
        </Flex>
        {props.inputWarnNode}
        <Flex
          direction={"column"}
          className={cn("oui-w-full oui-gap-0.5")}
          itemAlign={"start"}
        >
          <Text className="oui-text-2xs oui-text-base-contrast-54">
            {t("common.orderPrice")}
          </Text>
          <Grid cols={2} gap={2} className="oui-w-full oui-px-0.5">
            <PriceInput
              disabled={values.order_type === OrderType.MARKET}
              type={"order price"}
              label={
                values.order_type === OrderType.LIMIT
                  ? t("common.limit")
                  : t("common.market")
              }
              value={values.order_price}
              error={getErrorMsg(`${props.type}_order_price`)}
              classNames={{
                root: cn(
                  "oui-bg-base-6",
                  !getErrorMsg(`${props.type}_order_price`) &&
                    "oui-outline-line-12",
                ),
              }}
              onValueChange={(value) => {
                props.onChange(`${props.type}_order_price`, value);
              }}
              quote_dp={props.quote_dp}
            />
            <OrderPriceType
              disabled={props.disableOrderTypeSelector}
              type={values.order_type}
              onChange={(value) => {
                props.onChange(`${props.type}_order_type`, value as OrderType);
              }}
            />
          </Grid>
        </Flex>
      </Flex>
      <RenderROI
        price={values.trigger_price}
        orderType={values.order_type}
        pnl={values.PnL}
        roi={props.roi}
        dp={props.quote_dp}
        className="oui-mt-1"
      />
    </Flex>
  );
};

const RenderROI: React.FC<{
  className?: string;
  price?: number | string;
  pnl?: number | string;
  roi?: number | null;
  dp: number;
  orderType: OrderType;
}> = (props) => {
  const { t } = useTranslation();
  const { price, pnl, roi, dp, className, orderType } = props;
  if (!roi || !price || !pnl) {
    return null;
  }
  return (
    <Text className={cn("oui-text-2xs oui-text-base-contrast-36", className)}>
      <Trans
        i18nKey="tpsl.advanced.ROI"
        components={[
          <Fragment key="price">
            <Text.numeral
              className="oui-px-1 oui-text-base-contrast"
              dp={dp}
              suffix={<Text className="oui-ps-0.5">USDC</Text>}
            >
              {price}
            </Text.numeral>
          </Fragment>,
          <Fragment key="orderType">
            <Text className="oui-px-1 oui-text-base-contrast">
              {orderType === OrderType.MARKET
                ? t("common.market")
                : t("common.limit")}
            </Text>
          </Fragment>,
          <Fragment key="pnl">
            <Text.numeral
              coloring
              className="oui-whitespace-nowrap oui-px-1"
              dp={2}
              suffix={<Text className="oui-ps-0.5">USDC</Text>}
            >
              {pnl}
            </Text.numeral>
          </Fragment>,

          <Fragment key="roi">
            <Text.numeral
              coloring
              className="oui-whitespace-nowrap oui-px-1"
              dp={2}
              suffix="%"
            >
              {roi}
            </Text.numeral>
          </Fragment>,
        ]}
      />
    </Text>
  );
};
