import { Flex, Text } from '@chakra-ui/react';
import React from 'react';

import type { PaxeerXIdentities } from 'types/api/paxeerX';

import { route } from 'nextjs/routes';

import { Link } from 'toolkit/chakra/link';
import { Skeleton } from 'toolkit/chakra/skeleton';
import { TableBody, TableCell, TableColumnHeader, TableHeader, TableRoot, TableRow } from 'toolkit/chakra/table';
import CopyToClipboard from 'ui/shared/CopyToClipboard';
import Pagination from 'ui/shared/pagination/Pagination';
import { formatScanTableCount, SCAN_ROWS_PER_PAGE, ScanShowRows, ScanTableCard } from 'ui/shared/scan';

import { listIdentities } from './utils';

export interface Props {
  identities: PaxeerXIdentities;
  isLoading?: boolean;
}

const IdentityList = ({ identities, isLoading }: Props) => {
  const entries = listIdentities(identities);

  const [ rowsPerPage, setRowsPerPage ] = React.useState(SCAN_ROWS_PER_PAGE[0]);
  const [ page, setPage ] = React.useState(1);

  const pageCount = Math.max(1, Math.ceil(entries.length / rowsPerPage));
  const currentPage = Math.min(page, pageCount);
  const rows = entries.slice((currentPage - 1) * rowsPerPage, currentPage * rowsPerPage);

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
      title={ formatScanTableCount({ kind: 'total', value: entries.length, itemsName: 'identities' }) }
      note="Every spelling of this account the node answers for"
      pagination={ paginationNode }
      showRows={ <ScanShowRows value={ rowsPerPage } onValueChange={ handleRowsPerPageChange } isLoading={ isLoading }/> }
    >
      { entries.length === 0 ? (
        <Text color="text.secondary" px={ 4 } py={ 6 }>This account has no Paxeer X identities yet.</Text>
      ) : (
        <TableRoot variant="scan" data-label="paxeer-x-identities">
          <TableHeader>
            <TableRow>
              <TableColumnHeader width="30%">Identity</TableColumnHeader>
              <TableColumnHeader width="70%">Value</TableColumnHeader>
            </TableRow>
          </TableHeader>
          <TableBody>
            { rows.map((entry) => (
              <TableRow key={ entry.kind } data-identity={ entry.kind }>
                <TableCell verticalAlign="middle">
                  <Skeleton loading={ isLoading } fontWeight={ 600 } display="inline-block">
                    { entry.label }
                  </Skeleton>
                </TableCell>
                <TableCell verticalAlign="middle">
                  <Flex columnGap={ 2 } alignItems="center" minW={ 0 }>
                    <Skeleton loading={ isLoading } overflow="hidden" minW={ 0 }>
                      { entry.kind === 'evm' ? (
                        <Link href={ route({ pathname: '/address/[hash]', query: { hash: entry.value } }) } wordBreak="break-all">
                          { entry.value }
                        </Link>
                      ) : (
                        <Text as="span" wordBreak="break-all" whiteSpace="normal">{ entry.value }</Text>
                      ) }
                    </Skeleton>
                    <CopyToClipboard text={ entry.value } isLoading={ isLoading }/>
                  </Flex>
                </TableCell>
              </TableRow>
            )) }
          </TableBody>
        </TableRoot>
      ) }
    </ScanTableCard>
  );
};

export default React.memo(IdentityList);
