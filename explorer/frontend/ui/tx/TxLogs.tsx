import { Box, Text } from '@chakra-ui/react';
import React from 'react';

import type { Log } from 'types/api/log';

import { LOG } from 'stubs/log';
import { generateListStub } from 'stubs/utils';
import DataFetchAlert from 'ui/shared/DataFetchAlert';
import LogItem from 'ui/shared/logs/LogItem';
import Pagination from 'ui/shared/pagination/Pagination';
import useQueryWithPages from 'ui/shared/pagination/useQueryWithPages';
import { formatScanTableCount, ScanTableCard } from 'ui/shared/scan';
import TxPendingAlert from 'ui/tx/TxPendingAlert';
import TxSocketAlert from 'ui/tx/TxSocketAlert';

import type { TxQuery } from './useTxQuery';

interface Props {
  txQuery: TxQuery;
  logsFilter?: (log: Log) => boolean;
}

const TxLogs = ({ txQuery, logsFilter }: Props) => {
  const { data, isPlaceholderData, isError, pagination } = useQueryWithPages({
    resourceName: 'general:tx_logs',
    pathParams: { hash: txQuery.data?.hash },
    options: {
      enabled: !txQuery.isPlaceholderData && Boolean(txQuery.data?.hash) && Boolean(txQuery.data?.status),
      placeholderData: generateListStub<'general:tx_logs'>(LOG, 3, { next_page_params: null }),
    },
  });

  if (!txQuery.isPending && !txQuery.isPlaceholderData && !txQuery.isError && !txQuery.data.status) {
    return txQuery.socketStatus ? <TxSocketAlert status={ txQuery.socketStatus }/> : <TxPendingAlert/>;
  }

  if (isError || txQuery.isError) {
    return <DataFetchAlert/>;
  }

  let items: Array<Log> = [];

  if (data?.items) {
    if (isPlaceholderData) {
      items = data?.items;
    } else {
      items = logsFilter ? data.items.filter(logsFilter) : data.items;
    }
  }

  if (!items.length) {
    return <Text as="span">There are no logs for this transaction.</Text>;
  }

  return (
    <ScanTableCard
      title="Transaction receipt event logs"
      note={ formatScanTableCount({
        kind: pagination.isVisible ? 'more_than' : 'total',
        value: items.length,
        itemsName: items.length === 1 ? 'log' : 'logs',
      }) }
      pagination={ pagination.isVisible ? <Pagination { ...pagination }/> : null }
    >
      <Box px={ 4 } pb={ 2 }>
        { items.map((item, index) => (
          <LogItem
            key={ index }
            { ...item }
            type="transaction"
            isLoading={ isPlaceholderData }
            defaultDataType={ txQuery.data?.zilliqa?.is_scilla ? 'UTF-8' : undefined }
          />
        )) }
      </Box>
    </ScanTableCard>
  );
};

export default TxLogs;
