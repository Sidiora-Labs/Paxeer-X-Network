import { Box, Grid, GridItem } from '@chakra-ui/react';
import React from 'react';

import config from 'configs/app';
import useApiQuery from 'lib/api/useApiQuery';
import { BLOCK } from 'stubs/block';
import { HOMEPAGE_STATS, HOMEPAGE_STATS_MICROSERVICE } from 'stubs/stats';
import IconSvg from 'ui/shared/IconSvg';
import NativeTokenIcon from 'ui/shared/NativeTokenIcon';

import StatsDegraded from './fallbacks/StatsDegraded';
import Highlights from './Highlights';
import type { HighlightsItemProps } from './highlights/HighlightsItem';
import ChainIndicatorsChart from './indicators/ChainIndicatorsChart';
import useChartDataQuery from './indicators/useChartDataQuery';
import { isHomeStatsItemEnabled } from './utils';

const isStatsFeatureEnabled = config.features.stats.isEnabled;

const HISTORY_DAYS = 14;
const SECONDS_PER_DAY = 24 * 60 * 60;

// The latest-block counter follows the cadence the live block list flushes on, so the history column
// carries its own query behind a memo boundary and the chart is left alone by every flush that only
// moves the counters beside it.
const StatsHistory = React.memo(function StatsHistory({ isLoading }: { isLoading: boolean }) {
  const chartQuery = useChartDataQuery('daily_txs');

  return (
    <ChainIndicatorsChart
      isLoading={ isLoading }
      title={ `${ config.chain.name } transaction history in ${ HISTORY_DAYS } days` }
      chartQuery={ chartQuery }
      days={ HISTORY_DAYS }
    />
  );
});

