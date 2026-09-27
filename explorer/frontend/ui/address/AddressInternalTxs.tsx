import { Box, Flex } from '@chakra-ui/react';
import React from 'react';

import { route } from 'nextjs/routes';

import useIsMobile from 'lib/hooks/useIsMobile';
import useIsMounted from 'lib/hooks/useIsMounted';
import { Link } from 'toolkit/chakra/link';
import InternalTxsList from 'ui/internalTxs/InternalTxsList';
import InternalTxsTable from 'ui/internalTxs/InternalTxsTable';
import DataListDisplay from 'ui/shared/DataListDisplay';
import Pagination from 'ui/shared/pagination/Pagination';
import { formatScanTableCount, ScanDirectionBadge, ScanTableCard } from 'ui/shared/scan';

import AddressCsvExportLink from './AddressCsvExportLink';
import AddressTxsFilter from './AddressTxsFilter';
import useAddressInternalTxsQuery from './useAddressInternalTxsQuery';

type Props = {
  shouldRender?: boolean;
  isQueryEnabled?: boolean;
  internalTxsCount?: number;
};

const AddressInternalTxs = ({ shouldRender = true, isQueryEnabled = true, internalTxsCount }: Props) => {
  const isMounted = useIsMounted();
  const isMobile = useIsMobile();

  const { hash, query, filterValue, onFilterChange } = useAddressInternalTxsQuery({ enabled: isQueryEnabled });
  const { data, isPlaceholderData, isError, pagination } = query;

  if (!isMounted || !shouldRender) {
    return null;
  }

  const content = data?.items ? (
    <>
      <Box hideFrom="lg">
        <InternalTxsList data={ data.items } currentAddress={ hash } isLoading={ isPlaceholderData }/>
      </Box>
      <Box hideBelow="lg">
        <InternalTxsTable data={ data.items } currentAddress={ hash } isLoading={ isPlaceholderData }/>
      </Box>
    </>
  ) : null ;

  const itemsNum = data?.items.length;
  const title = formatScanTableCount(
    internalTxsCount !== undefined && itemsNum !== undefined && itemsNum < internalTxsCount ?
      { kind: 'latest', value: internalTxsCount, itemsName: 'internal transactions', shownValue: itemsNum } :
      { kind: 'total', value: internalTxsCount ?? itemsNum ?? 0, itemsName: 'internal transactions' },
  );

  const actions = (
    <>
      <AddressTxsFilter
        initialValue={ filterValue }
        onFilterChange={ onFilterChange }
        hasActiveFilter={ Boolean(filterValue) }
        isLoading={ pagination.isLoading }
      />
      { filterValue && <ScanDirectionBadge direction={ filterValue === 'from' ? 'out' : 'in' }/> }
      { !isMobile && (
        <AddressCsvExportLink
          address={ hash }
          label="Download Page Data"
          isLoading={ pagination.isLoading }
          params={{ type: 'internal-transactions', filterType: 'address', filterValue }}
        />
      ) }
    </>
  );

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
      <Link href={ route({ pathname: '/internal-txs' }) } textStyle="xs" fontWeight="500" textTransform="uppercase">
        View all internal transactions →
      </Link>
    </Flex>
  );

  return (
    <>
      <ScanTableCard
        title={ title }
        actions={ actions }
        pagination={ <Pagination { ...pagination }/> }
      >
        <DataListDisplay
          isError={ isError }
          itemsNum={ itemsNum }
          hasActiveFilters={ Boolean(filterValue) }
          emptyStateProps={{
            term: 'transaction',
          }}
          emptyText="There are no internal transactions for this address."
        >
          { content }
        </DataListDisplay>
        { viewAllRow }
      </ScanTableCard>
      <Flex justifyContent="flex-end" mt={ 3 }>
        <AddressCsvExportLink
          address={ hash }
          label="CSV Export"
          isLoading={ pagination.isLoading }
          params={{ type: 'internal-transactions', filterType: 'address', filterValue }}
        />
      </Flex>
    </>
  );
};

export default AddressInternalTxs;
