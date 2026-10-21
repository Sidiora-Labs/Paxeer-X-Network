import type { BoxProps } from '@chakra-ui/react';
import { Box } from '@chakra-ui/react';
import BigNumber from 'bignumber.js';
import React from 'react';

import type { Route } from 'nextjs-routes';
import { route } from 'nextjs-routes';

import config from 'configs/app';
import useApiQuery from 'lib/api/useApiQuery';
import { useMultichainContext } from 'lib/contexts/multichain';
import getStatsLabelFromTitle from 'lib/stats/getStatsLabelFromTitle';
import { HOMEPAGE_STATS } from 'stubs/stats';
import { TXS_STATS, TXS_STATS_MICROSERVICE } from 'stubs/tx';
import { Link } from 'toolkit/chakra/link';
import { thinsp } from 'toolkit/utils/htmlEntities';
import type { ScanStatDelta } from 'ui/shared/scan';
import { ScanStatCard } from 'ui/shared/scan';
import calculateUsdValue from 'ui/shared/value/calculateUsdValue';

interface Props extends BoxProps {}

const DELTA_ACCURACY = 2;

function toShareDelta(
  part: number | string | null | undefined,
  whole: number | string | null | undefined,
  direction: ScanStatDelta['direction'],
): ScanStatDelta | undefined {
  if (part === null || part === undefined || whole === null || whole === undefined) {
    return undefined;
  }

  const wholeBn = BigNumber(whole);
  const partBn = BigNumber(part);

  if (!wholeBn.isFinite() || !partBn.isFinite() || wholeBn.isLessThanOrEqualTo(0)) {
    return undefined;
  }

  return { value: `${ partBn.div(wholeBn).times(100).dp(DELTA_ACCURACY).toFormat() }%`, direction };
}

function toChangeDelta(change: number | null | undefined): ScanStatDelta | undefined {
  if (change === null || change === undefined || !Number.isFinite(change)) {
    return undefined;
  }

  return {
    value: `${ BigNumber(Math.abs(change)).dp(DELTA_ACCURACY).toFormat() }%`,
    direction: change < 0 ? 'down' : 'up',
  };
}

