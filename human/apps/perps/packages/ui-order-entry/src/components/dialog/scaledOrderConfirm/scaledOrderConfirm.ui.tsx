import { forwardRef, SVGProps, useMemo } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { OrderSide } from "@paxeer-network/types";
import {
  Button,
  Flex,
  Grid,
  DataTable,
  cn,
  Badge,
  TokenIcon,
  Tooltip,
  Text,
} from "@paxeer-network/ui";
import { SymbolBadge } from "../../symbolBadge";
import {
  ScaledOrderConfirmScriptOptions,
  ScaledOrderConfirmScriptReturns,
} from "./scaledOrderConfirm.script";

export type ScaledOrderConfirmProps = ScaledOrderConfirmScriptOptions &
  ScaledOrderConfirmScriptReturns & {
    close?: () => void;
    resolve: (value?: any) => void;
    reject: (reason?: any) => void;
  };

export const ScaledOrderConfirm = (props: ScaledOrderConfirmProps) => {
  const { order, symbolInfo, dataSource, national, askAndBid, totalQuantity } =
    props;
  const { base, quote, base_dp, quote_dp } = symbolInfo;
  const { t } = useTranslation();

  const onCancel = () => {
    props.reject();
    props.close?.();
  };

  const onConfirm = () => {
    props.resolve();
    props.close?.();
  };

  const columns = useMemo(() => {
    return [
      {
        title: t("common.symbol"),
        dataIndex: "symbol",
        width: 125,
        render: (value: string, record: any) => {
          return (
            <Flex gap={2}>
              <div
                className={cn(
                  "oui-h-[42px] oui-w-1 oui-shrink-0 oui-rounded-[1px]",
                  record.side === OrderSide.BUY
                    ? "oui-bg-trade-profit"
                    : "oui-bg-trade-loss",
                )}
              />
              <Flex direction="column" itemAlign="start">
                <Flex gapX={1}>
                  <TokenIcon symbol={value} className="oui-size-3" />
                  <Text.formatted rule="symbol" size="xs" formatString="base">
                    {value}
                  </Text.formatted>
                </Flex>

                <Flex gap={1}>
                  <SymbolBadge symbol={value} />
                  <Badge color="neutral" size="xs">
                    {t("orderEntry.orderType.limit")}
                  </Badge>
                </Flex>
              </Flex>
            </Flex>
          );
        },
      },
      {
        title: t("common.quantity"),
        dataIndex: "order_quantity",
        width: 100,
        render: (value: string, record: any) => {
          return (
            <Text.numeral
              rule="price"
              dp={base_dp}
              padding={false}
              color={record.side === OrderSide.BUY ? "buy" : "sell"}
            >
              {value}
            </Text.numeral>
          );
        },
      },
      {
        title: t("common.orderPrice"),
        dataIndex: "order_price",
        width: 100,
        render: (value: string, record: any) => {
          // buy: limit_price_i >= ask0 , show warning.
          // sell: limit price <= bid0 , show warning.
          const showWarning = !!(record.side === OrderSide.BUY
            ? askAndBid?.[0] && Number(value) >= askAndBid?.[0]
            : askAndBid?.[1] && Number(value) <= askAndBid?.[1]);

          return (
            <Flex gapX={1}>
              <Text.numeral rule="price" dp={quote_dp}>
                {value}
              </Text.numeral>

              {showWarning && (
                <Tooltip
                  content={t(
                    "orderEntry.confirmScaledOrder.orderPrice.warning",
                  )}
                  className="oui-w-[240px] oui-text-2xs oui-font-semibold oui-text-base-contrast-80"
                >
                  <TooltipIcon className="oui-text-warning-darken" />
                </Tooltip>
              )}
            </Flex>
          );
        },
      },
    ];
  }, [t, symbolInfo, askAndBid, base_dp, quote_dp]);

  return (
    <div className="oui-orderEntry-scaledOrderConfirm oui-font-semibold">
      <DataTable
        classNames={{
          root: cn(
            "oui-scaledOrderConfirm-table",
            "oui-bg-base-7",
            "oui-rounded-lg",
            // need to set overflow hidden because table header will avoid the border radius
            "oui-overflow-hidden",
            // "oui-text-2xs lg:oui-text-xs",
            // if orders is greater than 6, set the height to 320px to show scroll bar
            order.orders?.length >= 6 && "oui-h-[320px]",
          ),
          // set the min height of the table to show 2 rows
          scroll: "!oui-min-h-[130px]",
        }}
        dataSource={dataSource}
        columns={columns}
        bordered
        onRow={() => {
          return {
            className: cn("oui-h-[50px]"),
          };
        }}
      />
      <div className="oui-scaledOrderConfirm-content oui-mb-5 oui-mt-4 oui-text-2xs">
        <Flex justify="between">
          <Text>{t("orderEntry.totalOrders")}</Text>
          <Text intensity={80}>{order.orders?.length}</Text>
        </Flex>

        <Flex justify="between" mt={2}>
          <Text>{t("orderEntry.totalQuantity")}</Text>
          <Text.numeral
            rule="price"
            unit={base}
            dp={base_dp}
            padding={false}
            intensity={80}
            unitClassName={"oui-text-base-contrast-36 oui-ms-1"}
          >
            {totalQuantity}
          </Text.numeral>
        </Flex>

        <Flex justify="between" mt={2}>
          <Text>{t("common.notional")}</Text>
          <Text.numeral
            rule="price"
            unit={quote}
            dp={quote_dp}
            padding={false}
            intensity={80}
            unitClassName={"oui-text-base-contrast-36 oui-ms-1"}
          >
            {national}
          </Text.numeral>
        </Flex>
      </div>

      <Grid cols={2} gapX={3}>
        <Button
          color={"secondary"}
          size={"md"}
          className="oui-scaledOrderConfirm-cancel-btn"
          onClick={onCancel}
        >
          {t("common.cancel")}
        </Button>
        <Button
          size={"md"}
          className="oui-scaledOrderConfirm-confirm-btn"
          onClick={onConfirm}
        >
          {t("common.confirm")}
        </Button>
      </Grid>
    </div>
  );
};

export const TooltipIcon = forwardRef<SVGSVGElement, SVGProps<SVGSVGElement>>(
  (props, ref) => {
    return (
      <svg
        width="12"
        height="13"
        viewBox="0 0 12 13"
        fill="currentColor"
        xmlns="http://www.w3.org/2000/svg"
        ref={ref}
        {...props}
      >
        <path d="M5.99951 1.50708C3.23811 1.50708 0.999512 3.74558 0.999512 6.50708C0.999512 9.26858 3.23811 11.5071 5.99951 11.5071C8.76091 11.5071 10.9995 9.26858 10.9995 6.50708C10.9995 3.74558 8.76091 1.50708 5.99951 1.50708ZM5.99951 4.00708C6.27566 4.00708 6.49951 4.23108 6.49951 4.50708C6.49951 4.78308 6.27566 5.00708 5.99951 5.00708C5.72336 5.00708 5.49951 4.78308 5.49951 4.50708C5.49951 4.23108 5.72336 4.00708 5.99951 4.00708ZM5.99951 5.50708C6.27566 5.50708 6.49951 5.73108 6.49951 6.00708V8.50708C6.49951 8.78308 6.27566 9.00708 5.99951 9.00708C5.72336 9.00708 5.49951 8.78308 5.49951 8.50708V6.00708C5.49951 5.73108 5.72336 5.50708 5.99951 5.50708Z" />
      </svg>
    );
  },
);
