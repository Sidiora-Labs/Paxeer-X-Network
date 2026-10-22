import React from 'react';

import useApiQuery from 'lib/api/useApiQuery';
import PaxeerXFeedMarketsTable from 'ui/paxeerX/feed/PaxeerXFeedMarketsTable';
import DataListDisplay from 'ui/shared/DataListDisplay';
import PageTitle from 'ui/shared/Page/PageTitle';
import { formatScanTableCount, ScanTableCard } from 'ui/shared/scan';

const PaxeerXFeed = () => {
  const { data, isError, isPending } = useApiQuery('general:paxeer_x_feed_markets');

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
            <PaxeerXFeedMarketsTable items={ items }/>
          </ScanTableCard>
        ) }
      </DataListDisplay>
    </>
  );
};

export default PaxeerXFeed;
