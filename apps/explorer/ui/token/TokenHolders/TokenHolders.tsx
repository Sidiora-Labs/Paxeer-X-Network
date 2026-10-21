import { Box } from '@chakra-ui/react';
import React from 'react';

import type { TokenInfo } from 'types/api/token';

import useIsMobile from 'lib/hooks/useIsMobile';
import useIsMounted from 'lib/hooks/useIsMounted';
import AddressCsvExportLink from 'ui/address/AddressCsvExportLink';
import DataFetchAlert from 'ui/shared/DataFetchAlert';
import DataListDisplay from 'ui/shared/DataListDisplay';
import Pagination from 'ui/shared/pagination/Pagination';
import type { QueryWithPagesResult } from 'ui/shared/pagination/useQueryWithPages';
import { formatScanTableCount, ScanShowRows, ScanTableCard, SCAN_ROWS_PER_PAGE } from 'ui/shared/scan';

import TokenHoldersList from './TokenHoldersList';
import TokenHoldersTable from './TokenHoldersTable';

const DEFAULT_ROWS_TO_SHOW = 50;

type Props = {
  token?: TokenInfo;
  holdersQuery: QueryWithPagesResult<'general:token_holders'>;
  shouldRender?: boolean;
  holdersCount?: number;
};

const TokenHolders = ({ holdersQuery, token, shouldRender = true, holdersCount }: Props) => {
  const isMobile = useIsMobile();
  const isMounted = useIsMounted();
  const [ rowsToShow, setRowsToShow ] = React.useState(DEFAULT_ROWS_TO_SHOW);

  if (!isMounted || !shouldRender) {
    return null;
  }

  if (holdersQuery.isError) {
    return <DataFetchAlert/>;
  }

  const items = holdersQuery.data?.items.slice(0, rowsToShow);

  const content = items && token ? (
    <>
      <Box display={{ base: 'none', lg: 'block' }}>
        <TokenHoldersTable
          data={ items }
          token={ token }
          top={ 0 }
          isLoading={ holdersQuery.isPlaceholderData }
        />
      </Box>
      <Box display={{ base: 'block', lg: 'none' }}>
        <TokenHoldersList
          data={ items }
          token={ token }
          isLoading={ holdersQuery.isPlaceholderData }
        />
      </Box>
    </>
  ) : null;

  const itemsNum = items?.length ?? 0;
  const title = formatScanTableCount((() => {
    if (holdersCount !== undefined) {
      return itemsNum < holdersCount ?
        { kind: 'latest' as const, value: holdersCount, itemsName: 'holders', shownValue: itemsNum } :
        { kind: 'total' as const, value: holdersCount, itemsName: 'holders' };
    }

    return holdersQuery.pagination.hasNextPage ?
      { kind: 'more_than' as const, value: itemsNum, itemsName: 'holders' } :
      { kind: 'total' as const, value: itemsNum, itemsName: 'holders' };
  })());

  const actions = !isMobile && token ? (
    <AddressCsvExportLink
      address={ token.address_hash }
      label="Download Page Data"
      params={{ type: 'holders' }}
      isLoading={ holdersQuery.pagination.isLoading }
    />
  ) : null;

  return (
    <ScanTableCard
      title={ title }
      actions={ actions }
      pagination={ holdersQuery.pagination.isVisible ? <Pagination { ...holdersQuery.pagination }/> : null }
      showRows={ (
        <ScanShowRows
          value={ rowsToShow }
          onValueChange={ setRowsToShow }
          options={ SCAN_ROWS_PER_PAGE }
          label="Show"
          suffix="Records"
          isLoading={ holdersQuery.pagination.isLoading }
        />
      ) }
    >
      <DataListDisplay
        isError={ holdersQuery.isError }
        itemsNum={ itemsNum }
        emptyText="There are no holders for this token."
      >
        { content }
      </DataListDisplay>
    </ScanTableCard>
  );
};

export default TokenHolders;
