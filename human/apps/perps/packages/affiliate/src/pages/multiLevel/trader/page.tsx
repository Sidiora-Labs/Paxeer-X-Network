import { useEffect, useMemo, useState } from "react";
import { VolBarChart } from "@paxeer-network/chart";
import { useTranslation } from "@paxeer-network/i18n";
import {
  Column,
  DataTable,
  DatePicker,
  Divider,
  Flex,
  ListView,
  Select,
  Spinner,
  Text,
  usePagination,
  useScreen,
} from "@paxeer-network/ui";
import { commifyOptional, Decimal } from "@paxeer-network/utils";
import { useReferralContext } from "../../../provider";
import { DateRange } from "../../../utils/types";
import {
  calendarDate,
  calendarDateString,
  calendarDayDateRange,
  joinRebateVolume,
  rebateChartData,
  RebateTableRow,
  sumRebates,
  sumVolume,
  utcDayWindow,
  utcYesterday,
} from "./traderData";
import { useTraderRebates, useTraderVolume } from "./useTraderData";

type ChartPeriod = "7" | "30" | "90";

const MAX_TABLE_RANGE_DAYS = 90;

const money = (value?: number, digits = 2) =>
  value === undefined
    ? "--"
    : commifyOptional(value, {
        fix: digits,
        fallback: "0",
        padEnd: true,
        prefix: "$",
      });

