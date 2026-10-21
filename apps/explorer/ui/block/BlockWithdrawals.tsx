import { Box } from '@chakra-ui/react';
import React from 'react';

import DataListDisplay from 'ui/shared/DataListDisplay';
import Pagination from 'ui/shared/pagination/Pagination';
import type { QueryWithPagesResult } from 'ui/shared/pagination/useQueryWithPages';
import { formatScanTableCount, ScanTableCard } from 'ui/shared/scan';
import BeaconChainWithdrawalsList from 'ui/withdrawals/beaconChain/BeaconChainWithdrawalsList';
import BeaconChainWithdrawalsTable from 'ui/withdrawals/beaconChain/BeaconChainWithdrawalsTable';

type Props = {
  blockWithdrawalsQuery: QueryWithPagesResult<'general:block_withdrawals'>;
  itemsCount?: number;
};

const BlockWithdrawals = ({ blockWithdrawalsQuery, itemsCount }: Props) => {
  const items = blockWithdrawalsQuery.data?.items;

  const content = items ? (
    <>
      <Box hideFrom="lg">
        <BeaconChainWithdrawalsList
          items={ items }
          isLoading={ blockWithdrawalsQuery.isPlaceholderData }
          view="block"
        />
      </Box>
      <Box hideBelow="lg">
        <BeaconChainWithdrawalsTable
          items={ items }
          isLoading={ blockWithdrawalsQuery.isPlaceholderData }
          top={ 0 }
          view="block"
        />
      </Box>
    </>
  ) : null ;

  const title = formatScanTableCount({
    kind: 'total',
    value: itemsCount ?? items?.length ?? 0,
    itemsName: 'withdrawals',
  });

  const pagination = blockWithdrawalsQuery.pagination.isVisible ?
    <Pagination { ...blockWithdrawalsQuery.pagination }/> :
    undefined;

  return (
    <ScanTableCard title={ title } pagination={ pagination }>
      <DataListDisplay
        isError={ blockWithdrawalsQuery.isError }
        itemsNum={ items?.length }
        emptyText="There are no withdrawals for this block."
      >
        { content }
      </DataListDisplay>
    </ScanTableCard>
  );
};

export default BlockWithdrawals;
