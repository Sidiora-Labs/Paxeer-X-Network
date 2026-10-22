import { Box } from '@chakra-ui/react';
import { useQueryClient } from '@tanstack/react-query';
import { useRouter } from 'next/router';
import React from 'react';

import type { SocketMessage } from 'lib/socket/types';
import type { MarketUpdate, MarketUpdatesResponse } from 'types/api/paxeerXFeed';

import useApiQuery, { getResourceKey } from 'lib/api/useApiQuery';
import throwOnAbsentParamError from 'lib/errors/throwOnAbsentParamError';
import getQueryParamString from 'lib/router/getQueryParamString';
import useSocketBuffer from 'lib/socket/useSocketBuffer';
import useSocketMessage from 'lib/socket/useSocketMessage';
import PaxeerXFeedMarketUpdatesTable from 'ui/paxeerX/feed/PaxeerXFeedMarketUpdatesTable';
import useFeedSocket from 'ui/paxeerX/feed/useFeedSocket';
import DataListDisplay from 'ui/shared/DataListDisplay';
import PageTitle from 'ui/shared/Page/PageTitle';
import Pagination from 'ui/shared/pagination/Pagination';
import useQueryWithPages from 'ui/shared/pagination/useQueryWithPages';
import { ScanTableCard } from 'ui/shared/scan';

// Newest first, the order the endpoint pages in.
const byNewest = (a: MarketUpdate, b: MarketUpdate) => b.block_number - a.block_number || b.sequence - a.sequence;

const PaxeerXFeedMarket = () => {
  const router = useRouter();
  const queryClient = useQueryClient();
  const id = getQueryParamString(router.query.id);
  throwOnAbsentParamError(id);

  const page = getQueryParamString(router.query.page);
  const isFirstPage = !page || page === '1';

  const { channel, refetchInterval } = useFeedSocket({ topic: `paxeer_x_feed:market:${ id }`, isDisabled: !id || !isFirstPage });

  const marketsQuery = useApiQuery('general:paxeer_x_feed_markets');
  const market = marketsQuery.data?.items.find((item) => String(item.market_id) === id);

  const { data, isError, isPending, pagination } = useQueryWithPages({
    resourceName: 'general:paxeer_x_feed_market_updates',
    pathParams: { id },
    options: { refetchInterval },
  });

  // ponytail: the first page keeps its length, so rows pushed off it by live updates are reached again only
  // after a reload; the market history keyset has no update index in the socket item to move next_page_params.
  const handleFlush = React.useCallback((updates: Array<MarketUpdate>) => {
    queryClient.setQueryData(getResourceKey('general:paxeer_x_feed_market_updates', { pathParams: { id } }), (prevData: MarketUpdatesResponse | undefined) => {
      if (!prevData) {
        return prevData;
      }

      const known = new Set(prevData.items.map((item) => item.update_hash));
      const fresh = updates.filter((item) => !known.has(item.update_hash) && (!prevData.items[0] || byNewest(item, prevData.items[0]) < 0));

      if (fresh.length === 0) {
        return prevData;
      }

      const items = [ ...fresh, ...prevData.items ]
        .sort(byNewest)
        .slice(0, Math.max(prevData.items.length, fresh.length));

      return { ...prevData, items };
    });
  }, [ id, queryClient ]);

  const { push, hoverProps } = useSocketBuffer<MarketUpdate>({ onFlush: handleFlush });

  const handleNewUpdate: SocketMessage.PaxeerXFeedMarketNewUpdate['handler'] = React.useCallback((payload) => {
    push(payload);
  }, [ push ]);

  useSocketMessage({
    channel,
    event: 'new_update',
    handler: handleNewUpdate,
  });

  const title = market ? `${ market.symbol } price history` : `Market ${ id } price history`;
  const paginationNode = pagination.isVisible ? <Pagination { ...pagination }/> : null;

  return (
    <>
      <PageTitle title={ title } withTextAd isLoading={ marketsQuery.isPending }/>
      <DataListDisplay
        isError={ isError }
        itemsNum={ data?.items.length }
        emptyText={ isPending ? null : 'There are no price updates for this market.' }
      >
        { data?.items && (
          <ScanTableCard
            title={ market ? `${ market.symbol } · ${ market.chainflow_symbol }` : `Market ${ id }` }
            note={ `Showing page ${ pagination.page } of the submitted prices, newest first; change is against the previous update` }
            pagination={ paginationNode }
          >
            <Box { ...hoverProps }>
              <PaxeerXFeedMarketUpdatesTable items={ data.items }/>
            </Box>
          </ScanTableCard>
        ) }
      </DataListDisplay>
    </>
  );
};

export default PaxeerXFeedMarket;
