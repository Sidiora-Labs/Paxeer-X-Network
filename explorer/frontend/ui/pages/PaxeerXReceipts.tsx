import { Box } from '@chakra-ui/react';
import React from 'react';

import useIsMobile from 'lib/hooks/useIsMobile';
import { PAXEER_X_RECEIPTS_ITEM } from 'stubs/paxeerXLists';
import { generateListStub } from 'stubs/utils';
import PaxeerXReceiptsListItem from 'ui/paxeerX/receipts/PaxeerXReceiptsListItem';
import PaxeerXReceiptsTable from 'ui/paxeerX/receipts/PaxeerXReceiptsTable';
import ActionBar from 'ui/shared/ActionBar';
import DataListDisplay from 'ui/shared/DataListDisplay';
import PageTitle from 'ui/shared/Page/PageTitle';
import Pagination from 'ui/shared/pagination/Pagination';
import useQueryWithPages from 'ui/shared/pagination/useQueryWithPages';
import { formatScanTableCount, ScanTableCard } from 'ui/shared/scan';

const ITEMS_NAME = 'kernel receipts';

const PaxeerXReceipts = () => {
  const isMobile = useIsMobile();

  const { data, isError, isPlaceholderData, pagination } = useQueryWithPages({
    resourceName: 'general:paxeer_x_receipts',
    options: {
      placeholderData: generateListStub<'general:paxeer_x_receipts'>(
        PAXEER_X_RECEIPTS_ITEM,
        50,
        {
          next_page_params: {
            items_count: 50,
            id: PAXEER_X_RECEIPTS_ITEM.id,
          },
        },
      ),
    },
  });

  const content = data?.items ? (
    <>
      <Box hideFrom="lg">
        { data.items.map((item, index) => (
          <PaxeerXReceiptsListItem
            key={ item.id + (isPlaceholderData ? String(index) : '') }
            item={ item }
            isLoading={ isPlaceholderData }
          />
        )) }
      </Box>
      <Box hideBelow="lg">
        <PaxeerXReceiptsTable items={ data.items } top={ 0 } isLoading={ isPlaceholderData }/>
      </Box>
    </>
  ) : null;

  const actionBar = isMobile && pagination.isVisible ? (
    <ActionBar mt={ -6 }>
      <Pagination ml="auto" { ...pagination }/>
    </ActionBar>
  ) : null;

  const paginationNode = !isMobile && pagination.isVisible ? <Pagination { ...pagination }/> : null;

  const items = data?.items;

  const countLine = (() => {
    if (!items || items.length === 0) {
      return 'Kernel receipts';
    }

    if (pagination.hasNextPage || pagination.page > 1) {
      return formatScanTableCount({ kind: 'more_than', value: pagination.page * items.length, itemsName: ITEMS_NAME });
    }

    return formatScanTableCount({ kind: 'total', value: items.length, itemsName: ITEMS_NAME });
  })();

  return (
    <>
      <PageTitle title="Kernel receipts" withTextAd/>
      <DataListDisplay
        isError={ isError }
        itemsNum={ data?.items.length }
        emptyText="There are no kernel receipts."
        actionBar={ actionBar }
      >
        { content && (
          <ScanTableCard
            title={ countLine }
            note={ `Showing page ${ pagination.page } of the receipts the node returns, newest first` }
            pagination={ paginationNode }
          >
            { content }
          </ScanTableCard>
        ) }
      </DataListDisplay>
    </>
  );
};

export default PaxeerXReceipts;