const TxsStats = (props: Props) => {
  const multichainContext = useMultichainContext();

  const chainConfig = multichainContext?.chain.app_config || config;
  const isStatsFeatureEnabled = chainConfig.features.stats.isEnabled;
  const rollupFeature = chainConfig.features.rollup;
  const isOptimisticRollup = rollupFeature.isEnabled && rollupFeature.type === 'optimistic';
  const isArbitrumRollup = rollupFeature.isEnabled && rollupFeature.type === 'arbitrum';

  const txsStatsQuery = useApiQuery('stats:pages_transactions', {
    queryOptions: {
      enabled: isStatsFeatureEnabled,
      placeholderData: isStatsFeatureEnabled ? TXS_STATS_MICROSERVICE : undefined,
    },
  });

  const txsStatsApiQuery = useApiQuery('general:txs_stats', {
    queryOptions: {
      enabled: !isStatsFeatureEnabled,
      placeholderData: !isStatsFeatureEnabled ? TXS_STATS : undefined,
    },
  });

  const statsQuery = useApiQuery('general:stats', {
    queryOptions: {
      placeholderData: HOMEPAGE_STATS,
    },
  });

  if ((isStatsFeatureEnabled && !txsStatsQuery.data) || (!isStatsFeatureEnabled && !txsStatsApiQuery.data)) {
    return null;
  }

  const isLoading = isStatsFeatureEnabled ? txsStatsQuery.isPlaceholderData : txsStatsApiQuery.isPlaceholderData;

  const txCount24h = isStatsFeatureEnabled ? txsStatsQuery.data?.transactions_24h?.value : txsStatsApiQuery.data?.transactions_count_24h;
  const operationalTxns24hArbitrum = isArbitrumRollup && isStatsFeatureEnabled ? txsStatsQuery.data?.operational_transactions_24h?.value : null;
  const operationalTxns24hOptimistic = isOptimisticRollup && isStatsFeatureEnabled ? txsStatsQuery.data?.op_stack_operational_transactions_24h?.value : null;

  const pendingTxns = isStatsFeatureEnabled ? txsStatsQuery.data?.pending_transactions_30m?.value : txsStatsApiQuery.data?.pending_transactions_count;

  // in microservice data, fee values are already divided by 10^decimals
  const txFeeSum24h = isStatsFeatureEnabled ?
    Number(txsStatsQuery.data?.transactions_fee_24h?.value) :
    Number(txsStatsApiQuery.data?.transaction_fees_sum_24h) / (10 ** chainConfig.chain.currency.decimals);

  const avgFee = isStatsFeatureEnabled ? txsStatsQuery.data?.average_transactions_fee_24h?.value : txsStatsApiQuery.data?.transaction_fees_avg_24h;

  const txFeeAvg = avgFee ? calculateUsdValue({
    amount: avgFee,
    exchangeRate: statsQuery.data?.coin_price,
    // in microservice data, fee values are already divided by 10^decimals
    decimals: isStatsFeatureEnabled ? '0' : String(chainConfig.chain.currency.decimals),
  }) : null;

  const pendingPeriod = isStatsFeatureEnabled ? '30min' : '1h';
  const coinPriceDelta = toChangeDelta(statsQuery.data?.coin_price_change_percentage);
  const coinPriceHint = `The ${ chainConfig.chain.currency.symbol } price the fiat figure is taken at moved by this much over the last day`;

  const chartRoute = (id: string): Route | undefined => chainConfig.features.stats.isEnabled ?
    {
      pathname: '/stats/[id]' as const,
      query: { id, ...(multichainContext?.chain.id ? { chain_id: multichainContext.chain.id } : {}) },
    } :
    undefined;

  const items = [
    txCount24h ? {
      label: `${ txsStatsQuery.data?.transactions_24h?.title ?
        getStatsLabelFromTitle(txsStatsQuery.data.transactions_24h.title) :
        'Transactions' } (24h)`,
      value: Number(txCount24h).toLocaleString(),
      delta: toShareDelta(txCount24h, statsQuery.data?.total_transactions, 'up'),
      hint: 'The last day\'s transactions as a share of every transaction the chain has recorded',
      href: chartRoute('newTxns'),
    } : null,
    operationalTxns24hArbitrum ? {
      label: `${ txsStatsQuery.data?.operational_transactions_24h?.title ?
        getStatsLabelFromTitle(txsStatsQuery.data.operational_transactions_24h.title) :
        'Daily op txns' } (24h)`,
      value: Number(operationalTxns24hArbitrum).toLocaleString(),
      delta: undefined,
      hint: undefined,
      href: undefined,
    } : null,
    operationalTxns24hOptimistic ? {
      label: `${ txsStatsQuery.data?.op_stack_operational_transactions_24h?.title ?
        getStatsLabelFromTitle(txsStatsQuery.data.op_stack_operational_transactions_24h.title) :
        'Daily op txns' } (24h)`,
      value: Number(operationalTxns24hOptimistic).toLocaleString(),
      delta: undefined,
      hint: undefined,
      href: undefined,
    } : null,
    pendingTxns ? {
      label: `${ txsStatsQuery.data?.pending_transactions_30m?.title ?
        getStatsLabelFromTitle(txsStatsQuery.data.pending_transactions_30m.title) :
        'Pending transactions' } (last ${ pendingPeriod })`,
      value: Number(pendingTxns).toLocaleString(),
      delta: toShareDelta(pendingTxns, txCount24h, 'down'),
      hint: 'The transactions still waiting as a share of the transactions the chain settled over the last day',
      href: undefined,
    } : null,
    txFeeSum24h != null && Number.isFinite(txFeeSum24h) ? {
      label: `${ txsStatsQuery.data?.transactions_fee_24h?.title ?
        getStatsLabelFromTitle(txsStatsQuery.data.transactions_fee_24h.title) :
        'Total transaction fee' } (24h)`,
      value: `${ txFeeSum24h.toLocaleString(undefined, { maximumFractionDigits: 2 }) }${ thinsp }${ chainConfig.chain.currency.symbol }`,
      delta: coinPriceDelta,
      hint: coinPriceHint,
      href: chartRoute('txnsFee'),
    } : null,
    txFeeAvg ? {
      label: `${ txsStatsQuery.data?.average_transactions_fee_24h?.title ?
        getStatsLabelFromTitle(txsStatsQuery.data.average_transactions_fee_24h.title) :
        'Avg. transaction fee' } (24h)`,
      value: txFeeAvg.usdStr ? `$${ txFeeAvg.usdStr }` : `${ txFeeAvg.valueStr }${ thinsp }${ chainConfig.chain.currency.symbol }`,
      delta: coinPriceDelta,
      hint: coinPriceHint,
      href: chartRoute('averageTxnFee'),
    } : null,
  ].filter(item => item !== null);

  if (items.length === 0) {
    return null;
  }

  return (
    <Box
      display="grid"
      gridTemplateColumns={{
        base: 'minmax(0, 1fr)',
        md: 'repeat(2, minmax(0, 1fr))',
        lg: `repeat(${ items.length }, minmax(0, 1fr))`,
      }}
      rowGap={ 3 }
      columnGap={ 3 }
      mb={ 6 }
      data-scan-stat-row
      { ...props }
    >
      { items.map((item) => {
        const card = (
          <ScanStatCard
            label={ item.label }
            value={ item.value }
            delta={ item.delta }
            hint={ item.hint }
            isLoading={ isLoading }
          />
        );

        return item.href && !isLoading ? (
          <Link key={ item.label } href={ route(item.href) } variant="plain" display="block">{ card }</Link>
        ) : (
          <React.Fragment key={ item.label }>{ card }</React.Fragment>
        );
      }) }
    </Box>
  );
};

export default React.memo(TxsStats);