export const MultiLevelTraderPage = () => {
  const { t } = useTranslation();
  const { refereeKickbackInfo, referralInfo } = useReferralContext();
  const yesterday = utcYesterday();
  const [summaryPeriod, setSummaryPeriod] = useState<ChartPeriod>("90");
  const summaryRange = utcDayWindow(Number(summaryPeriod), yesterday);
  const summaryRebates = useTraderRebates(summaryRange);
  const summaryVolume = useTraderVolume(summaryRange);
  const [chartPeriod, setChartPeriod] = useState<ChartPeriod>("90");
  const chartRange = utcDayWindow(Number(chartPeriod), yesterday);
  const chartRebates = useTraderRebates(chartRange);
  const [dateRange, setDateRange] = useState<DateRange>(() =>
    calendarDayDateRange(yesterday, MAX_TABLE_RANGE_DAYS),
  );
  const tableRange = {
    startDate: calendarDateString(dateRange.from!),
    endDate: calendarDateString(dateRange.to!),
  };
  const tableRebates = useTraderRebates(tableRange);
  const tableVolume = useTraderVolume(tableRange);
  const { pagination, setPage } = usePagination();
  const { isMobile } = useScreen();

  useEffect(() => {
    setPage(1);
  }, [tableRange.startDate, tableRange.endDate, setPage]);

  const rebateTotal = summaryRebates.rows
    ? sumRebates(summaryRebates.rows)
    : undefined;
  const volumeTotal = summaryVolume.data
    ? sumVolume(summaryVolume.data)
    : undefined;
  const rebateRate = refereeKickbackInfo?.referee_kickback_rate;
  const rateText =
    rebateRate === undefined
      ? "--"
      : `${new Decimal(rebateRate).mul(100).toDecimalPlaces(2).toString()}%`;
  const chartData = useMemo(
    () =>
      rebateChartData(chartRebates.rows ?? [], Number(chartPeriod), yesterday),
    [chartRebates.rows, chartPeriod, yesterday],
  );
  const tableRows = useMemo(
    () => joinRebateVolume(tableRebates.rows ?? [], tableVolume.data ?? []),
    [tableRebates.rows, tableVolume.data],
  );
  const columns: Column<RebateTableRow>[] = [
    { title: t("common.date"), dataIndex: "date" },
    {
      title: `${t("affiliate.trader.rebate")} (USDC)`,
      dataIndex: "referee_rebate",
      render: (value: number) => money(value, 6),
    },
    {
      title: `${t("affiliate.trader.tradingVol")} (USDC)`,
      dataIndex: "volume",
      render: (value?: number) => money(tableVolume.error ? undefined : value),
    },
  ];
  const periodOptions = [
    { label: t("common.select.7d"), value: "7" },
    { label: t("common.select.30d"), value: "30" },
    { label: t("common.select.90d"), value: "90" },
  ];
  const summaryPastLabel =
    summaryPeriod === "7"
      ? t("affiliate.trader.past7d")
      : summaryPeriod === "30"
        ? t("affiliate.trader.past30d")
        : t("affiliate.trader.past90d");

  return (
    <Flex
      id="oui-affiliate-multiLevel-trader-page"
      width="100%"
      direction="column"
      className="oui-gap-8 md:oui-gap-10 oui-tabular-nums"
    >
      <Flex
        width="100%"
        direction="column"
        itemAlign="stretch"
        gap={6}
        className="oui-rounded-2xl oui-border oui-border-line-6 oui-bg-base-9 oui-p-5 md:oui-p-6"
      >
        <Flex width="100%" justify="between" itemAlign="center">
          <Text size="lg">{t("affiliate.summary")}</Text>
          <div className="oui-min-w-14 oui-shrink-0">
            <Select.options
              size="xs"
              value={summaryPeriod}
              onValueChange={(value) => setSummaryPeriod(value as ChartPeriod)}
              options={periodOptions}
            />
          </div>
        </Flex>
        <div className="oui-grid oui-w-full oui-grid-cols-1 oui-gap-4 md:oui-grid-cols-3 md:oui-gap-6">
          <Flex
            direction="column"
            justify="between"
            itemAlign="start"
            className="oui-min-h-[160px] oui-rounded-2xl oui-bg-base-contrast-4 oui-p-5 md:oui-min-h-[180px]"
          >
            <Text size="sm" intensity={54}>
              {t("affiliate.trader.yourRebateRate")}
            </Text>
            <Text className="oui-text-3xl oui-leading-10">{rateText}</Text>
            <Text size="sm" intensity={54}>
              {referralInfo?.referee_info?.referer_code
                ? t("affiliate.referredBy", {
                    name: referralInfo.referee_info.referer_code,
                  })
                : "\u00a0"}
            </Text>
          </Flex>
          <Flex
            direction="column"
            justify="between"
            itemAlign="start"
            className="oui-min-h-[160px] oui-rounded-2xl oui-bg-base-contrast-4 oui-p-5 md:oui-min-h-[180px]"
          >
            <Text size="sm" intensity={54}>
              {t("affiliate.trader.refereeRebate")} (USDC)
            </Text>
            {rebateTotal === undefined || summaryRebates.error ? (
              <Text size="3xl">--</Text>
            ) : (
              <Text.numeral
                rule="human"
                prefix="$"
                dp={2}
                size="3xl"
                className="oui-text-trade-profit"
              >
                {rebateTotal}
              </Text.numeral>
            )}
            <Text size="sm" intensity={54}>
              {summaryPastLabel}
            </Text>
          </Flex>
          <Flex
            direction="column"
            justify="between"
            itemAlign="start"
            className="oui-min-h-[160px] oui-rounded-2xl oui-bg-base-contrast-4 oui-p-5 md:oui-min-h-[180px]"
          >
            <Text size="sm" intensity={54}>
              {t("affiliate.trader.tradingVolume")}
            </Text>
            {volumeTotal === undefined || summaryVolume.error ? (
              <Text size="3xl">--</Text>
            ) : (
              <Text.numeral rule="human" prefix="$" dp={2} size="3xl">
                {volumeTotal}
              </Text.numeral>
            )}
            <Text size="sm" intensity={54}>
              {summaryPastLabel}
            </Text>
          </Flex>
        </div>
      </Flex>

      <Flex
        width="100%"
        direction="column"
        itemAlign="stretch"
        gap={6}
        className="oui-rounded-2xl oui-border oui-border-line-6 oui-bg-base-9 oui-p-5 md:oui-p-6"
      >
        <Flex width="100%" justify="between" itemAlign="center">
          <Text size="lg">{t("affiliate.trader.refereeRebate")} (USDC)</Text>
          <div className="oui-min-w-14 oui-shrink-0">
            <Select.options
              size="xs"
              value={chartPeriod}
              onValueChange={(value) => setChartPeriod(value as ChartPeriod)}
              options={periodOptions}
            />
          </div>
        </Flex>
        {chartRebates.error ? (
          <Text size="sm" intensity={54}>
            {t("common.somethingWentWrong")}
          </Text>
        ) : chartRebates.isLoading ? (
          <Flex
            justify="center"
            itemAlign="center"
            className="oui-h-[200px] md:oui-h-[240px]"
          >
            <Spinner />
          </Flex>
        ) : (
          <div className="oui-h-[200px] oui-w-full md:oui-h-[240px]">
            <VolBarChart
              data={chartData}
              colors={{ fill: "rgb(var(--oui-color-success))" }}
              className="oui-h-full oui-w-full"
              tooltip={{ rm: Decimal.ROUND_DOWN, dp: 6 }}
            />
          </div>
        )}
      </Flex>

      <Flex
        width="100%"
        direction="column"
        itemAlign="stretch"
        gap={4}
        className="oui-rounded-2xl oui-border oui-border-line-6 oui-bg-base-9 oui-p-5 md:oui-p-6"
      >
        <Text size="lg">{t("affiliate.trader.myRebates")}</Text>
        <div className="oui-border-y oui-border-line-6 oui-py-3">
          <div className="oui-w-fit">
            <DatePicker.range
              size="xs"
              value={dateRange}
              maxDays={MAX_TABLE_RANGE_DAYS}
              disabled={{ after: calendarDate(yesterday) }}
              onChange={(range) => setDateRange(range)}
            />
          </div>
        </div>
        {tableRebates.error ? (
          <Text size="sm" intensity={54}>
            {t("common.somethingWentWrong")}
          </Text>
        ) : isMobile && (tableRebates.isLoading || tableVolume.isLoading) ? (
          <Flex
            justify="center"
            itemAlign="center"
            className="oui-min-h-[160px]"
          >
            <Spinner />
          </Flex>
        ) : isMobile ? (
          <ListView<RebateTableRow, RebateTableRow[]>
            dataSource={tableRows}
            renderItem={(item) => (
              <Flex direction="column" gap={3} className="oui-py-3">
                <Flex justify="between" width="100%">
                  <Text intensity={54}>{t("common.date")}</Text>
                  <Text>{item.date}</Text>
                </Flex>
                <Flex justify="between" width="100%">
                  <Text intensity={54}>
                    {t("affiliate.trader.rebate")} (USDC)
                  </Text>
                  <Text>{money(item.referee_rebate, 6)}</Text>
                </Flex>
                <Flex justify="between" width="100%">
                  <Text intensity={54}>
                    {t("affiliate.trader.tradingVol")} (USDC)
                  </Text>
                  <Text>
                    {money(tableVolume.error ? undefined : item.volume)}
                  </Text>
                </Flex>
                <Divider />
              </Flex>
            )}
          />
        ) : (
          <DataTable
            bordered
            loading={tableRebates.isLoading || tableVolume.isLoading}
            columns={columns}
            dataSource={tableRows}
            pagination={pagination}
            manualPagination={false}
          />
        )}
      </Flex>
    </Flex>
  );
};
