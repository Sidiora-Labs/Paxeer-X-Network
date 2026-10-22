import { Box } from '@chakra-ui/react';
import { useQueryClient } from '@tanstack/react-query';
import React from 'react';

import type { SocketMessage } from 'lib/socket/types';
import type { FeedMarketsResponse, PriceUpdateItem } from 'types/api/paxeerXFeed';

import useApiQuery, { getResourceKey } from 'lib/api/useApiQuery';
import useSocketBuffer from 'lib/socket/useSocketBuffer';
import useSocketMessage from 'lib/socket/useSocketMessage';
import applyPriceUpdates from 'ui/paxeerX/feed/applyPriceUpdates';
import PaxeerXFeedMarketsTable from 'ui/paxeerX/feed/PaxeerXFeedMarketsTable';
import useFeedSocket from 'ui/paxeerX/feed/useFeedSocket';
import DataListDisplay from 'ui/shared/DataListDisplay';
import PageTitle from 'ui/shared/Page/PageTitle';
import { formatScanTableCount, ScanTableCard } from 'ui/shared/scan';

const PaxeerXFeed = () => {
  const queryClient = useQueryClient();

  const { channel, refetchInterval } = useFeedSocket({ topic: 'paxeer_x_feed:new_update', isDisabled: false });

  const { data, isError, isPending } = useApiQuery('general:paxeer_x_feed_markets', {
    queryOptions: { refetchInterval },
  });

  const handleFlush = React.useCallback((updates: Array<PriceUpdateItem>) => {
    queryClient.setQueryData(getResourceKey('general:paxeer_x_feed_markets'), (prevData: FeedMarketsResponse | undefined) => {
      return prevData ? applyPriceUpdates(prevData, updates) : prevData;
    });
  }, [ queryClient ]);

  const { push, hoverProps } = useSocketBuffer<PriceUpdateItem>({ onFlush: handleFlush });

  const handleNewUpdate: SocketMessage.PaxeerXFeedNewUpdate['handler'] = React.useCallback((payload) => {
    push(payload);
  }, [ push ]);

  useSocketMessage({
    channel,
    event: 'new_update',
    handler: handleNewUpdate,
  });

  const items = data?.items;

  return (
    <>
      <PageTitle title="Price feed" withTextAd isLoading={ isPending }/>
      <DataListDisplay
        isError={ isError }
        itemsNum={ items?.length }
        emptyText={ isPending ? null : 'There are no price feed markets.' }
      >
        { items && (
          <ScanTableCard
            title={ formatScanTableCount({ kind: 'total', value: items.length, itemsName: 'markets' }) }
            note={ `Latest submitted prices per market at chain head block ${ data.chain_head.toLocaleString() }` }
          >
            <Box { ...hoverProps }>
              <PaxeerXFeedMarketsTable items={ items }/>
            </Box>
          </ScanTableCard>
        ) }
      </DataListDisplay>
    </>
  );
};

export default PaxeerXFeed;
