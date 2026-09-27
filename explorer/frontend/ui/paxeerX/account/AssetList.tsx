import { Text } from '@chakra-ui/react';
import React from 'react';

import type { PaxeerXBalance } from 'types/api/paxeerX';

import { TableBody, TableColumnHeader, TableHeader, TableRoot, TableRow } from 'toolkit/chakra/table';
import Pagination from 'ui/shared/pagination/Pagination';
import { formatScanTableCount, SCAN_ROWS_PER_PAGE, ScanShowRows, ScanTableCard } from 'ui/shared/scan';

import AssetListItem from './AssetListItem';

export interface Props {
  items: Array<PaxeerXBalance>;
  isLoading?: boolean;
}

// One row per asset with a single total; the per-location parts live in an expandable row.
const AssetList = ({ items, isLoading }: Props) => {
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
      title={ formatScanTableCount({ kind: 'total', value: items.length, itemsName: 'assets' }) }
      note="Chain, custody and kernel balances of one asset add up to its total"
      pagination={ paginationNode }
      showRows={ <ScanShowRows value={ rowsPerPage } onValueChange={ handleRowsPerPageChange } isLoading={ isLoading }/> }
    >
      { items.length === 0 ? (
        <Text color="text.secondary" px={ 4 } py={ 6 }>No assets are held by this account.</Text>
      ) : (
        <TableRoot variant="scan" minW="700px" data-label="paxeer-x-assets">
          <TableHeader>
            <TableRow>
              <TableColumnHeader width="40%">Asset</TableColumnHeader>
              <TableColumnHeader width="35%">Denom</TableColumnHeader>
              <TableColumnHeader width="25%" isNumeric>Total</TableColumnHeader>
            </TableRow>
          </TableHeader>
          <TableBody>
            { rows.map((item) => (
              <AssetListItem key={ item.asset.id } item={ item } isLoading={ isLoading }/>
            )) }
          </TableBody>
        </TableRoot>
      ) }
    </ScanTableCard>
  );
};

export default React.memo(AssetList);
