import { Text } from '@chakra-ui/react';
import React from 'react';

import type { PaxeerXActivityItem } from 'types/api/paxeerX';

import { TableBody, TableColumnHeader, TableHeader, TableRoot, TableRow } from 'toolkit/chakra/table';
import Pagination from 'ui/shared/pagination/Pagination';
import { ScanTableCard } from 'ui/shared/scan';
import TimeFormatToggle from 'ui/shared/time/TimeFormatToggle';

import ActivityListItem from './ActivityListItem';

export interface Props {
  items: Array<PaxeerXActivityItem>;
  isLoading?: boolean;
  page: number;
  total: number | null;
  hasNextPage: boolean;
  canGoBackwards: boolean;
  onNextPageClick: () => void;
  onPrevPageClick: () => void;
  resetPage: () => void;
}

// One feed for chain-side and kernel-side activity, each row carrying its rung on the status ladder.
const ActivityList = ({ items, isLoading, page, total, hasNextPage, canGoBackwards,
  onNextPageClick, onPrevPageClick, resetPage }: Props) => {
  const paginationNode = (
    <Pagination
      page={ page }
      onNextPageClick={ onNextPageClick }
      onPrevPageClick={ onPrevPageClick }
      resetPage={ resetPage }
      hasPages={ hasNextPage || page > 1 }
      hasNextPage={ hasNextPage }
      canGoBackwards={ canGoBackwards }
      isLoading={ Boolean(isLoading) }
      isVisible
    />
  );

  let title: string;
  if (isLoading) {
    title = 'Loading activity entries…';
  } else if (total !== null) {
    title = `A total of ${ total } activity entries found`;
  } else {
    title = `${ items.length } activity entries on this page`;
  }

  return (
    <ScanTableCard
      title={ title }
      note="Chain-side and kernel-side entries in one feed, newest first"
      pagination={ paginationNode }

    >
      { items.length === 0 ? (
        <Text color="text.secondary" px={ 4 } py={ 6 }>
          { page === 1 && total === 0 ? 'There is no Paxeer X activity for this account yet.' : 'There are no activity entries on this page.' }
        </Text>
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
            { items.map((item) => (
              <ActivityListItem key={ `${ item.hash }-${ item.kind }-${ item.block_number }-${ item.ordinal }` } item={ item } isLoading={ isLoading }/>
            )) }
          </TableBody>
        </TableRoot>
      ) }
    </ScanTableCard>
  );
};

export default React.memo(ActivityList);
