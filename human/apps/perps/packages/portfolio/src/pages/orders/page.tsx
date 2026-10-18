import { FC, SVGProps, useRef } from "react";
import React from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { Box, Button, Divider, Flex, Text, Tooltip } from "@paxeer-network/ui";
import { OrderListInstance, OrdersWidget } from "@paxeer-network/ui-orders";
import { SharePnLConfig } from "@paxeer-network/ui-share";

export const OrdersPage: React.FC<{ sharePnLConfig?: SharePnLConfig }> = (
  props,
) => {
  const { sharePnLConfig } = props;
  const { t } = useTranslation();

  const ordersRef = useRef<OrderListInstance>(null);

  const onDownload = () => {
    ordersRef.current?.download?.();
  };

  return (
    <Flex
      // p={6}
      direction={"column"}
      itemAlign={"start"}
      gap={4}
      width="100%"
      height="100%"
    >
      <Flex width="100%" justify="between">
        <Text size="lg">{t("common.orders")}</Text>
        <Tooltip content={t("orders.download.tooltip")}>
          <Button
            color="gray"
            size="sm"
            className="oui-bg-base-4"
            onClick={onDownload}
          >
            {t("common.download")}
            <TooltipIcon className="oui-text-base-contrast-36 oui-ms-[5px]" />
          </Button>
        </Tooltip>
      </Flex>
      <Divider className="oui-w-full" />
      {/* 26(title height) + 1(divider) + 32 (padding) */}
      <Box width="100%" className="oui-h-[calc(100%_-_59px)]">
        <OrdersWidget ref={ordersRef} sharePnLConfig={sharePnLConfig} />
      </Box>
    </Flex>
  );
};

const TooltipIcon = React.forwardRef<SVGSVGElement, SVGProps<SVGSVGElement>>(
  (props, ref) => {
    return (
      <svg
        width="12"
        height="12"
        viewBox="0 0 12 12"
        fill="currentColor"
        xmlns="http://www.w3.org/2000/svg"
        ref={ref}
        {...props}
      >
        <path d="M5.999 1.007a5 5 0 1 0 0 10 5 5 0 0 0 0-10m0 2.5a.5.5 0 1 1 0 1 .5.5 0 0 1 0-1m0 1.5a.5.5 0 0 1 .5.5v2.5a.5.5 0 0 1-1 0v-2.5a.5.5 0 0 1 .5-.5" />
      </svg>
    );
  },
);
