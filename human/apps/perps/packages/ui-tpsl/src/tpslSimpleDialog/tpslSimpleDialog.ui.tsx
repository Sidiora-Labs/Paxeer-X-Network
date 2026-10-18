import React from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { useOrderEntryFormErrorMsg } from "@paxeer-network/react-app";
import { Box, Button, cn, Divider, Flex, Text } from "@paxeer-network/ui";
import { TPSLQuantity } from "../components/tpslQty";
import { useTPSLSimpleDialog } from "./tpslSimpleDialog.script";

type Props = ReturnType<typeof useTPSLSimpleDialog> & {
  close?: () => void;
  onComplete?: () => void;
  showAdvancedTPSLDialog?: (options: { qty: number }) => void;
};

export const TPSLSimpleDialogUI: React.FC<Props> = (props) => {
  const { type, triggerPrice, errors, TPSL_OrderEntity } = props;
  const { t } = useTranslation();
  const { getErrorMsg } = useOrderEntryFormErrorMsg(errors);

  const footer = (
    <Flex width="100%" itemAlign="center" gap={3} mt={4}>
      <Button
        key="secondary"
        color="gray"
        onClick={() => {
          props.onComplete?.();
          props.close?.();
        }}
        fullWidth
        className="oui-text-sm"
        size="md"
      >
        {t("common.cancel")}
      </Button>
      <Button
        key="primary"
        onClick={() => {
          props.onSubmit().then(() => {
            props.onComplete?.();
            props.close?.();
          });
        }}
        fullWidth
        className="oui-text-sm"
        size="md"
      >
        {t("common.confirm")}
      </Button>
    </Flex>
  );

  const errorMessage = () => {
    const tpError = getErrorMsg("tp_trigger_price");
    const slError = getErrorMsg("sl_trigger_price");
    let text = "";
    if (tpError && type === "tp") {
      text = tpError;
    }
    if (slError && type === "sl") {
      text = slError;
    }
    if (!text) {
      return null;
    }
    return (
      <Flex
        justify={"start"}
        itemAlign={"start"}
        gap={2}
        className="oui-mt-2 oui-w-full"
      >
        <div className="oui-relative oui-top-[7px] oui-size-1 oui-rounded-full oui-bg-danger" />
        <Text className="oui-text-danger">{text}</Text>
      </Flex>
    );
  };

  const renderPnl = () => {
    const { tp_pnl, sl_pnl } = TPSL_OrderEntity;
    if (type === "sl") {
      return (
        <Flex justify={"between"} className="oui-w-full">
          <Text size="2xs">{t("tpsl.totalEstSlPnl")}</Text>
          {sl_pnl ? (
            <Text.numeral
              suffix={
                <Text className="oui-ms-1 oui-text-base-contrast-36">USDC</Text>
              }
              coloring
              visible={true}
              size="2xs"
              dp={2}
            >
              {Number(sl_pnl)}
            </Text.numeral>
          ) : (
            <Text size="2xs">-- USDC</Text>
          )}
        </Flex>
      );
    }
    return (
      <Flex justify={"between"} className="oui-w-full">
        <Text size="2xs">{t("tpsl.totalEstTpPnl")}</Text>
        <Text.numeral
          suffix={
            <Text className="oui-text-base-contrast-36 oui-ms-1">USDC</Text>
          }
          rule="price"
          coloring
          visible={true}
          size="2xs"
          dp={2}
        >
          {tp_pnl ? Number(tp_pnl) : "--"}
        </Text.numeral>
      </Flex>
    );
  };

  return (
    <Box className="oui-w-full oui-px-0.5">
      <TPSLQuantity
        maxQty={props.maxQty}
        quantity={props.orderQuantity ?? props.maxQty}
        baseTick={props.symbolInfo("base_tick")}
        base_dp={props.symbolInfo("base_dp")}
        base={props.symbolInfo("base")}
        isEditing={false}
        errorMsg={getErrorMsg("quantity")}
        onQuantityChange={props.setQuantity}
      />
      <Flex
        direction={"column"}
        itemAlign={"start"}
        className={cn(
          "oui-mt-4 oui-w-full oui-gap-1 oui-text-xs oui-text-base-contrast-36",
        )}
      >
        <Flex justify={"between"} className="oui-w-full">
          <Text size="xs">{t("common.triggerPrice")}</Text>
          <Text.numeral
            className="oui-text-base-contrast"
            suffix={
              <Text className="oui-ms-1 oui-text-xs oui-text-base-contrast-36">
                USDC
              </Text>
            }
            rule="price"
            size="xs"
            dp={props.symbolInfo("quote_dp")}
          >
            {triggerPrice ? Number(triggerPrice) : "--"}
          </Text.numeral>
        </Flex>
        <Flex justify={"between"} className="oui-w-full">
          <Text size="xs">{t("common.orderPrice")}</Text>
          <Text className="oui-text-base-contrast" size="xs">
            {t("common.market")}
          </Text>
        </Flex>
        {renderPnl()}
      </Flex>
      {errorMessage()}

      <Divider className="oui-my-3 oui-w-full" />
      <Flex
        itemAlign={"center"}
        onClick={() => {
          props.close?.();
          props.showAdvancedTPSLDialog?.({
            qty: Number(TPSL_OrderEntity.quantity),
          });
        }}
        className="oui-pb-4"
      >
        <Text color="primary" className="oui-cursor-pointer oui-text-sm">
          {t("tpsl.advancedSetting")}
        </Text>
      </Flex>
      {footer}
    </Box>
  );
};
