import { Box } from '@chakra-ui/react';
import React from 'react';

import BeaconChainDepositsList from 'ui/deposits/beaconChain/BeaconChainDepositsList';
import BeaconChainDepositsTable from 'ui/deposits/beaconChain/BeaconChainDepositsTable';
import DataListDisplay from 'ui/shared/DataListDisplay';
import Pagination from 'ui/shared/pagination/Pagination';
import type { QueryWithPagesResult } from 'ui/shared/pagination/useQueryWithPages';
import { formatScanTableCount, ScanTableCard } from 'ui/shared/scan';

type Props = {
  blockDepositsQuery: QueryWithPagesResult<'general:block_deposits'>;
  itemsCount?: number;
};

const BlockDeposits = ({ blockDepositsQuery, itemsCount }: Props) => {
  const items = blockDepositsQuery.data?.items;

  const content = items ? (
    <>
      <Box hideFrom="lg">
        <BeaconChainDepositsList
          items={ items }
          isLoading={ blockDepositsQuery.isPlaceholderData }
          view="block"
        />
      </Box>
      <Box hideBelow="lg">
        <BeaconChainDepositsTable
          items={ items }
          isLoading={ blockDepositsQuery.isPlaceholderData }
          top={ 0 }
          view="block"
        />
      </Box>
    </>
  ) : null ;

  const title = formatScanTableCount({
    kind: 'total',
    value: itemsCount ?? items?.length ?? 0,
    itemsName: 'deposits',
  });

  const pagination = blockDepositsQuery.pagination.isVisible ?
    <Pagination { ...blockDepositsQuery.pagination }/> :
    undefined;

  return (
    <ScanTableCard title={ title } pagination={ pagination }>
      <DataListDisplay
        isError={ blockDepositsQuery.isError }
        itemsNum={ items?.length }
        emptyText="There are no deposits for this block."
      >
        { content }
      </DataListDisplay>
    </ScanTableCard>
  );
};

export default BlockDeposits;
