import { Flex } from '@chakra-ui/react';
import { useRouter } from 'next/router';
import React from 'react';

import { route } from 'nextjs/routes';

import config from 'configs/app';
import useIsMobile from 'lib/hooks/useIsMobile';
import useIsMounted from 'lib/hooks/useIsMounted';
import getQueryParamString from 'lib/router/getQueryParamString';
import { INTERCHAIN_MESSAGE } from 'stubs/interchainIndexer';
import { generateListStub } from 'stubs/utils';
import { Link } from 'toolkit/chakra/link';
import RoutedTabs from 'toolkit/components/RoutedTabs/RoutedTabs';
import AddressTxsCrossChain from 'ui/crossChain/address/AddressTxsCrossChain';
import Pagination from 'ui/shared/pagination/Pagination';
import useQueryWithPages from 'ui/shared/pagination/useQueryWithPages';
import { formatScanTableCount, ScanTableCard } from 'ui/shared/scan';
import TxsWithAPISorting from 'ui/txs/TxsWithAPISorting';

import AddressCsvExportLink from './AddressCsvExportLink';
import AddressTxsFilter from './AddressTxsFilter';
import useAddressTxsQuery from './useAddressTxsQuery';

export const ADDRESS_TXS_TAB_IDS = [ 'txs_local' as const, 'txs_cross_chain' as const ];

interface Props {
  shouldRender?: boolean;
  isQueryEnabled?: boolean;
  txsCount?: number;
}

const AddressTxs = ({ shouldRender = true, isQueryEnabled = true, txsCount }: Props) => {
  const router = useRouter();
  const isMounted = useIsMounted();
  const isMobile = useIsMobile();
  const hash = getQueryParamString(router.query.hash);
  const tab = getQueryParamString(router.query.tab) as typeof ADDRESS_TXS_TAB_IDS[number] | 'txs' | undefined;

  const isLocalTab = tab === 'txs_local' || tab === 'txs';

  const localQuery = useAddressTxsQuery({
    addressHash: hash,
    enabled: isQueryEnabled && isLocalTab,
  });

  const crossChainQuery = useQueryWithPages({
    resourceName: 'interchainIndexer:address_messages',
    pathParams: { hash },
    options: {
      placeholderData: generateListStub<'interchainIndexer:address_messages'>(INTERCHAIN_MESSAGE, 50, { next_page_params: undefined }),
      enabled: isQueryEnabled && !isLocalTab,
    },
  });

  const handleTabValueChange = React.useCallback(({ value }: { value: string }) => {
    if (value === 'txs_local') {
      localQuery.setFilterValue(undefined);
    }
  }, [ localQuery ]);

  const txsLocalFilter = isLocalTab ? (
    <AddressTxsFilter
      initialValue={ localQuery.initialFilterValue }
      onFilterChange={ localQuery.onFilterChange }
      hasActiveFilter={ Boolean(localQuery.filterValue) }
      isLoading={ localQuery.query.pagination.isLoading }
    />
  ) : null;

  if (!isMounted || !shouldRender) {
    return null;
  }

  const localItemsNum = localQuery.query.data?.items.length;
  const localTitle = formatScanTableCount(
    txsCount !== undefined && localItemsNum !== undefined && localItemsNum < txsCount ?
      { kind: 'latest', value: txsCount, itemsName: 'transactions', shownValue: localItemsNum } :
      { kind: 'total', value: txsCount ?? localItemsNum ?? 0, itemsName: 'transactions' },
  );

  const crossChainItemsNum = crossChainQuery.data?.items.length;
  const crossChainTitle = formatScanTableCount({ kind: 'total', value: crossChainItemsNum ?? 0, itemsName: 'cross-chain messages' });

  const viewAllRow = (
    <Flex
      data-view-all
      justifyContent="center"
      alignItems="center"
      px={ 4 }
      py={ 3 }
      borderTopWidth="1px"
      borderStyle="solid"
      borderColor="border.divider"
    >
      <Link href={ route({ pathname: '/txs' }) } textStyle="xs" fontWeight="500" textTransform="uppercase">
        View all transactions →
      </Link>
    </Flex>
  );

  const localActions = !isMobile ? (
    <>
      { txsLocalFilter }
      <AddressCsvExportLink
        address={ hash }
        label="Download Page Data"
        params={{ type: 'transactions', filterType: 'address', filterValue: localQuery.filterValue }}
        isLoading={ localQuery.query.pagination.isLoading }
      />
    </>
  ) : null;

  const tabs = [
    {
      id: [ 'txs_local', 'txs' ],
      title: 'Txns',
      component: (
        <>
          <ScanTableCard
            title={ localTitle }
            actions={ localActions }
            pagination={ !isMobile ? <Pagination { ...localQuery.query.pagination }/> : null }
          >
            <TxsWithAPISorting
              filter={ txsLocalFilter }
              filterValue={ localQuery.filterValue }
              query={ localQuery.query }
              currentAddress={ hash }
              enableTimeIncrement
              socketType="address_txs"
              sorting={ localQuery.sort }
              setSort={ localQuery.setSort }
              showBlockInfo
              showTableViewButton
            />
            { viewAllRow }
          </ScanTableCard>
          <Flex justifyContent="flex-end" mt={ 3 }>
            <AddressCsvExportLink
              address={ hash }
              label="CSV Export"
              params={{ type: 'transactions', filterType: 'address', filterValue: localQuery.filterValue }}
              isLoading={ localQuery.query.pagination.isLoading }
            />
          </Flex>
        </>
      ),
    },
    config.features.crossChainTxs.isEnabled && {
      id: 'txs_cross_chain',
      title: 'Cross-chain txns',
      component: (
        <ScanTableCard
          title={ crossChainTitle }
          pagination={ !isMobile ? <Pagination { ...crossChainQuery.pagination }/> : null }
        >
          <AddressTxsCrossChain
            pagination={ crossChainQuery.pagination }
            items={ crossChainQuery.data?.items }
            isLoading={ crossChainQuery.isPlaceholderData }
            isError={ crossChainQuery.isError }
            currentAddress={ hash }
          />
        </ScanTableCard>
      ),
    },
  ].filter(Boolean);

  return (
    <RoutedTabs
      variant="pill"
      size="sm"
      tabs={ tabs }
      onValueChange={ handleTabValueChange }
      defaultTabId="txs_local"
    />
  );
};

export default React.memo(AddressTxs);
