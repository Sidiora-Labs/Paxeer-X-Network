import { useRouter } from 'next/router';
import React from 'react';

import useApiQuery from 'lib/api/useApiQuery';
import throwOnAbsentParamError from 'lib/errors/throwOnAbsentParamError';
import getQueryParamString from 'lib/router/getQueryParamString';
import PaxeerXFeedMarketUpdatesTable from 'ui/paxeerX/feed/PaxeerXFeedMarketUpdatesTable';
import DataListDisplay from 'ui/shared/DataListDisplay';
import PageTitle from 'ui/shared/Page/PageTitle';
import Pagination from 'ui/shared/pagination/Pagination';
import useQueryWithPages from 'ui/shared/pagination/useQueryWithPages';
import { ScanTableCard } from 'ui/shared/scan';

const PaxeerXFeedMarket = () => {
  const router = useRouter();
  const id = getQueryParamString(router.query.id);
  throwOnAbsentParamError(id);

  const marketsQuery = useApiQuery('general:paxeer_x_feed_markets');
  const market = marketsQuery.data?.items.find((item) => String(item.market_id) === id);

  const { data, isError, isPending, pagination } = useQueryWithPages({
    resourceName: 'general:paxeer_x_feed_market_updates',
    pathParams: { id },
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
            <PaxeerXFeedMarketUpdatesTable items={ data.items }/>
          </ScanTableCard>
        ) }
      </DataListDisplay>
    </>
  );
};

export default PaxeerXFeedMarket;
