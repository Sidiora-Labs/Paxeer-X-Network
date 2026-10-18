import { useMemo } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { API } from "@paxeer-network/types";
import {
  Box,
  cn,
  Flex,
  HoverCard,
  Column,
  Text,
  Tooltip,
} from "@paxeer-network/ui";
import { SymbolLeverageDialogId } from "@paxeer-network/ui-leverage";
import { SharePnLOptions, SharePnLDialogId } from "@paxeer-network/ui-share";
import { Decimal } from "@paxeer-network/utils";
import { LIQ_DISTANCE_THRESHOLD } from "../../../constants";
import { FundingFeeButton } from "../../fundingFeeHistory/fundingFeeButton";
import { negateFee } from "../../fundingFeeHistory/negateFee";
import { RwaStatusTag } from "../../rwaStatus/rwaStatus";
import { AdjustMarginButton } from "../adjustMargin/adjustMarginButton";
import { ClosePositionWidget } from "../closePosition";
import { LeverageBadge } from "./components";
import { renderQuantity } from "./listElement";
import { NumeralWithCtx } from "./numeralWithCtx";
import { PartialTPSL } from "./partialTPSL";
import { PositionSymbolCell } from "./positionSymbolCell";
import { ReversePositionButton } from "./reversePotisionButton";
import { ShareButtonWidget } from "./shareButton";
import { TriggerPrice } from "./triggerPrice";
import { UnrealizedPnLPopoverCard } from "./unrealPnLHover";

interface ColumnConfig {
  pnlNotionalDecimalPrecision?: number;
  sharePnLConfig?: SharePnLOptions;
  onSymbolChange?: (symbol: API.Symbol) => void;
  positionReverse?: boolean;
}

