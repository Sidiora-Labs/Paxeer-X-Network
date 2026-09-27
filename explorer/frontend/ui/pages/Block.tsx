import { chakra, Flex } from '@chakra-ui/react';
import { capitalize, pickBy } from 'es-toolkit';
import { useRouter } from 'next/router';
import React from 'react';

import { route, routeParams } from 'nextjs/routes';

import config from 'configs/app';
import { useMultichainContext } from 'lib/contexts/multichain';
import throwOnAbsentParamError from 'lib/errors/throwOnAbsentParamError';
import throwOnResourceLoadError from 'lib/errors/throwOnResourceLoadError';
import getNetworkValidatorTitle from 'lib/networks/getNetworkValidatorTitle';
import getQueryParamString from 'lib/router/getQueryParamString';
import { Link } from 'toolkit/chakra/link';
import { Skeleton } from 'toolkit/chakra/skeleton';
import BlockCeloEpochTag from 'ui/block/BlockCeloEpochTag';
import BlockDeposits from 'ui/block/BlockDeposits';
import BlockDetails from 'ui/block/BlockDetails';
import BlockInternalTxs from 'ui/block/BlockInternalTxs';
import BlockWithdrawals from 'ui/block/BlockWithdrawals';
import useBlockBlobTxsQuery from 'ui/block/useBlockBlobTxsQuery';
import useBlockDepositsQuery from 'ui/block/useBlockDepositsQuery';
import useBlockInternalTxsQuery from 'ui/block/useBlockInternalTxsQuery';
import useBlockQuery from 'ui/block/useBlockQuery';
import useBlockTxsQuery from 'ui/block/useBlockTxsQuery';
import useBlockWithdrawalsQuery from 'ui/block/useBlockWithdrawalsQuery';
import TextAd from 'ui/shared/ad/TextAd';
import ServiceDegradationWarning from 'ui/shared/alerts/ServiceDegradationWarning';
import BlockPendingUpdateAlert from 'ui/shared/block/BlockPendingUpdateAlert';
import AddressEntity from 'ui/shared/entities/address/AddressEntity';
import * as BlockEntity from 'ui/shared/entities/block/BlockEntity';
import IconSvg from 'ui/shared/IconSvg';
import NetworkExplorers from 'ui/shared/NetworkExplorers';
import PageTitle from 'ui/shared/Page/PageTitle';
import type { ScanSectionTabItem } from 'ui/shared/scan';
import { ScanSectionTabs } from 'ui/shared/scan';
import TxsWithFrontendSorting from 'ui/txs/TxsWithFrontendSorting';

interface BlockTab extends ScanSectionTabItem {
  component: React.ReactNode;
}

const beaconChainFeature = config.features.beaconChain;
const apiDocsFeature = config.features.apiDocs;

