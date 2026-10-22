import React from 'react';

import type { PriceObservation } from 'types/api/paxeerXFeed';

import { route } from 'nextjs/routes';

import { Link } from 'toolkit/chakra/link';
import { TableBody, TableCell, TableColumnHeader, TableRoot, TableRow } from 'toolkit/chakra/table';
import TimeWithTooltip from 'ui/shared/time/TimeWithTooltip';

import PriceValue from './PriceValue';

interface Props {
  items: Array<PriceObservation>;
}

const PaxeerXFeedObservationsTable = ({ items }: Props) => (
  <TableRoot variant="scan" minW="800px" data-label="paxeer-x-feed-observations">
    <thead>
      <TableRow>
        <TableColumnHeader width="22%">Market</TableColumnHeader>
        <TableColumnHeader width="22%" isNumeric>Index price</TableColumnHeader>
        <TableColumnHeader width="22%" isNumeric>Mark price</TableColumnHeader>
        <TableColumnHeader width="22%">Source time</TableColumnHeader>
        <TableColumnHeader width="12%" isNumeric>Sequence</TableColumnHeader>
      </TableRow>
    </thead>
    <TableBody>
      { items.map((item) => (
        <TableRow key={ item.market_id } data-observation={ item.market_id }>
          <TableCell verticalAlign="middle" fontWeight={ 600 }>
            <Link href={ route({ pathname: '/feed/market/[id]', query: { id: String(item.market_id) } }) }>
              { item.symbol ?? `Market ${ item.market_id }` }
            </Link>
          </TableCell>
          <TableCell verticalAlign="middle" isNumeric><PriceValue value={ item.index_price }/></TableCell>
          <TableCell verticalAlign="middle" isNumeric><PriceValue value={ item.mark_price }/></TableCell>
          <TableCell verticalAlign="middle" color="text.secondary">
            <TimeWithTooltip timestamp={ item.source_timestamp_ms } timeFormat="absolute"/>
          </TableCell>
          <TableCell verticalAlign="middle" isNumeric>{ item.sequence }</TableCell>
        </TableRow>
      )) }
    </TableBody>
  </TableRoot>
);

export default PaxeerXFeedObservationsTable;