const Stats = () => {
  // data from stats microservice is prioritized over data from stats api
  const statsQuery = useApiQuery('stats:pages_main', {
    queryOptions: {
      refetchOnMount: false,
      placeholderData: isStatsFeatureEnabled ? HOMEPAGE_STATS_MICROSERVICE : undefined,
      enabled: isStatsFeatureEnabled,
    },
  });

  const apiQuery = useApiQuery('general:stats', {
    queryOptions: {
      refetchOnMount: false,
      placeholderData: HOMEPAGE_STATS,
    },
  });

  const blocksQuery = useApiQuery('general:homepage_blocks', {
    queryOptions: {
      placeholderData: [ BLOCK ],
    },
  });

  const isLoading = statsQuery.isPlaceholderData || apiQuery.isPlaceholderData || blocksQuery.isPlaceholderData;

  const apiData = apiQuery.data;
  const statsData = statsQuery.data;
  const latestBlock = blocksQuery.data?.[0];

  const coinItems: Array<HighlightsItemProps> = React.useMemo(() => {
    const items: Array<HighlightsItemProps> = [];

    if (typeof apiData?.coin_price === 'string') {
      items.push({
        id: 'coin_price',
        label: `${ config.chain.currency.symbol } price`,
        value: '$' + Number(apiData.coin_price).toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 6 }),
        delta: typeof apiData.coin_price_change_percentage === 'number' ? {
          value: `${ apiData.coin_price_change_percentage > 0 ? '+' : '' }${ apiData.coin_price_change_percentage }%`,
          direction: apiData.coin_price_change_percentage >= 0 ? 'up' : 'down',
        } : undefined,
        icon: <NativeTokenIcon boxSize={ 5 }/>,
        isLoading,
      });
    }

    if (typeof apiData?.market_cap === 'string') {
      items.push({
        id: 'market_cap',
        label: `${ config.chain.currency.symbol } market cap`,
        value: '$' + Number(apiData.market_cap).toLocaleString(undefined, { maximumFractionDigits: 2 }),
        icon: <IconSvg name="globe" boxSize={ 5 } color="icon.secondary"/>,
        isLoading,
      });
    }

    return items;
  }, [ apiData, isLoading ]);

  // Each counter carries its own memo, so the flush that moves the latest block leaves the transaction
  // counter beside it with the very same props and nothing of it re-renders.
  const totalTxsItem: HighlightsItemProps | undefined = React.useMemo(() => {
    const totalTxs = statsData?.total_transactions?.value || apiData?.total_transactions;
    const txsPerDay = statsData?.yesterday_transactions?.value || apiData?.transactions_today;

    if (!totalTxs) {
      return undefined;
    }

    const item: HighlightsItemProps = {
      id: 'total_txs',
      label: 'Transactions',
      value: Number(totalTxs).toLocaleString(undefined, { maximumFractionDigits: 2, notation: 'compact' }),
      secondary: txsPerDay ?
        `${ (Number(txsPerDay) / SECONDS_PER_DAY).toLocaleString(undefined, { maximumFractionDigits: 1 }) } TPS` :
        undefined,
      icon: <IconSvg name="transactions" boxSize={ 5 } color="icon.secondary"/>,
      href: { pathname: '/txs' as const },
      isLoading,
    };

    return isHomeStatsItemEnabled({ id: 'total_txs', label: item.label, value: item.value }) ? item : undefined;
  }, [ statsData, apiData, isLoading ]);

  const latestBlockItem: HighlightsItemProps | undefined = React.useMemo(() => {
    if (!latestBlock) {
      return undefined;
    }

    const blockTime = (() => {
      if (statsData?.average_block_time?.value) {
        return Number(statsData.average_block_time.value);
      }

      if (apiData?.average_block_time !== undefined) {
        return apiData.average_block_time / 1000;
      }

      return undefined;
    })();

    const item: HighlightsItemProps = {
      id: 'total_blocks',
      label: 'Latest block',
      value: latestBlock.height.toLocaleString(),
      secondary: blockTime !== undefined ? `${ blockTime.toFixed(1) }s` : undefined,
      icon: <IconSvg name="block" boxSize={ 5 } color="icon.secondary"/>,
      href: { pathname: '/blocks' as const },
      isLoading,
    };

    return isHomeStatsItemEnabled({ id: 'total_blocks', label: item.label, value: item.value }) ? item : undefined;
  }, [ statsData, apiData, latestBlock, isLoading ]);

  const chainItems: Array<HighlightsItemProps> = React.useMemo(
    () => [ totalTxsItem, latestBlockItem ].filter((item): item is HighlightsItemProps => Boolean(item)),
    [ totalTxsItem, latestBlockItem ],
  );

  if (apiQuery.isError || statsQuery.isError) {
    return <StatsDegraded/>;
  }

  const hasChart = config.UI.homepage.charts.includes('daily_txs');

  if (coinItems.length === 0 && chainItems.length === 0 && !hasChart) {
    return null;
  }

  return (
    <Box
      data-label="home-stats"
      bgColor="bg.surface"
      borderWidth="1px"
      borderStyle="solid"
      borderColor="border.divider"
      borderRadius="md"
      boxShadow="card"
      overflow="hidden"
    >
      <Grid templateColumns={{ base: '1fr', lg: 'repeat(3, minmax(0, 1fr))' }}>
        <GridItem
          data-label="home-stats-coin"
          borderBottomWidth={{ base: '1px', lg: '0' }}
          borderRightWidth={{ base: '0', lg: '1px' }}
          borderStyle="solid"
          borderColor="border.divider"
        >
          <Highlights items={ coinItems }/>
        </GridItem>
        <GridItem
          data-label="home-stats-chain"
          borderBottomWidth={{ base: '1px', lg: '0' }}
          borderRightWidth={{ base: '0', lg: '1px' }}
          borderStyle="solid"
          borderColor="border.divider"
        >
          <Highlights items={ chainItems }/>
        </GridItem>
        <GridItem data-label="home-stats-history" px={{ base: 4, lg: 5 }} py={{ base: 3, lg: 4 }}>
          { hasChart && <StatsHistory isLoading={ isLoading }/> }
        </GridItem>
      </Grid>
    </Box>
  );
};

export default Stats;
