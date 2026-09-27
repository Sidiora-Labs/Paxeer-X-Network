import { Box, chakra, Flex } from '@chakra-ui/react';
import React, { useEffect } from 'react';

import { Resolution } from '@blockscout/stats-types';
import type { StatsIntervalIds } from 'types/client/stats';

import { route, type Route } from 'nextjs-routes';

import { IconButton } from 'toolkit/chakra/icon-button';
import { Link } from 'toolkit/chakra/link';
import { Skeleton } from 'toolkit/chakra/skeleton';
import { Tooltip } from 'toolkit/chakra/tooltip';
import { ChartWidgetContent, useChartZoom } from 'toolkit/components/charts';
import { Hint } from 'toolkit/components/Hint/Hint';
import { useChartsConfig, useDefaultBarColor } from 'ui/shared/chart/config';
import useChartQuery from 'ui/shared/chart/useChartQuery';
import IconSvg from 'ui/shared/IconSvg';
import { STATS_INTERVALS } from 'ui/stats/constants';

type Props = {
  id: string;
  title: string;
  description: string;
  interval: StatsIntervalIds;
  onLoadingError: () => void;
  isPlaceholderData: boolean;
  className?: string;
  href?: Route;
};

export function formatChartValue(value: number, units?: string): string {
  const formatted = value.toLocaleString(undefined, { maximumFractionDigits: 4 });

  return units ? `${ formatted } ${ units }` : formatted;
}

const ChartWidgetContainer = ({
  id,
  title,
  description,
  interval,
  onLoadingError,
  isPlaceholderData,
  className,
  href,
}: Props) => {
  const { items, lineQuery } = useChartQuery(id, Resolution.DAY, interval, !isPlaceholderData);
  const chartsConfig = useChartsConfig();
  const { zoomRange, handleZoom, handleZoomReset } = useChartZoom();
  const barColor = useDefaultBarColor();

  useEffect(() => {
    if (lineQuery.isError) {
      onLoadingError();
    }
  }, [ lineQuery.isError, onLoadingError ]);

  const units = lineQuery.data?.info?.units;

  const charts = React.useMemo(() => {
    if (!lineQuery.data?.info || !items) {
      return [];
    }

    return [
      {
        id: lineQuery.data?.info?.id,
        name: 'Value',
        items,
        charts: chartsConfig,
        units: lineQuery.data.info.units,
      },
    ];
  }, [ lineQuery.data?.info, items, chartsConfig ]);

  const currentValue = React.useMemo(() => {
    const lastItem = items?.[items.length - 1];

    if (!lastItem || !Number.isFinite(lastItem.value)) {
      return undefined;
    }

    return formatChartValue(lastItem.value, units);
  }, [ items, units ]);

  const isLoading = lineQuery.isPlaceholderData;
  const hasNonEmptyCharts = charts.some((chart) => chart.items && chart.items.length > 2);

  return (
    <Box
      className={ className }
      data-chart-card={ id }
      display="flex"
      flexDirection="column"
      bg="bg.surface"
      borderWidth="1px"
      borderStyle="solid"
      borderColor="border.divider"
      borderRadius="md"
      boxShadow="card"
      px={ 4 }
      py={ 3 }
      minH="300px"
    >
      <Flex data-chart-card-header alignItems="flex-start" justifyContent="space-between" columnGap={ 3 }>
        <Flex alignItems="center" columnGap={ 1.5 } minW={ 0 }>
          <Skeleton loading={ isLoading } textStyle="sm" fontWeight="500" color="text.primary">
            <chakra.span data-chart-title>
              { title } ({ STATS_INTERVALS[interval].shortTitle })
            </chakra.span>
          </Skeleton>
          { description && <Hint label={ description } isLoading={ isLoading } boxSize={ 4 }/> }
        </Flex>
        <Flex alignItems="center" columnGap={ 2 } flexShrink={ 0 }>
          { zoomRange && (
            <Tooltip content="Reset zoom">
              <IconButton
                aria-label="Reset zoom"
                size="md"
                variant="icon_background"
                onClick={ handleZoomReset }
              >
                <IconSvg name="repeat" boxSize={ 5 }/>
              </IconButton>
            </Tooltip>
          ) }
          { href && (
            <Link href={ route(href) } data-chart-view textStyle="xs" fontWeight="500" display="inline-flex" alignItems="center" columnGap={ 1 }>
              View
              <IconSvg name="arrows/east-mini" boxSize={ 3 }/>
            </Link>
          ) }
        </Flex>
      </Flex>

      { currentValue !== undefined && (
        <Skeleton loading={ isLoading } mt={ 1 } textStyle="heading.sm" fontWeight="500" color="text.primary" w="fit-content">
          <chakra.span data-chart-value>{ currentValue }</chakra.span>
        </Skeleton>
      ) }

      <Box
        data-chart-surface
        display="flex"
        flexDirection="column"
        flexGrow={ 1 }
        mt={ 3 }
        minH="230px"
        css={{
          '& svg text': { color: 'text.secondary' },
          '--chart-bar-color': barColor,
        }}
      >
        <ChartWidgetContent
          charts={ charts }
          isError={ lineQuery.isError }
          isLoading={ isLoading }
          empty={ !hasNonEmptyCharts }
          handleZoom={ handleZoom }
          zoomRange={ zoomRange }
        />
      </Box>
    </Box>
  );
};

export default chakra(ChartWidgetContainer);
