import { Flex } from '@chakra-ui/react';
import { useRouter } from 'next/router';
import React from 'react';

import { route } from 'nextjs/routes';

import config from 'configs/app';
import useIsMobile from 'lib/hooks/useIsMobile';
import useIsMounted from 'lib/hooks/useIsMounted';
import getQueryParamString from 'lib/router/getQueryParamString';
import { INTERCHAIN_TRANSFER } from 'stubs/interchainIndexer';
import { generateListStub } from 'stubs/utils';
import { Link } from 'toolkit/chakra/link';
import RoutedTabs from 'toolkit/components/RoutedTabs/RoutedTabs';
import TokenTransfersCrossChainContent from 'ui/crossChain/transfers/TokenTransfersCrossChainContent';
import Pagination from 'ui/shared/pagination/Pagination';
import useQueryWithPages from 'ui/shared/pagination/useQueryWithPages';
import { formatScanTableCount, ScanTableCard } from 'ui/shared/scan';
import TokenTransferFilter from 'ui/shared/TokenTransfer/TokenTransferFilter';

import AddressAdvancedFilterLink from './AddressAdvancedFilterLink';
import AddressCsvExportLink from './AddressCsvExportLink';
import AddressTokenTransfersLocal from './AddressTokenTransfersLocal';
import useAddressTokenTransfersQuery from './useAddressTokenTransfersQuery';

export const ADDRESS_TOKEN_TRANSFERS_TAB_IDS = [ 'token_transfers_local' as const, 'token_transfers_cross_chain' as const ];

interface Props {
  shouldRender?: boolean;
  isQueryEnabled?: boolean;
  transfersCount?: number;
  // for tests only
  overloadCount?: number;
}

const AddressTokenTransfers = ({ shouldRender = true, overloadCount, isQueryEnabled = true, transfersCount }: Props) => {
  const router = useRouter();
  const isMounted = useIsMounted();
  const isMobile = useIsMobile();
  const hash = getQueryParamString(router.query.hash);
  const tab = getQueryParamString(router.query.tab) as typeof ADDRESS_TOKEN_TRANSFERS_TAB_IDS[number] | 'token_transfers' | undefined;

  const isLocalTab = tab === 'token_transfers_local' || tab === 'token_transfers';

  const localQuery = useAddressTokenTransfersQuery({
    currentAddress: hash,
    enabled: isQueryEnabled && isLocalTab,
  });

  const crossChainQuery = useQueryWithPages({
    resourceName: 'interchainIndexer:address_transfers',
    pathParams: { hash },
    options: {
      placeholderData: generateListStub<'interchainIndexer:address_transfers'>(INTERCHAIN_TRANSFER, 50, { next_page_params: undefined }),
      enabled: isQueryEnabled && !isLocalTab,
    },
  });

  const handleTabValueChange = React.useCallback(({ value }: { value: string }) => {
    if (value === 'token_transfers_local') {
      localQuery.setFilters({ type: [], filter: undefined });
    }
  }, [ localQuery ]);

  if (!isMounted || !shouldRender) {
    return null;
  }

  const localItemsNum = localQuery.query.data?.items.length;
  const localTitle = formatScanTableCount(
    transfersCount !== undefined && localItemsNum !== undefined && localItemsNum < transfersCount ?
      { kind: 'latest', value: transfersCount, itemsName: 'token transfers', shownValue: localItemsNum } :
      { kind: 'total', value: transfersCount ?? localItemsNum ?? 0, itemsName: 'token transfers' },
  );

  const crossChainItemsNum = crossChainQuery.data?.items.length;
  const crossChainTitle = formatScanTableCount({ kind: 'total', value: crossChainItemsNum ?? 0, itemsName: 'cross-chain transfers' });

  const numActiveFilters = (localQuery.filters.type?.length || 0) + (localQuery.filters.filter ? 1 : 0);

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
      <Link href={ route({ pathname: '/token-transfers' }) } textStyle="xs" fontWeight="500" textTransform="uppercase">
        View all token transfers →
      </Link>
    </Flex>
  );

  const localActions = !isMobile ? (
    <>
      <TokenTransferFilter
        defaultTypeFilters={ localQuery.filters.type }
        onTypeFilterChange={ localQuery.onTypeFilterChange }
        appliedFiltersNum={ numActiveFilters }
        withAddressFilter
        onAddressFilterChange={ localQuery.onAddressFilterChange }
        defaultAddressFilter={ localQuery.filters.filter }
        isLoading={ localQuery.query.isPlaceholderData }
      />
      <AddressAdvancedFilterLink
        isLoading={ localQuery.query.isPlaceholderData }
        address={ hash }
        typeFilter={ localQuery.filters.type }
        directionFilter={ localQuery.filters.filter }
      />
      <AddressCsvExportLink
        address={ hash }
        label="Download Page Data"
        params={{ type: 'token-transfers', filterType: 'address', filterValue: localQuery.filters.filter }}
        isLoading={ localQuery.query.isPlaceholderData }
      />
    </>
  ) : null;

  const tabs = [
    {
      id: [ 'token_transfers_local', 'token_transfers' ],
      title: 'Transfers',
      component: (
        <>
          <ScanTableCard
            title={ localTitle }
            actions={ localActions }
            pagination={ !isMobile ? <Pagination { ...localQuery.query.pagination }/> : null }
          >
            <AddressTokenTransfersLocal
              query={ localQuery.query }
              filters={ localQuery.filters }
              onTypeFilterChange={ localQuery.onTypeFilterChange }
              onAddressFilterChange={ localQuery.onAddressFilterChange }
              addressHash={ hash }
              overloadCount={ overloadCount }
            />
            { viewAllRow }
          </ScanTableCard>
          <Flex justifyContent="flex-end" mt={ 3 }>
            <AddressCsvExportLink
              address={ hash }
              label="CSV Export"
              params={{ type: 'token-transfers', filterType: 'address', filterValue: localQuery.filters.filter }}
              isLoading={ localQuery.query.isPlaceholderData }
            />
          </Flex>
        </>
      ),
    },
    config.features.crossChainTxs.isEnabled && {
      id: 'token_transfers_cross_chain',
      title: 'Cross-chain transfers',
      component: (
        <ScanTableCard
          title={ crossChainTitle }
          pagination={ <Pagination { ...crossChainQuery.pagination }/> }
        >
          <TokenTransfersCrossChainContent
            items={ crossChainQuery.data?.items }
            isLoading={ crossChainQuery.isPlaceholderData }
            isError={ crossChainQuery.isError }
            pagination={ crossChainQuery.pagination }
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
    />
  );
};

export default React.memo(AddressTokenTransfers);
