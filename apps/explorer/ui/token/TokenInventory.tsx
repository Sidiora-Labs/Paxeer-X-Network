import { Flex, Grid, Text } from '@chakra-ui/react';
import type { UseQueryResult } from '@tanstack/react-query';
import React from 'react';

import type { TokenInfo } from 'types/api/token';

import type { ResourceError } from 'lib/api/resources';
import { AddressHighlightProvider } from 'lib/contexts/addressHighlight';
import useIsMobile from 'lib/hooks/useIsMobile';
import useIsMounted from 'lib/hooks/useIsMounted';
import DataListDisplay from 'ui/shared/DataListDisplay';
import AddressEntity from 'ui/shared/entities/address/AddressEntity';
import Pagination from 'ui/shared/pagination/Pagination';
import type { QueryWithPagesResult } from 'ui/shared/pagination/useQueryWithPages';
import ResetIconButton from 'ui/shared/ResetIconButton';
import { formatScanTableCount, ScanShowRows, ScanTableCard, SCAN_ROWS_PER_PAGE } from 'ui/shared/scan';

import TokenInventoryItem from './TokenInventoryItem';

const DEFAULT_ROWS_TO_SHOW = 50;

type Props = {
  inventoryQuery: QueryWithPagesResult<'general:token_inventory'>;
  tokenQuery: UseQueryResult<TokenInfo, ResourceError<unknown>>;
  ownerFilter?: string;
  shouldRender?: boolean;
  inventoryCount?: number;
};

const TokenInventory = ({ inventoryQuery, tokenQuery, ownerFilter, shouldRender = true, inventoryCount }: Props) => {
  const isMobile = useIsMobile();
  const isMounted = useIsMounted();
  const [ rowsToShow, setRowsToShow ] = React.useState(DEFAULT_ROWS_TO_SHOW);

  const resetOwnerFilter = React.useCallback(() => {
    inventoryQuery.onFilterChange({});
  }, [ inventoryQuery ]);

  if (!isMounted || !shouldRender) {
    return null;
  }

  const ownerFilterComponent = ownerFilter ? (
    <Flex alignItems="center" flexWrap="wrap" columnGap={ 2 } data-inventory-owner-filter>
      <Text whiteSpace="nowrap">Filtered by owner</Text>
      <Flex alignItems="center">
        <AddressEntity address={{ hash: ownerFilter }} truncation={ isMobile ? 'constant' : 'none' }/>
        <ResetIconButton onClick={ resetOwnerFilter }/>
      </Flex>
    </Flex>
  ) : null;

  const items = inventoryQuery.data?.items.slice(0, rowsToShow);
  const token = tokenQuery.data;

  const content = items && token ? (
    <AddressHighlightProvider>
      <Grid
        w="100%"
        p={ 4 }
        columnGap={{ base: 3, lg: 6 }}
        rowGap={{ base: 3, lg: 6 }}
        gridTemplateColumns={{ base: 'repeat(2, calc((100% - 12px)/2))', lg: 'repeat(auto-fill, minmax(210px, 1fr))' }}
        data-inventory-grid
      >
        { items.map((item, index) => (
          <TokenInventoryItem
            key={ item.id + '_' + index + (inventoryQuery.isPlaceholderData ? '_' + 'placeholder' : '') }
            item={ item }
            isLoading={ inventoryQuery.isPlaceholderData || tokenQuery.isPlaceholderData }
            token={ token }
          />
        )) }
      </Grid>
    </AddressHighlightProvider>
  ) : null;

  const itemsNum = items?.length ?? 0;
  const title = formatScanTableCount((() => {
    if (inventoryCount !== undefined) {
      return itemsNum < inventoryCount ?
        { kind: 'latest' as const, value: inventoryCount, itemsName: 'tokens', shownValue: itemsNum } :
        { kind: 'total' as const, value: inventoryCount, itemsName: 'tokens' };
    }

    return inventoryQuery.pagination.hasNextPage ?
      { kind: 'more_than' as const, value: itemsNum, itemsName: 'tokens' } :
      { kind: 'total' as const, value: itemsNum, itemsName: 'tokens' };
  })());

  return (
    <ScanTableCard
      title={ title }
      actions={ ownerFilterComponent }
      pagination={ inventoryQuery.pagination.isVisible ? <Pagination { ...inventoryQuery.pagination }/> : null }
      showRows={ (
        <ScanShowRows
          value={ rowsToShow }
          onValueChange={ setRowsToShow }
          options={ SCAN_ROWS_PER_PAGE }
          label="Show"
          suffix="Records"
          isLoading={ inventoryQuery.isPlaceholderData }
        />
      ) }
    >
      <DataListDisplay
        isError={ inventoryQuery.isError }
        itemsNum={ itemsNum }
        emptyText="There are no tokens."
        hasActiveFilters={ Boolean(ownerFilter) }
        emptyStateProps={{
          description: 'No tokens found for the selected owner.',
        }}
      >
        { content }
      </DataListDisplay>
    </ScanTableCard>
  );
};

export default TokenInventory;
