import { ReactNode } from "react";
import { useMemo } from "react";
import { useLeverageBySymbol, useMaxLeverage } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { API, MarginMode } from "@paxeer-network/types";
import {
  Badge,
  Box,
  capitalizeFirstLetter,
  cn,
  Column,
  Flex,
  Text,
  Tooltip,
} from "@paxeer-network/ui";
import { SharePnLConfig, SharePnLDialogId } from "@paxeer-network/ui-share";
import { commifyOptional, formatNum } from "@paxeer-network/utils";
import { useSymbolContext } from "../../../provider/symbolContext";
import { FundingFeeButton } from "../../fundingFeeHistory/fundingFeeButton";
import { PositionSymbolCell } from "../../positions/desktop/positionSymbolCell";
import { ShareButtonWidget } from "../../positions/desktop/shareButton";
import { PositionHistoryExt } from "../positionHistory.script";

export const usePositionHistoryColumn = (props: {
  onSymbolChange?: (symbol: API.Symbol) => void;
  pnlNotionalDecimalPrecision?: number;
  sharePnLConfig?: SharePnLConfig;
}) => {
  const { onSymbolChange, pnlNotionalDecimalPrecision } = props;
  const { t } = useTranslation();

  const column = useMemo(
    () =>
      [
        // instrument
        {
          title: t("common.symbol"),
          dataIndex: "symbol",
          fixed: "start",
          width: 250,
          onSort: (r1: any, r2: any) => {
            return r1.symbol?.localeCompare(r2.symbol || "");
          },
          render: (value: string, record) => (
            <SymbolInfo record={record} onSymbolChange={onSymbolChange} />
          ),
        },
        // quantity
        {
          title: t("positions.history.column.closed&maxClosed"),
          dataIndex: "close_maxClose",
          width: 200,
          render: (value: string, record) => <Quantity record={record} />,
        },
        // net pnl
        {
          title: t("positions.history.column.netPnl"),
          dataIndex: "netPnL",
          width: 140,
          onSort: (a, b) => {
            if (a.netPnL == null || b.netPnL == null) return -1;
            return (a.netPnL ?? 0) - (b.netPnL ?? 0);
          },
          render: (_: any, record) => (
            <Flex gapX={1}>
              <NetPnL
                record={record}
                pnlNotionalDecimalPrecision={pnlNotionalDecimalPrecision}
              />
              <ShareButtonWidget
                position={record}
                sharePnLConfig={props.sharePnLConfig}
                modalId={SharePnLDialogId}
                isPositionHistory
              />
            </Flex>
          ),
        },
        // avg open
        {
          title: t("common.avgPrice"),
          dataIndex: "avg_open",
          width: 140,
          render: (_: any, record) => {
            const avgOpen =
              record.avg_open_price != null
                ? Math.abs(record.avg_open_price)
                : "--";
            // TODO(hooks): Extract this render body into a cell component before using hooks.
            const { quote_dp } = useSymbolContext();
            return (
              <Text.numeral dp={quote_dp} padding={false}>
                {avgOpen}
              </Text.numeral>
            );
          },
        },
        // avg close
        {
          title: t("common.avgClose"),
          dataIndex: "avg_close",
          width: 175,
          render: (_: any, record) => {
            const avgClose =
              record.avg_close_price != null
                ? Math.abs(record.avg_close_price)
                : "--";
            // TODO(hooks): Extract this render body into a cell component before using hooks.
            const { quote_dp } = useSymbolContext();
            return (
              <Text.numeral dp={quote_dp} padding={false}>
                {avgClose}
              </Text.numeral>
            );
          },
        },
        {
          title: t("funding.fundingFee"),
          dataIndex: "accumulated_funding_fee",
          render: (value, record) => {
            return (
              <FundingFeeButton
                fee={-value}
                symbol={record.symbol}
                start_t={record.open_timestamp.toString()}
                end_t={(
                  record.close_timestamp ?? record.last_update_time
                ).toString()}
                marginMode={record.margin_mode}
              />
            );
          },
        },
        // time opened
        {
          title: t("positions.history.column.timeOpened"),
          dataIndex: "open_timestamp",
          width: 175,
          onSort: true,
          render: (_: any, record) => (
            <Text.formatted rule={"date"} formatString="yyyy-MM-dd HH:mm:ss">
              {record.open_timestamp}
            </Text.formatted>
          ),
        },
        // time close
        {
          title: t("positions.history.column.timeClosed"),
          dataIndex: "close_timestamp",
          width: 175,
          onSort: true,
          render: (_: any, record) => {
            if (record.position_status == "closed" && record.close_timestamp) {
              return (
                <Text.formatted
                  rule={"date"}
                  formatString="yyyy-MM-dd HH:mm:ss"
                >
                  {record.close_timestamp ?? "--"}
                </Text.formatted>
              );
            }
            return "--";
          },
        },
        // updated time
        {
          title: t("positions.history.column.updatedTime"),
          dataIndex: "last_update_time",
          width: 175,
          onSort: true,
          render: (_: any, record) => (
            <Text.formatted rule={"date"} formatString="yyyy-MM-dd HH:mm:ss">
              {record.last_update_time}
            </Text.formatted>
          ),
        },
      ] as Column<PositionHistoryExt>[],
    [onSymbolChange, pnlNotionalDecimalPrecision, props.sharePnLConfig, t],
  );

  return column;
};

