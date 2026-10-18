/* eslint-disable @typescript-eslint/no-explicit-any */
import React from "react";
import {
  PnlAreaChart,
  PnLBarChart,
  VolBarChart,
  VolumeAreaChart,
} from "@paxeer-network/chart";
import { useTranslation } from "@paxeer-network/i18n";
import { EMPTY_LIST } from "@paxeer-network/types";
import {
  Card,
  Grid,
  Box,
  Statistic,
  Text,
  Flex,
  Tooltip,
  cn,
} from "@paxeer-network/ui";
import { Decimal } from "@paxeer-network/utils";
import type { UsePerformanceScriptReturn } from "./performance.script";

const LazyPeriodTitle = React.lazy(() =>
  import("../shared/periodHeader").then((mod) => {
    return { default: mod.PeriodTitle };
  }),
);

export type PerformanceUIProps = {
  // periodTypes: string[];
  // period: string;
  // onPeriodChange: (period: string) => void;
} & UsePerformanceScriptReturn;

const formatForChart = (value: number | null | undefined, dp: number = 2) => {
  if (value == null) return 0;
  const decimal = new Decimal(value);
  return Number(decimal.toFixed(dp, Decimal.ROUND_HALF_UP));
};

export const PerformanceUI: React.FC<PerformanceUIProps> = (props) => {
  const {
    periodTypes,
    period,
    onPeriodChange,
    aggregateValue,
    invisible,
    visible,
    volumeUpdateDate,
    curPeriod,
  } = props;
  const { t } = useTranslation();

  return (
    <Card
      title={
        <React.Suspense fallback={null}>
          <LazyPeriodTitle
            onPeriodChange={onPeriodChange}
            periodTypes={periodTypes}
            period={period}
            title={t("portfolio.overview.performance")}
          />
        </React.Suspense>
      }
      id="portfolio-overview-performance"
    >
      <Grid cols={3} gap={4}>
        <Box gradient="neutral" r="md" px={4} py={2} angle={184}>
          <Statistic
            label={
              <LabelWithHint
                label={t("portfolio.overview.performance.roi", {
                  period: curPeriod,
                })}
                hint={t("portfolio.overview.performance.roi.tooltip")}
              />
            }
            valueProps={{ rule: "percentages", coloring: true, visible }}
          >
            {invisible ? "--" : aggregateValue.roi}
          </Statistic>
        </Box>
        <Box gradient="neutral" r="md" px={4} py={2} angle={184}>
          <Statistic
            label={
              <LabelWithHint
                label={t("portfolio.overview.performance.pnl", {
                  period: curPeriod,
                })}
                hint={t("portfolio.overview.performance.pnl.tooltip")}
              />
            }
            valueProps={{ coloring: true, showIdentifier: true, visible }}
          >
            {invisible ? "--" : aggregateValue.pnl}
          </Statistic>
        </Box>
        <Box gradient="neutral" r="md" px={4} py={2} angle={184}>
          <Statistic
            classNames={{
              label: "oui-w-full",
            }}
            label={
              <Flex justify={"between"}>
                <span>
                  <LabelWithHint
                    label={t("portfolio.overview.performance.volume", {
                      period: curPeriod,
                    })}
                    hint={t("portfolio.overview.performance.volume.tooltip")}
                  />
                </span>
                <span>{volumeUpdateDate}</span>
              </Flex>
            }
            valueProps={{ visible }}
          >
            {invisible ? "--" : aggregateValue.vol}
          </Statistic>
        </Box>
      </Grid>
      <Grid cols={2} gap={4}>
        <PerformancePnL
          data={props.data ?? EMPTY_LIST}
          invisible={props.invisible}
        />
        <CumulativePnlChart
          data={props.data ?? EMPTY_LIST}
          invisible={props.invisible || (props.data?.length ?? 0) <= 2}
        />
      </Grid>
      <Grid cols={2} gap={4}>
        <DailyVolumeChart
          data={props.data ?? EMPTY_LIST}
          invisible={props.invisible}
        />
        <CumulativeVolumeChart
          data={props.data ?? EMPTY_LIST}
          invisible={props.invisible || (props.data?.length ?? 0) <= 2}
        />
      </Grid>
    </Card>
  );
};

