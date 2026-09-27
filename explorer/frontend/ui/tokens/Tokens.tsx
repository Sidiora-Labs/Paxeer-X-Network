import { Box } from '@chakra-ui/react';
import React from 'react';

import type { TokenInfo } from 'types/api/token';
import type { TokensSortingValue } from 'types/api/tokens';
import type { AggregatedTokenInfo } from 'types/client/multichainAggregator';

import getItemIndex from 'lib/getItemIndex';
import DataFetchAlert from 'ui/shared/DataFetchAlert';
import DataListDisplay from 'ui/shared/DataListDisplay';
import type { QueryWithPagesResult } from 'ui/shared/pagination/useQueryWithPages';
import { formatScanTableCount, ScanTableCard } from 'ui/shared/scan';

import TokensListItem from './TokensListItem';
import TokensTable from './TokensTable';

interface Props {
  query: QueryWithPagesResult<'general:tokens'> | QueryWithPagesResult<'general:tokens_bridged'> | QueryWithPagesResult<'multichainAggregator:tokens'>;
  onSortChange?: (value: TokensSortingValue) => void;
  sort?: TokensSortingValue;
  actionBar?: React.ReactNode;
  hasActiveFilters: boolean;
  description?: React.ReactNode;
  actions?: React.ReactNode;
  pagination?: React.ReactNode;
  showRows?: React.ReactNode;
  rowsCount?: number;
  coinPrice?: string | null;
}

const countReputableTokens = (items: Array<TokenInfo | AggregatedTokenInfo>) =>
  items.filter((item) => item.reputation !== 'scam').length;

const Tokens = ({
  query,
  onSortChange,
  sort,
  actionBar,
  description,
  hasActiveFilters,
  actions,
  pagination,
  showRows,
  rowsCount,
  coinPrice,
}: Props) => {

  const { isError, isPlaceholderData, data, pagination: paginationParams } = query;

  if (isError) {
    return <DataFetchAlert/>;
  }

  const allItems = data?.items ?? [];
  const items = rowsCount === undefined ? allItems : allItems.slice(0, rowsCount);

  const title = formatScanTableCount({
    kind: paginationParams.hasNextPage ? 'more_than' : 'total',
    value: allItems.length === 0 ? 0 : getItemIndex(allItems.length - 1, paginationParams.page),
    itemsName: 'token contracts',
  });

  const note = allItems.length > 0 ?
    `Showing ${ countReputableTokens(allItems).toLocaleString() } tokens with an ok or a neutral reputation` :
    undefined;

  const content = data?.items ? (
    <>
      <Box hideFrom="lg">
        { description }
        { items.map((item, index) => {
          const chainIds = 'chain_infos' in item ? Object.keys(item.chain_infos).join(',') : undefined;

          return (
            <TokensListItem
              key={ item.address_hash + (isPlaceholderData ? index : '') + (chainIds ? chainIds : '') }
              token={ item }
              index={ index }
              page={ paginationParams.page }
              isLoading={ isPlaceholderData }
              coinPrice={ coinPrice }
            />
          );
        }) }
      </Box>
      <Box hideBelow="lg">
        { description }
        <TokensTable
          items={ items }
          page={ paginationParams.page }
          isLoading={ isPlaceholderData }
          setSorting={ onSortChange }
          sorting={ sort }
          coinPrice={ coinPrice }
        />
      </Box>
    </>
  ) : null;

  return (
    <ScanTableCard
      title={ title }
      note={ note }
      actions={ actions }
      pagination={ pagination }
      showRows={ showRows }
    >
      <DataListDisplay
        isError={ isError }
        itemsNum={ items.length }
        emptyText="There are no tokens."
        hasActiveFilters={ hasActiveFilters }
        emptyStateProps={{
          term: 'token',
        }}
        actionBar={ actionBar }
        showActionBarIfEmpty
      >
        { content }
      </DataListDisplay>
    </ScanTableCard>
  );
};

export default Tokens;