export const SymbolInfo = (props: {
  record: PositionHistoryExt;
  onSymbolChange?: (symbol: API.Symbol) => void;
}) => {
  const { record, onSymbolChange } = props;
  const { t } = useTranslation();

  const tags = useMemo(() => {
    const list: ReactNode[] = [];

    const status = record.position_status;

    const renderStatus = () => {
      if (status === "closed") {
        return t("positions.history.status.closed");
      } else if (status === "partial_closed") {
        return t("positions.history.status.partialClosed");
      } else {
        return capitalizeFirstLetter(status.replace("_", " "));
      }
    };

    list.push(
      <Badge
        key={`status-${status}`}
        color={status !== "closed" ? "primaryLight" : "neutral"}
        size="xs"
        className="oui-whitespace-nowrap oui-break-normal"
      >
        {renderStatus()}
      </Badge>,
    );

    if (record.type === "adl") {
      list.push(
        <Badge key={`type-${record.type}`} color={"danger"} size="xs">
          {t("positions.history.type.adl")}
        </Badge>,
      );
    } else if (record.type === "liquidated") {
      list.push(
        <Tooltip
          key={`type-${record.type}`}
          className="oui-min-w-[204px] oui-bg-base-5"
          arrow={{ className: "oui-fill-base-5" }}
          content={
            <Flex
              direction={"column"}
              itemAlign={"start"}
              className="oui-text-2xs"
            >
              {record.liquidation_id != null && (
                <Flex justify={"between"} width={"100%"} gap={2}>
                  <Text intensity={54}>
                    {t("positions.history.liquidated.liquidationId")}
                  </Text>
                  <Text intensity={98}>{record.liquidation_id}</Text>
                </Flex>
              )}
              <Flex justify={"between"} width={"100%"} gap={2}>
                <Text intensity={54}>
                  {t("positions.history.liquidated.liquidatorFee")}
                </Text>
                <Text color="lose">
                  {record.liquidator_fee > 0 && "-"}
                  {commifyOptional(record.liquidator_fee)}
                </Text>
              </Flex>
              <Flex justify={"between"} width={"100%"} gap={2}>
                <Text intensity={54}>
                  {t("positions.history.liquidated.insFundFee")}
                </Text>
                <Text color="lose">
                  {record.insurance_fund_fee > 0 && "-"}
                  {commifyOptional(record.insurance_fund_fee)}
                </Text>
              </Flex>
            </Flex>
          }
        >
          <div>
            <Badge size="xs" color="danger" className="oui-cursor-pointer">
              <span className="oui-underline oui-decoration-dashed oui-decoration-1">
                {t("positions.history.type.liquidated")}
              </span>
            </Badge>
          </div>
        </Tooltip>,
      );
    }

    if (record.margin_mode != null) {
      list.push(
        <Badge
          key={`margin-mode-${record.margin_mode}`}
          color="neutral"
          size="xs"
        >
          {record.margin_mode === MarginMode.ISOLATED
            ? t("marginMode.isolated")
            : t("marginMode.cross")}
        </Badge>,
      );
    }

    list.push(
      <LeverageBadge
        key={`leverage-${record.symbol}`}
        symbol={record.symbol}
        leverage={record.leverage}
      />,
    );

    return list;
  }, [record, t]);

  return (
    <Flex gap={2} className="oui-py-1">
      <Box
        width={4}
        height={38}
        className={cn(
          "oui-shrink-0 oui-rounded-[1px]",
          record.side === "LONG" ? "oui-bg-trade-profit" : "oui-bg-trade-loss",
        )}
      />

      <Flex direction={"column"} itemAlign={"start"} gap={1}>
        <PositionSymbolCell
          symbol={record.symbol}
          onSymbolChange={onSymbolChange}
        />
        <Flex gap={1} wrap="wrap">
          {tags}
        </Flex>
      </Flex>
    </Flex>
  );
};

