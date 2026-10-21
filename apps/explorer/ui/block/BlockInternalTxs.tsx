import { Box } from '@chakra-ui/react';
import React from 'react';

import InternalTxsList from 'ui/internalTxs/InternalTxsList';
import InternalTxsTable from 'ui/internalTxs/InternalTxsTable';
import DataListDisplay from 'ui/shared/DataListDisplay';
import Pagination from 'ui/shared/pagination/Pagination';
import type { QueryWithPagesResult } from 'ui/shared/pagination/useQueryWithPages';
import { formatScanTableCount, ScanTableCard } from 'ui/shared/scan';

interface Props {
  query: QueryWithPagesResult<'general:block_internal_txs'>;
  itemsCount?: number;
  top?: number;
}

const BlockInternalTxs = ({ query, itemsCount, top }: Props) => {
  const { data, isPlaceholderData, isError } = query;

  const content = data?.items ? (
    <>
      <Box hideFrom="lg">
        <InternalTxsList data={ data.items } isLoading={ isPlaceholderData } showBlockInfo={ false }/>
      </Box>
      <Box hideBelow="lg">
        <InternalTxsTable data={ data.items } isLoading={ isPlaceholderData } top={ top } showBlockInfo={ false }/>
      </Box>
    </>
  ) : null;

  const title = formatScanTableCount({
    kind: 'total',
    value: itemsCount ?? data?.items.length ?? 0,
    itemsName: 'internal transactions',
  });

  const pagination = query.pagination.isVisible ? <Pagination { ...query.pagination }/> : undefined;

  return (
    <ScanTableCard title={ title } pagination={ pagination }>
      <DataListDisplay
        isError={ isError }
        itemsNum={ data?.items.length }
        emptyText="There are no internal transactions."
      >
        { content }
      </DataListDisplay>
    </ScanTableCard>
  );
};

export default React.memo(BlockInternalTxs);
