import { Box } from '@chakra-ui/react';
import React from 'react';

import type { TxsSocketType } from './socket/types';
import type { AddressFromToFilter } from 'types/api/address';
import type { Transaction, TransactionsSortingField, TransactionsSortingValue } from 'types/api/transaction';
import type { PaginationParams } from 'ui/shared/pagination/types';

import useApiQuery from 'lib/api/useApiQuery';
import useIsMobile from 'lib/hooks/useIsMobile';
import useTableViewValue from 'lib/hooks/useTableViewValue';
import { HOMEPAGE_STATS } from 'stubs/stats';
import AddressCsvExportLink from 'ui/address/AddressCsvExportLink';
import { ACTION_BAR_HEIGHT_DESKTOP } from 'ui/shared/ActionBar';
import DataListDisplay from 'ui/shared/DataListDisplay';
import Pagination from 'ui/shared/pagination/Pagination';
import { formatScanTableCount, ScanTableCard } from 'ui/shared/scan';
import getNextSortValue from 'ui/shared/sort/getNextSortValue';
import TableViewToggleButton from 'ui/shared/TableViewToggleButton';

import useDescribeTxs from './noves/useDescribeTxs';
import TxsHeaderMobile from './TxsHeaderMobile';
import TxsList from './TxsList';
import TxsTable from './TxsTable';

const SORT_SEQUENCE: Record<TransactionsSortingField, Array<TransactionsSortingValue>> = {
  value: [ 'value-desc', 'value-asc', 'default' ],
  fee: [ 'fee-desc', 'fee-asc', 'default' ],
  block_number: [ 'block_number-asc', 'default' ],
};

const ITEMS_NAME = 'transactions';

type Props = {
  pagination: PaginationParams;
  showBlockInfo?: boolean;
  socketType?: TxsSocketType;
  currentAddress?: string;
  filter?: React.ReactNode;
  filterValue?: AddressFromToFilter;
  enableTimeIncrement?: boolean;
  top?: number;
  items?: Array<Transaction>;
  isPlaceholderData: boolean;
  isError: boolean;
  setSorting?: (value: TransactionsSortingValue) => void;
  sort: TransactionsSortingValue;
  stickyHeader?: boolean;
  showTableViewButton?: boolean;
};

const TxsContent = ({
  pagination,
  filter,
  filterValue,
  showBlockInfo = true,
  socketType,
  currentAddress,
  enableTimeIncrement,
  top,
  items,
  isPlaceholderData,
  isError,
  setSorting,
  sort,
  stickyHeader = true,
  showTableViewButton,
}: Props) => {
  const isMobile = useIsMobile();

  const tableViewFlag = useTableViewValue();

  const isTableView = isMobile ? showTableViewButton && !tableViewFlag.isLoading && tableViewFlag.value : true;
  const isLoading = isPlaceholderData || tableViewFlag.isLoading;

  const isChainWideList = socketType === 'txs_validated' || socketType === 'txs_pending';

  const statsQuery = useApiQuery('general:stats', {
    queryOptions: {
      enabled: isChainWideList,
      placeholderData: isChainWideList ? HOMEPAGE_STATS : undefined,
    },
  });

  const onSortToggle = React.useCallback((field: TransactionsSortingField) => {
    const value = getNextSortValue<TransactionsSortingField, TransactionsSortingValue>(SORT_SEQUENCE, field)(sort);
    setSorting?.(value);
  }, [ sort, setSorting ]);

  const translationQuery = useDescribeTxs(items, currentAddress, isPlaceholderData);

  const content = items && items.length > 0 ? (
    <>
      <Box display={ isTableView ? 'none' : 'block' }>
        <TxsList
          showBlockInfo={ showBlockInfo }
          socketType={ socketType }
          isLoading={ isLoading }
          enableTimeIncrement={ enableTimeIncrement }
          currentAddress={ currentAddress }
          items={ items }
          translationQuery={ translationQuery }
        />
      </Box>
      <Box display={ isTableView ? 'block' : 'none' }>
        <TxsTable
          txs={ items }
          sort={ sort }
          onSortToggle={ setSorting ? onSortToggle : undefined }
          showBlockInfo={ showBlockInfo }
          socketType={ socketType }
          top={ top || (pagination.isVisible ? ACTION_BAR_HEIGHT_DESKTOP : 0) }
          currentAddress={ currentAddress }
          enableTimeIncrement={ enableTimeIncrement }
          isLoading={ isLoading }
          stickyHeader={ !isMobile && stickyHeader }
          translationQuery={ translationQuery }
        />
      </Box>
    </>
  ) : null;

  const tableViewButton = isMobile && showTableViewButton ? (
    <TableViewToggleButton
      value={ tableViewFlag.value }
      onClick={ tableViewFlag.onToggle }
      loading={ isLoading }
    />
  ) : null;

  const csvExportLink = currentAddress ? (
    <AddressCsvExportLink
      address={ currentAddress }
      params={{ type: 'transactions', filterType: 'address', filterValue }}
      isLoading={ pagination.isLoading }
    />
  ) : null;

  const actionBar = isMobile ? (
    <TxsHeaderMobile
      mt={ -6 }
      sorting={ sort }
      setSorting={ setSorting }
      paginationProps={ pagination }
      showPagination={ pagination.isVisible }
      filterComponent={ filter }
      linkSlot={ csvExportLink }
      tableViewButton={ tableViewButton }
    />
  ) : null;

  const paginationNode = !isMobile && pagination.isVisible ? <Pagination { ...pagination }/> : null;

  const totalTxs = Number(statsQuery.data?.total_transactions);

  const countLine = (() => {
    if (!items || items.length === 0) {
      return 'Transactions';
    }

    if (isChainWideList && Number.isFinite(totalTxs) && totalTxs >= items.length) {
      return formatScanTableCount({ kind: 'latest', value: totalTxs, shownValue: items.length, itemsName: ITEMS_NAME });
    }

    if (pagination.hasNextPage || pagination.page > 1) {
      return formatScanTableCount({ kind: 'more_than', value: pagination.page * items.length, itemsName: ITEMS_NAME });
    }

    return formatScanTableCount({ kind: 'total', value: items.length, itemsName: ITEMS_NAME });
  })();

  const note = (() => {
    if (filterValue) {
      return `Showing only the ${ filterValue === 'from' ? 'outgoing' : 'incoming' } transactions of this address`;
    }

    if (socketType === 'txs_pending') {
      return 'Transactions waiting to be included in a block';
    }

    return `Showing page ${ pagination.page } of the records the node returns, newest first`;
  })();

  return (
    <DataListDisplay
      isError={ isError }
      itemsNum={ items?.length }
      emptyText="There are no transactions."
      actionBar={ actionBar }
      hasActiveFilters={ Boolean(filterValue) }
      emptyStateProps={{
        term: 'transaction',
      }}
    >
      { content && (
        <ScanTableCard
          title={ countLine }
          note={ note }
          actions={ isMobile ? null : csvExportLink }
          pagination={ paginationNode }
        >
          { content }
        </ScanTableCard>
      ) }
    </DataListDisplay>
  );
};

export default TxsContent;