export const Quantity = (props: { record: PositionHistoryExt }) => {
  const { record } = props;

  const { base_dp } = useSymbolContext();

  return (
    <Flex
      gap={1}
      direction={"column"}
      itemAlign={"start"}
      className="oui-truncate"
    >
      <Text.numeral dp={base_dp} padding={false}>
        {Math.abs(record.closed_position_qty)}
      </Text.numeral>
      <Text.numeral dp={base_dp} padding={false} className="oui-truncate">
        {Math.abs(record.max_position_qty)}
      </Text.numeral>
      {/* <Text className="oui-truncate">{`${record.symbol.split("_")[1]}`}</Text> */}
    </Flex>
  );
};

export const NetPnL = (props: {
  record: PositionHistoryExt;
  pnlNotionalDecimalPrecision?: number;
}) => {
  const { record, pnlNotionalDecimalPrecision } = props;
  const { t } = useTranslation();
  const netPnl = record.netPnL != null ? record.netPnL : undefined;

  const text = () => (
    <Text.pnl
      dp={pnlNotionalDecimalPrecision}
      color={
        record.netPnL != null
          ? record.netPnL > 0
            ? "profit"
            : "lose"
          : undefined
      }
      className={
        netPnl == null
          ? ""
          : "oui-cursor-pointer oui-border-b oui-border-dashed oui-border-line-12"
      }
    >
      {netPnl ?? "--"}
    </Text.pnl>
  );

  if (record.netPnL == null) return text();

  return (
    <Tooltip
      // open={record.max_position_qty == 3.22}
      delayDuration={200}
      // @ts-ignore
      content={
        <Flex direction={"column"} itemAlign={"start"} className="oui-text-2xs">
          <Text intensity={80}>{t("positions.history.column.netPnl")}</Text>
          <Flex justify={"between"} width={"100%"} gap={2}>
            <Text intensity={54}>{t("common.realizedPnl")}</Text>
            <Text
              color={record.realized_pnl >= 0 ? "profit" : "lose"}
              className="oui-cursor-pointer"
            >
              {commifyOptional(formatNum.pnl(record.realized_pnl)?.toString())}
            </Text>
          </Flex>
          <Flex justify={"between"} width={"100%"} gap={2}>
            <Text intensity={54}>{t("funding.fundingFee")}</Text>
            <Text
              color={record.accumulated_funding_fee > 0 ? "lose" : "profit"}
              className="oui-cursor-pointer"
            >
              {commifyOptional(-record.accumulated_funding_fee)}
            </Text>
          </Flex>
          <Flex justify={"between"} width={"100%"} gap={2}>
            <Text intensity={54}>
              {t("positions.history.netPnl.tradingFee")}
            </Text>
            <Text
              color={record.trading_fee > 0 ? "lose" : "profit"}
              className="oui-cursor-pointer"
            >
              {commifyOptional(-record.trading_fee)}
            </Text>
          </Flex>
        </Flex>
      }
      className="oui-min-w-[204px] oui-bg-base-5"
      arrow={{
        className: "oui-fill-base-5",
      }}
    >
      <div>{text()}</div>
    </Tooltip>
  );
};

const LeverageBadge = (props: { symbol: string; leverage: number }) => {
  const { symbol, leverage } = props;

  return (
    <Badge color="neutral" size="xs">
      {leverage ? (
        <Text.numeral dp={0} size="2xs" unit="X">
          {leverage}
        </Text.numeral>
      ) : (
        <LeverageDisplay symbol={symbol} />
      )}
    </Badge>
  );
};

const LeverageDisplay = ({ symbol }: { symbol: string }) => {
  const leverage = useMaxLeverage(symbol);

  return (
    <Text.numeral dp={0} size="2xs" unit="X">
      {leverage}
    </Text.numeral>
  );
};
