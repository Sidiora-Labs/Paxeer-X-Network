import { Text } from '@chakra-ui/react';
import React from 'react';

import type { PaxeerXActivityItem } from 'types/api/paxeerX';

import { TableBody, TableColumnHeader, TableHeader, TableRoot, TableRow } from 'toolkit/chakra/table';
import Pagination from 'ui/shared/pagination/Pagination';
import { formatScanTableCount, SCAN_ROWS_PER_PAGE, ScanShowRows, ScanTableCard } from 'ui/shared/scan';
import TimeFormatToggle from 'ui/shared/time/TimeFormatToggle';

import ActivityListItem from './ActivityListItem';

export interface Props {
  items: Array<PaxeerXActivityItem>;
  isLoading?: boolean;
}

// One feed for chain-side and kernel-side activity, each row carrying its rung on the status ladder.
const ActivityList = ({ items, isLoading }: Props) => {
  const [ rowsPerPage, setRowsPerPage ] = React.useState(SCAN_ROWS_PER_PAGE[0]);
  const [ page, setPage ] = React.useState(1);

  const pageCount = Math.max(1, Math.ceil(items.length / rowsPerPage));
  const currentPage = Math.min(page, pageCount);
  const rows = items.slice((currentPage - 1) * rowsPerPage, currentPage * rowsPerPage);

  const handleNextPageClick = React.useCallback(() => setPage((value) => value + 1), []);
  const handlePrevPageClick = React.useCallback(() => setPage((value) => Math.max(1, value - 1)), []);
  const handleResetPage = React.useCallback(() => setPage(1), []);
  const handleRowsPerPageChange = React.useCallback((value: number) => {
    setRowsPerPage(value);
    setPage(1);
  }, []);

  const paginationNode = (
    <Pagination
      page={ currentPage }
      pageCount={ pageCount }
      onNextPageClick={ handleNextPageClick }
      onPrevPageClick={ handlePrevPageClick }
      resetPage={ handleResetPage }
      hasPages={ pageCount > 1 }
      hasNextPage={ currentPage < pageCount }
      canGoBackwards={ currentPage > 1 }
      isLoading={ Boolean(isLoading) }
      isVisible={ pageCount > 1 }
    />
  );

  return (
    <ScanTableCard
      title={ formatScanTableCount({ kind: 'total', value: items.length, itemsName: 'activity entries' }) }
      note="Chain-side and kernel-side entries in one feed, newest first"
      pagination={ paginationNode }
      showRows={ <ScanShowRows value={ rowsPerPage } onValueChange={ handleRowsPerPageChange } isLoading={ isLoading }/> }
    >
      { items.length === 0 ? (
        <Text color="text.secondary" px={ 4 } py={ 6 }>There is no Paxeer X activity for this account yet.</Text>
      ) : (
        <TableRoot variant="scan" minW="900px" data-label="paxeer-x-activity">
          <TableHeader>
            <TableRow>
              <TableColumnHeader width="20%">Action</TableColumnHeader>
              <TableColumnHeader width="30%">Transaction</TableColumnHeader>
              <TableColumnHeader width="20%">
                Block
                <TimeFormatToggle/>
              </TableColumnHeader>
              <TableColumnHeader width="20%" isNumeric>Amount</TableColumnHeader>
              <TableColumnHeader width="10%">Status</TableColumnHeader>
            </TableRow>
          </TableHeader>
          <TableBody>
            { rows.map((item) => (
              <ActivityListItem key={ `${ item.hash }-${ item.kind }-${ item.block_number }` } item={ item } isLoading={ isLoading }/>
            )) }
          </TableBody>
        </TableRoot>
      ) }
    </ScanTableCard>
  );
};

export default React.memo(ActivityList);