const BlockPageContent = () => {
  const router = useRouter();
  const heightOrHash = getQueryParamString(router.query.height_or_hash);
  const tab = getQueryParamString(router.query.tab);
  const multichainContext = useMultichainContext();

  const blockQuery = useBlockQuery({ heightOrHash });
  const blockTxsQuery = useBlockTxsQuery({ heightOrHash, blockQuery, tab });
  const blockWithdrawalsQuery = useBlockWithdrawalsQuery({ heightOrHash, blockQuery, tab });
  const blockDepositsQuery = useBlockDepositsQuery({ heightOrHash, blockQuery, tab });
  const blockBlobTxsQuery = useBlockBlobTxsQuery({ heightOrHash, blockQuery, tab });
  const blockInternalTxsQuery = useBlockInternalTxsQuery({ heightOrHash, blockQuery, tab });

  const tabs: Array<BlockTab> = React.useMemo(() => ([
    {
      id: 'index',
      title: 'Overview',
      component: (
        <>
          <Flex rowGap={{ base: 1, lg: 2 }} mb={{ base: 3, lg: 6 }} flexDir="column">
            { blockQuery.isDegradedData && <ServiceDegradationWarning isLoading={ blockQuery.isPlaceholderData }/> }
            { blockQuery.data?.is_pending_update && <BlockPendingUpdateAlert/> }
          </Flex>
          <BlockDetails query={ blockQuery }/>
        </>
      ),
    },
    {
      id: 'txs',
      title: 'Transactions',
      component: (
        <>
          { blockTxsQuery.isDegradedData && <ServiceDegradationWarning isLoading={ blockTxsQuery.isPlaceholderData } mb={{ base: 3, lg: 6 }}/> }
          <TxsWithFrontendSorting query={ blockTxsQuery } showBlockInfo={ false }/>
        </>
      ),
    },
    {
      id: 'internal_txs',
      title: 'Internal txns',
      component: (
        <>
          { blockTxsQuery.isDegradedData && <ServiceDegradationWarning isLoading={ blockTxsQuery.isPlaceholderData } mb={{ base: 3, lg: 6 }}/> }
          <BlockInternalTxs query={ blockInternalTxsQuery } itemsCount={ blockQuery.data?.internal_transactions_count }/>
        </>
      ),
    },
    config.features.dataAvailability.isEnabled && blockQuery.data?.blob_transactions_count ?
      {
        id: 'blob_txs',
        title: 'Blob txns',
        component: (
          <TxsWithFrontendSorting query={ blockBlobTxsQuery } showBlockInfo={ false }/>
        ),
      } : null,
    beaconChainFeature.isEnabled && !beaconChainFeature.withdrawalsOnly && Boolean(blockQuery.data?.beacon_deposits_count) ?
      {
        id: 'deposits',
        title: 'Deposits',
        component: (
          <>
            { blockDepositsQuery.isDegradedData && <ServiceDegradationWarning isLoading={ blockDepositsQuery.isPlaceholderData } mb={{ base: 3, lg: 6 }}/> }
            <BlockDeposits blockDepositsQuery={ blockDepositsQuery } itemsCount={ blockQuery.data?.beacon_deposits_count }/>
          </>
        ),
      } : null,
    config.features.beaconChain.isEnabled && Boolean(blockQuery.data?.withdrawals_count) ?
      {
        id: 'withdrawals',
        title: 'Withdrawals',
        component: (
          <>
            { blockWithdrawalsQuery.isDegradedData &&
              <ServiceDegradationWarning isLoading={ blockWithdrawalsQuery.isPlaceholderData } mb={{ base: 3, lg: 6 }}/> }
            <BlockWithdrawals blockWithdrawalsQuery={ blockWithdrawalsQuery } itemsCount={ blockQuery.data?.withdrawals_count }/>
          </>
        ),
      } : null,
  ].filter(Boolean) as Array<BlockTab>), [
    blockBlobTxsQuery, blockDepositsQuery, blockInternalTxsQuery, blockQuery, blockTxsQuery, blockWithdrawalsQuery,
  ]);

  const activeTab = tabs.find(({ id }) => id === tab) ?? tabs[0];

  const handleTabChange = React.useCallback((value: string) => {
    const queryForPathname = pickBy(router.query, (_, key) => router.pathname.includes(`[${ String(key) }]`));

    router.push(
      { pathname: router.pathname, query: { ...queryForPathname, tab: value } },
      undefined,
      { shallow: true },
    );
  }, [ router ]);

  throwOnAbsentParamError(heightOrHash);

  if (blockQuery.isError) {
    if (!blockQuery.isDegradedData && blockQuery.error.status === 404 && !heightOrHash.startsWith('0x') && blockQuery.isFutureBlock) {
      const url = routeParams({ pathname: '/block/countdown/[height]', query: { height: heightOrHash } }, multichainContext);
      router.push(url, undefined, { shallow: true });
      return null;
    } else {
      throwOnResourceLoadError(blockQuery);
    }
  }

  const titleText = (() => {
    switch (blockQuery.data?.type) {
      case 'reorg':
        return 'Reorged block';

      case 'uncle':
        return 'Uncle block';

      default:
        return 'Block';
    }
  })();

  const beforeTitleElement = multichainContext?.chain ? (
    <BlockEntity.Icon variant="heading" chain={ multichainContext.chain } isLoading={ blockQuery.isPlaceholderData }/>
  ) : null;

  const titleContentAfter = (
    <>
      <Skeleton loading={ blockQuery.isPlaceholderData }>
        <chakra.span textStyle="lg" color="text.secondary" data-block-number>
          #{ blockQuery.data?.height }
        </chakra.span>
      </Skeleton>
      <BlockCeloEpochTag blockQuery={ blockQuery }/>
    </>
  );

  const titleSecondRow = (
    <>
      { !config.UI.views.block.hiddenFields?.miner && blockQuery.data?.miner && (
        <Skeleton
          loading={ blockQuery.isPlaceholderData }
          fontFamily="heading"
          display="flex"
          minW={ 0 }
          columnGap={ 2 }
          fontWeight={ 500 }
        >
          <chakra.span flexShrink={ 0 }>
            { capitalize(getNetworkValidatorTitle()) }
          </chakra.span>
          <AddressEntity address={ blockQuery.data.miner }/>
        </Skeleton>
      ) }
      <NetworkExplorers
        type="block"
        pathParam={ heightOrHash }
        ml={{ base: config.UI.views.block.hiddenFields?.miner ? 0 : 3, lg: 'auto' }}
      />
    </>
  );

  const apiEntry = apiDocsFeature.isEnabled ? (
    <Link href={ route({ pathname: '/api-docs' }, multichainContext) } textStyle="sm" display="inline-flex" alignItems="center" data-api-entry>
      <IconSvg name="API" boxSize={ 4 } mr={ 1 }/>
      API
    </Link>
  ) : null;

  return (
    <>
      <TextAd mb={ 6 }/>
      <PageTitle
        title={ titleText }
        beforeTitle={ beforeTitleElement }
        contentAfter={ titleContentAfter }
        secondRow={ titleSecondRow }
        isLoading={ blockQuery.isPlaceholderData }
      />
      <Flex flexDir="column" rowGap={{ base: 3, lg: 4 }}>
        <ScanSectionTabs
          items={ tabs.map(({ id, title }) => ({ id, title })) }
          value={ activeTab?.id ?? 'index' }
          onValueChange={ handleTabChange }
          rightSlot={ apiEntry }
        />
        { activeTab?.component }
      </Flex>
    </>
  );
};

export default BlockPageContent;
