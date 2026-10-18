import { FC, useMemo } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { useOrderEntryFormErrorMsg } from "@paxeer-network/react-app";
import {
  Button,
  Divider,
  Flex,
  SimpleDialog,
  Text,
  ThrottledButton,
} from "@paxeer-network/ui";
import { isPositionalTPSL } from "@paxeer-network/utils";
import { ActivitedPriceInput } from "./components/activitedPriceInput";
import { ConfirmDialogContent } from "./components/editDialogContent";
import { EditSheetContext } from "./components/editSheetContext";
import { EditSheetHeader } from "./components/editSheetHeader";
import { PriceInput } from "./components/priceInput";
import { QuantityInput } from "./components/quantityInput";
import { QuantitySlider } from "./components/quantitySlider";
import { TrailingCallbackInput } from "./components/trailingCallbackInput";
import { TriggerPriceInput } from "./components/triggerPriceInput";
import { EditSheetState } from "./editSheet.script";

export const EditSheet: FC<EditSheetState> = (props) => {
  const {
    item,
    symbolInfo,
    formattedOrder,
    isStopMarket,
    isTrailingStop,
    setOrderValue,
    errors,
  } = props;
  const { t } = useTranslation();

  const { getErrorMsg } = useOrderEntryFormErrorMsg(errors!);

  const disabled =
    !props.isChanged ||
    Object.keys(errors!).length > 0 ||
    // when activated price is not empty, the activated price input should not be empty
    (!!item.activated_price && !formattedOrder.activated_price);

  const header = <EditSheetHeader item={item} />;

  const lastPrice = (
    <Flex width={"100%"} justify={"between"}>
      <Text>{t("common.lastPrice")}</Text>
      <Text.numeral dp={symbolInfo.quote_dp}>
        {props.markPrice ?? "--"}
      </Text.numeral>
    </Flex>
  );

  const triggerPriceInput = props.showTriggerPrice && (
    <TriggerPriceInput
      trigger_price={formattedOrder.trigger_price}
      disabled={!!item.is_triggered}
    />
  );

  const renderPriceInput = () => {
    if (isTrailingStop) {
      return (
        item.activated_price && (
          <ActivitedPriceInput
            activated_price={formattedOrder.activated_price}
            disabled={item.is_activated}
          />
        )
      );
    }

    return (
      <PriceInput
        order_price={
          isStopMarket
            ? t("orderEntry.orderType.market")
            : formattedOrder.order_price
        }
        disabled={isStopMarket}
      />
    );
  };

  const trailingCallbackInput = isTrailingStop && (
    <TrailingCallbackInput
      isCallbackValue={!!item.callback_value}
      callback_value={formattedOrder.callback_value}
      callback_rate={formattedOrder.callback_rate}
    />
  );

  const quantityInput = <QuantityInput value={formattedOrder.order_quantity} />;

  const quantitySlider = (
    <QuantitySlider
      maxQty={props.maxQty}
      quantity={formattedOrder.order_quantity}
    />
  );

  const buttons = (
    <Flex width={"100%"} gap={3} mt={2}>
      <Button
        fullWidth
        color="secondary"
        onClick={(e) => {
          props.onClose();
        }}
      >
        {t("common.cancel")}
      </Button>
      <ThrottledButton
        fullWidth
        onClick={(e) => {
          e.stopPropagation();
          e.preventDefault();
          props.onSheetConfirm();
        }}
        loading={props.submitting}
        disabled={disabled}
      >
        {t("common.confirm")}
      </ThrottledButton>
    </Flex>
  );

  const ctxValue = useMemo(() => {
    return {
      symbolInfo,
      getErrorMsg,
      setOrderValue,
    };
  }, [symbolInfo, getErrorMsg, setOrderValue]);

  return (
    <EditSheetContext.Provider value={ctxValue}>
      <Flex
        direction={"column"}
        gap={3}
        width={"100%"}
        itemAlign={"start"}
        className="oui-text-sm"
      >
        {header}
        <Divider intensity={8} className="oui-w-full" />
        {lastPrice}
        <Flex width={"100%"} direction={"column"} itemAlign={"stretch"} gap={2}>
          {triggerPriceInput}
          {renderPriceInput()}
          {trailingCallbackInput}
          {!isPositionalTPSL(item) && quantityInput}
          {!isPositionalTPSL(item) && quantitySlider}
        </Flex>
        {buttons}
      </Flex>

      <SimpleDialog
        open={props.dialogOpen}
        onOpenChange={props.setDialogOpen}
        title={t("orders.editOrder")}
        size="xs"
        actions={{
          primary: {
            label: t("common.confirm"),
            onClick: props.onDialogConfirm,
            loading: props.submitting,
            fullWidth: true,
          },
          secondary: {
            label: t("common.cancel"),
            onClick: props.onCloseDialog,
            fullWidth: true,
          },
        }}
        classNames={{
          content: "oui-pb-4",
          body: "oui-p-0",
          footer: "oui-pt-3 oui-pb-0",
        }}
      >
        <ConfirmDialogContent {...props} />
      </SimpleDialog>
    </EditSheetContext.Provider>
  );
};
