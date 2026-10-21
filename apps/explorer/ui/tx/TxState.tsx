import { Box, Text } from '@chakra-ui/react';
import React from 'react';

import { TX_STATE_CHANGES } from 'stubs/txStateChanges';
import DataListDisplay from 'ui/shared/DataListDisplay';
import Pagination from 'ui/shared/pagination/Pagination';
import useQueryWithPages from 'ui/shared/pagination/useQueryWithPages';
import { formatScanTableCount, ScanTableCard } from 'ui/shared/scan';
import TxStateList from 'ui/tx/state/TxStateList';
import TxStateTable from 'ui/tx/state/TxStateTable';

import TxPendingAlert from './TxPendingAlert';
import TxSocketAlert from './TxSocketAlert';
import type { TxQuery } from './useTxQuery';

interface Props {
  txQuery: TxQuery;
}

const TxState = ({ txQuery }: Props) => {
  const { data, isPlaceholderData, isError, pagination } = useQueryWithPages({
    resourceName: 'general:tx_state_changes',
    pathParams: { hash: txQuery.data?.hash },
    options: {
      enabled: !txQuery.isPlaceholderData && Boolean(txQuery.data?.hash) && Boolean(txQuery.data?.status),
      placeholderData: {
        items: TX_STATE_CHANGES,
        next_page_params: {
          items_count: 1,
          state_changes: null,
        },
      },
    },
  });

  if (!txQuery.isPending && !txQuery.isPlaceholderData && !txQuery.isError && !txQuery.data.status) {
    return txQuery.socketStatus ? <TxSocketAlert status={ txQuery.socketStatus }/> : <TxPendingAlert/>;
  }

  const content = data ? (
    <>
      <Box hideBelow="lg">
        <TxStateTable data={ data.items } isLoading={ isPlaceholderData } top={ 0 }/>
      </Box>
      <Box hideFrom="lg">
        <TxStateList data={ data.items } isLoading={ isPlaceholderData }/>
      </Box>
    </>
  ) : null;

  const itemsNum = data?.items.length ?? 0;

  return (
    <>
      { !isError && !txQuery.isError && (
        <Text mb={{ base: 3, lg: 4 }}>
          A set of information that represents the current state is updated when a transaction takes place on the network.
          The below is a summary of those changes.
        </Text>
      ) }
      <ScanTableCard
        title="State changes"
        note={ formatScanTableCount({
          kind: pagination.isVisible ? 'more_than' : 'total',
          value: itemsNum,
          itemsName: itemsNum === 1 ? 'state change' : 'state changes',
        }) }
        pagination={ pagination.isVisible ? <Pagination { ...pagination }/> : null }
      >
        <DataListDisplay
          isError={ isError || txQuery.isError }
          itemsNum={ data?.items.length }
          emptyText="There are no state changes for this transaction."
        >
          { content }
        </DataListDisplay>
      </ScanTableCard>
    </>
  );
};

export default TxState;