type LabelWithHintProps = {
  label: string;
  hint?: React.ReactNode;
};

const LabelWithHint: React.FC<LabelWithHintProps> = (props) => {
  const { label, hint } = props;
  return (
    <Tooltip
      open={hint ? undefined : false}
      content={hint}
      className="oui-max-w-[240px] oui-bg-base-6 "
      arrow={{ className: "oui-fill-base-6" }}
      delayDuration={300}
    >
      <Text
        size="xs"
        intensity={36}
        className={cn(
          hint &&
            "oui-cursor-pointer oui-border-b oui-border-dashed oui-border-line-12",
        )}
      >
        {label}
      </Text>
    </Tooltip>
  );
};

export const PerformancePnL: React.FC<{
  data: ReadonlyArray<any> | any[];
  invisible: boolean;
}> = (props) => {
  const { t } = useTranslation();
  const chartData = React.useMemo(
    () =>
      (props.data ?? []).map((d) => ({
        ...d,
        pnl: formatForChart(d.pnl),
      })),
    [props.data],
  );
  return (
    <Box mt={4} height={"188px"}>
      <Text as="div" size="sm" className="oui-mb-3">
        {t("portfolio.overview.performance.dailyPnl")}
      </Text>
      <Box r="md" className="oui-h-[188px] oui-border oui-border-line-4">
        <PnLBarChart
          data={chartData}
          invisible={props.invisible || (chartData?.length ?? 0) <= 2}
        />
      </Box>
    </Box>
  );
};

export const CumulativePnlChart: React.FC<{
  data: ReadonlyArray<any> | any[];
  invisible: boolean;
}> = (props) => {
  const { t } = useTranslation();
  return (
    <Box mt={4}>
      <Text as="div" size="sm" className="oui-mb-3">
        {t("portfolio.overview.performance.cumulativePnl")}
      </Text>
      <Box r="md" className="oui-h-[188px] oui-border oui-border-line-4">
        <PnlAreaChart
          data={props.data}
          invisible={props.invisible || (props.data?.length ?? 0) <= 2}
        />
      </Box>
    </Box>
  );
};

export const DailyVolumeChart: React.FC<{
  data: ReadonlyArray<any> | any[];
  invisible: boolean;
}> = (props) => {
  const { t } = useTranslation();
  const volumeData = React.useMemo(
    () =>
      (props.data ?? []).map((d) => ({
        ...d,
        volume: formatForChart(d.perp_volume ?? 0),
      })),
    [props.data],
  );
  return (
    <Box mt={4} height={"188px"}>
      <Text as="div" size="sm" className="oui-mb-3">
        {t("portfolio.overview.performance.dailyVolume")}
      </Text>
      <Box r="md" className="oui-h-[188px] oui-border oui-border-line-4">
        <VolBarChart
          data={volumeData}
          invisible={props.invisible || volumeData.length <= 2}
          responsiveContainerProps={{
            width: "100%",
            height: 188,
          }}
        />
      </Box>
    </Box>
  );
};

export const CumulativeVolumeChart: React.FC<{
  data: ReadonlyArray<any> | any[];
  invisible: boolean;
}> = (props) => {
  const { t } = useTranslation();
  return (
    <Box mt={4}>
      <Text as="div" size="sm" className="oui-mb-3">
        {t("portfolio.overview.performance.cumulativeVolume")}
      </Text>
      <Box r="md" className="oui-h-[188px] oui-border oui-border-line-4">
        <VolumeAreaChart
          data={props.data ?? EMPTY_LIST}
          invisible={props.invisible || (props.data?.length ?? 0) <= 2}
        />
      </Box>
    </Box>
  );
};
