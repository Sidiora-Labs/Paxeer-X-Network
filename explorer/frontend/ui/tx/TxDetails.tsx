import { Flex } from '@chakra-ui/react';
import type { UseQueryResult } from '@tanstack/react-query';
import React from 'react';

import type * as tac from '@blockscout/tac-operation-lifecycle-types';

import type { ResourceError } from 'lib/api/resources';
import TestnetWarning from 'ui/shared/alerts/TestnetWarning';
import BlockPendingUpdateAlert from 'ui/shared/block/BlockPendingUpdateAlert';
import DataFetchAlert from 'ui/shared/DataFetchAlert';

import TxDetailsActions from './details/txDetailsActions/TxDetailsActions';
import TxInfo from './details/TxInfo';
import type { TxQuery } from './useTxQuery';

interface Props {
  txQuery: TxQuery;
  tacOperationQuery?: UseQueryResult<tac.OperationsFullResponse, ResourceError>;
}

const TxDetails = ({ txQuery, tacOperationQuery }: Props) => {
  if (txQuery.isError) {
    return <DataFetchAlert/>;
  }

  const isLoading = txQuery.isPlaceholderData || (tacOperationQuery?.isPlaceholderData ?? false);

  return (
    <Flex flexDir="column" rowGap={{ base: 3, lg: 4 }} data-tx-details>
      <Flex rowGap={{ base: 1, lg: 2 }} flexDir="column" _empty={{ display: 'none' }}>
        <TestnetWarning isLoading={ txQuery.isPlaceholderData }/>
        { txQuery.data?.is_pending_update && <BlockPendingUpdateAlert view="tx"/> }
      </Flex>
      <TxDetailsActions
        hash={ txQuery.data?.hash }
        actions={ txQuery.data?.actions }
        isTxDataLoading={ txQuery.isPlaceholderData }
      />
      <TxInfo
        data={ txQuery.data }
        tacOperations={ tacOperationQuery?.data?.items }
        isLoading={ isLoading }
        socketStatus={ txQuery.socketStatus }
      />
    </Flex>
  );
};

export default React.memo(TxDetails);
