import { Text } from '@chakra-ui/react';
import React from 'react';

import type { FeedMarket } from 'types/api/paxeerXFeed';

import { route } from 'nextjs/routes';

import useInitialList from 'lib/hooks/useInitialList';
import { Link } from 'toolkit/chakra/link';
import { Skeleton } from 'toolkit/chakra/skeleton';
import { TableBody, TableCell, TableColumnHeader, TableHeaderSticky, TableRoot, TableRow } from 'toolkit/chakra/table';
import BlockEntity from 'ui/shared/entities/block/BlockEntity';
import HashStringShorten from 'ui/shared/HashStringShorten';
import TimeWithTooltip from 'ui/shared/time/TimeWithTooltip';

import PriceValue from './PriceValue';

interface Props {
  items: Array<FeedMarket>;
  top?: number;
  isLoading?: boolean;
}

const dash = <Text color="text.secondary">—</Text>;

// A row is keyed by the submission it shows, so a market that takes a new price mounts its row again
// and fades it in the way the live block and transaction lists do.
const rowId = (market: FeedMarket) => `${ market.market_id }:${ market.latest?.update_hash ?? '' }`;

const PaxeerXFeedMarketsTable = ({ items, top = 0, isLoading }: Props) => {
  const initialList = useInitialList({ data: items, idFn: rowId, enabled: !isLoading });

  return (
    <TableRoot variant="scan" minW="900px" data-label="paxeer-x-feed-markets">
      <TableHeaderSticky top={ top }>
        <TableRow>
          <TableColumnHeader width="18%">Market</TableColumnHeader>
          <TableColumnHeader width="16%" isNumeric>Index price</TableColumnHeader>
          <TableColumnHeader width="16%" isNumeric>Mark price</TableColumnHeader>
          <TableColumnHeader width="14%">Source time</TableColumnHeader>
          <TableColumnHeader width="22%">Last update</TableColumnHeader>
          <TableColumnHeader width="14%">Block</TableColumnHeader>
        </TableRow>
      </TableHeaderSticky>
      <TableBody>
        { items.map((market, index) => {
          const { latest } = market;

          return (
            <TableRow
              key={ rowId(market) + (isLoading ? `-${ index }` : '') }
              data-market={ market.market_id }
              animation={ isLoading ? undefined : initialList.getAnimationProp(market) }
            >
              <TableCell verticalAlign="middle">
                <Skeleton loading={ isLoading } fontWeight={ 600 }>
                  <Link href={ route({ pathname: '/feed/market/[id]', query: { id: String(market.market_id) } }) }>
                    { market.symbol }
                  </Link>
                </Skeleton>
                <Skeleton loading={ isLoading } color="text.secondary" textStyle="xs">{ market.chainflow_symbol }</Skeleton>
              </TableCell>
              <TableCell verticalAlign="middle" isNumeric>
                { latest ? <PriceValue value={ latest.index_price } isLoading={ isLoading }/> : dash }
              </TableCell>
              <TableCell verticalAlign="middle" isNumeric>
                { latest ? <PriceValue value={ latest.mark_price } isLoading={ isLoading }/> : dash }
              </TableCell>
              <TableCell verticalAlign="middle" color="text.secondary">
                { latest ? <TimeWithTooltip timestamp={ latest.source_timestamp_ms } isLoading={ isLoading } enableIncrement/> : dash }
              </TableCell>
              <TableCell verticalAlign="middle">
                { latest ? (
                  <Skeleton loading={ isLoading }>
                    <Link href={ route({ pathname: '/feed/update/[hash]', query: { hash: latest.update_hash } }) }>
                      <HashStringShorten hash={ latest.update_hash } type="long"/>
                    </Link>
                  </Skeleton>
                ) : dash }
              </TableCell>
              <TableCell verticalAlign="middle">
                { latest ? <BlockEntity number={ latest.block_number } isLoading={ isLoading } truncation="none" noIcon/> : dash }
              </TableCell>
            </TableRow>
          );
        }) }
      </TableBody>
    </TableRoot>
  );
};

export default PaxeerXFeedMarketsTable;