export const useColumn = (config: ColumnConfig) => {
  const {
    pnlNotionalDecimalPrecision,
    sharePnLConfig,
    onSymbolChange,
    positionReverse,
  } = config;
  const { t } = useTranslation();
  const column = useMemo<Column<API.PositionTPSLExt>[]>(
    () => [
      {
        title: t("common.symbol"),
        dataIndex: "symbol",
        fixed: "start",
        width: 200,
        onSort: (r1, r2) => {
          return r1.symbol?.localeCompare(r2.symbol || "");
          // if (sortOrder === "asc") {
          //   return r1.symbol.localeCompare(r2.symbol);
          // }
          // return r2.symbol.localeCompare(r1.symbol);
        },
        render: (value: string, record) => (
          <Flex gap={2} className="oui-py-1">
            <Box
              width={4}
              height={24}
              className={cn(
                "oui-h-[42px] oui-rounded-[1px]",
                record.position_qty > 0
                  ? "oui-bg-trade-profit"
                  : "oui-bg-trade-loss",
              )}
            />
            <Flex direction="column" itemAlign="start" gap={1}>
              <Flex gap={1} itemAlign="center">
                <PositionSymbolCell
                  symbol={value}
                  onSymbolChange={onSymbolChange}
                />
                <RwaStatusTag symbol={value} />
              </Flex>
              <Flex gap={1} wrap="wrap">
                <LeverageBadge
                  symbol={value}
                  leverage={record.leverage}
                  modalId={SymbolLeverageDialogId}
                  marginMode={record.margin_mode as any}
                />
              </Flex>
            </Flex>
          </Flex>
        ),
      },
      {
        title: t("common.quantity"),
        dataIndex: "position_qty",
        // onSort: true,
        onSort: (a, b) => {
          return a.position_qty - b.position_qty;
        },
        width: 120,
        className: "oui-ps-6",
        // rule: "price",
        // numeralProps: {
        //   coloring: true,
        //   // tick: "base_dp",
        // },
        render: renderQuantity,
        // render: (value: string) => (
        //   <NumeralWithCtx
        //     coloring
        //     className="oui-font-semibold"
        //     tick="base_dp"
        //   >
        //     {value}
        //   </NumeralWithCtx>
        // ),
      },
      {
        title: t("common.avgOpen"),
        className: "oui-h-[48px]",
        width: 120,
        onSort: true,
        dataIndex: "average_open_price",
        render: (value: string) => {
          // const ctx = usePositionsRowContext();
          return (
            <NumeralWithCtx rm={Decimal.ROUND_DOWN}>{value}</NumeralWithCtx>
          );
        },
      },
      {
        title: t("common.markPrice"),
        dataIndex: "mark_price",
        width: 120,
        onSort: true,
        className: "oui-h-[48px]",
        render: (value: string) => {
          return (
            <NumeralWithCtx rm={Decimal.ROUND_DOWN}>{value}</NumeralWithCtx>
          );
        },
      },
      {
        title: (
          <Tooltip
            className="oui-max-w-[280px] oui-bg-base-8 oui-p-3 oui-text-2xs oui-text-base-contrast-54"
            content={
              <div>
                <div className="oui-text-pretty">
                  {t("common.liquidationPrice.tooltip")}
                </div>
                <div>
                  <a
                    href="https://paxeer-network.network/docs/introduction/trade-on-paxeer-network/perpetual-futures/liquidations"
                    target="_blank"
                    rel="noopener noreferrer"
                    className="oui-text-primary oui-underline"
                  >
                    {t("common.liquidationPrice.tooltip.learnMore")}
                  </a>
                </div>
              </div>
            }
          >
            <Text className="oui-underline oui-decoration-dotted">
              {t("positions.column.liqPrice")}
            </Text>
          </Tooltip>
        ),
        width: 100,
        onSort: true,
        dataIndex: "est_liq_price",
        render: (value: string, record) => {
          const liqPrice = Number(value);
          const markPrice = record.mark_price;
          if (
            liqPrice === 0 ||
            !markPrice ||
            markPrice === 0 ||
            Math.abs(liqPrice - markPrice) / markPrice >= LIQ_DISTANCE_THRESHOLD
          ) {
            return "--";
          }

          return (
            <NumeralWithCtx
              rm={Decimal.ROUND_DOWN}
              className={liqPrice > 0 ? "oui-text-warning-light" : ""}
            >
              {value ?? "--"}
            </NumeralWithCtx>
          );
        },
      },
      {
        title: (
          <HoverCard
            content={<UnrealizedPnLPopoverCard />}
            side="top"
            align="center"
            className="oui-max-w-[280px] oui-text-2xs"
          >
            <Text className="oui-underline oui-decoration-dotted">
              {t("common.unrealizedPnl")}
            </Text>
          </HoverCard>
        ),
        dataIndex: "unrealized_pnl",
        width: 180,
        onSort: true,
        rule: "price",
        numeralProps: {
          coloring: true,
          // tick: "base_dp",
        },
        // hint: <UnrealizedPnLPopoverCard />,
        render: (value: string, record) => {
          return (
            <Flex gap={2}>
              <Flex>
                <Text.pnl
                  dp={pnlNotionalDecimalPrecision}
                  coloring
                  className="oui-font-semibold"
                >
                  {value}
                </Text.pnl>
                <Text.roi
                  rule="percentages"
                  dp={pnlNotionalDecimalPrecision}
                  coloring
                  className="oui-font-semibold"
                  prefix="("
                  suffix=")"
                >
                  {record.unrealized_pnl_ROI}
                </Text.roi>
              </Flex>
              <ShareButtonWidget
                position={record}
                sharePnLConfig={sharePnLConfig}
                modalId={SharePnLDialogId}
              />
            </Flex>
          );
        },
      },
      {
        title: t("common.fullTPSL"),
        dataIndex: "full_tpsl",
        width: 150,
        render: (_: string, record) => {
          return (
            <TriggerPrice
              stopLossPrice={record.full_tp_sl?.sl_trigger_price}
              takeProfitPrice={record.full_tp_sl?.tp_trigger_price}
            />
          );
        },
      },

      {
        title: t("common.partialTPSL"),
        dataIndex: "partial_tpsl",
        width: 150,
        render: (_: string, record) => (
          <PartialTPSL
            orderNum={record.partial_tp_sl?.order_num}
            tpTriggerPrice={record.partial_tp_sl?.tp_trigger_price}
            slTriggerPrice={record.partial_tp_sl?.sl_trigger_price}
          />
        ),
      },
      {
        title: t("common.notional"),
        dataIndex: "notional",
        className: "oui-h-[48px]",
        width: 100,
        onSort: true,
        render: (value: string) => (
          <Text.notional dp={pnlNotionalDecimalPrecision}>
            {value}
          </Text.notional>
        ),
      },
      {
        title: (
          <Tooltip
            className="oui-max-w-[280px] oui-bg-base-8 oui-p-3 oui-text-2xs oui-text-base-contrast-54"
            content={
              <Flex
                direction={"column"}
                gap={3}
                className="oui-rounded-sm oui-bg-base-8 oui-text-base-contrast-54"
              >
                <span>{t("positions.column.margin.tooltip")}</span>
                {/* <Divider className="oui-w-full" />
                <span>{t("positions.column.margin.formula")}</span> */}
              </Flex>
            }
          >
            <Text className="oui-underline oui-decoration-dotted">
              {t("positions.column.margin")}
            </Text>
          </Tooltip>
        ),
        dataIndex: "mm",
        onSort: true,
        width: 140,
        rule: "price",
        render: (value: string, record: API.PositionTPSLExt) => {
          const isIsolated = record.margin_mode === "ISOLATED";
          return (
            <Flex gap={2} itemAlign="center">
              <Text.numeral>
                {isIsolated ? (record.margin ?? "--") : "--"}
              </Text.numeral>
              {isIsolated && (
                <AdjustMarginButton position={record} mode="dialog" />
              )}
            </Flex>
          );
        },
      },
      {
        title: (
          <Tooltip
            className="oui-max-w-[280px] oui-bg-base-8 oui-p-3 oui-text-2xs oui-text-base-contrast-54"
            content={t("positions.unsettledFundingFee.tooltip")}
          >
            <Text className="oui-underline oui-decoration-dotted">
              {t("positions.unsettledFundingFee")}
            </Text>
          </Tooltip>
        ),
        dataIndex: "accrued_funding_fee",
        width: 140,
        render: (value, record) => (
          <FundingFeeButton
            fee={negateFee(value)}
            symbol={record.symbol}
            start_t={record.timestamp.toString()}
            feeType="unsettled"
            marginMode={record.margin_mode}
          />
        ),
      },
      // {
      //   title: t("common.qty"),
      //   dataIndex: "close_qty",
      //   width: 100,
      //   fixed: "end",
      //   render: renderQuantityInput,
      // },
      // {
      //   title: t("common.price"),
      //   dataIndex: "close_price",
      //   width: 100,
      //   fixed: "end",
      //   render: renderPriceInput,
      //   // render: (value: string) => <PriceInput />,
      // },
      {
        title: null,
        dataIndex: "close_position",
        align: "right",
        width: positionReverse ? 100 : 70,
        fixed: "end",
        render(_, record) {
          return (
            <Flex gapX={2} justify={"end"}>
              <ClosePositionWidget />
              {positionReverse && <ReversePositionButton position={record} />}
            </Flex>
          );
        },
      },
    ],
    [
      onSymbolChange,
      pnlNotionalDecimalPrecision,
      sharePnLConfig,
      t,
      positionReverse,
    ],
  );
  return column;
};
