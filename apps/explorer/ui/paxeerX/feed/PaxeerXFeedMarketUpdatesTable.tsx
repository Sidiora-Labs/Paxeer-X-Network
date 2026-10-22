import { Text } from '@chakra-ui/react';
import React from 'react';

import type { MarketUpdate } from 'types/api/paxeerXFeed';

import { route } from 'nextjs/routes';

import { Link } from 'toolkit/chakra/link';
import { Skeleton } from 'toolkit/chakra/skeleton';
import { TableBody, TableCell, TableColumnHeader, TableHeaderSticky, TableRoot, TableRow } from 'toolkit/chakra/table';
import BlockEntity from 'ui/shared/entities/block/BlockEntity';
import HashStringShorten from 'ui/shared/HashStringShorten';
import TimeWithTooltip from 'ui/shared/time/TimeWithTooltip';

import PriceValue from './PriceValue';
import { formatChange, priceChange } from './utils';

interface Props {
  items: Array<MarketUpdate>;
  top?: number;
  isLoading?: boolean;
}

const changeColor = (text: string) => {
  if (text.startsWith('+')) {
    return 'green.500';
  }
  if (text.startsWith('-')) {
    return 'red.500';
  }
  return 'text.secondary';
};

// Rows come newest first, so each row's change is measured against the row below it; the oldest
// row of a page has nothing below it on that page and shows a dash.
const PaxeerXFeedMarketUpdatesTable = ({ items, top = 0, isLoading }: Props) => (
  <TableRoot variant="scan" minW="900px" data-label="paxeer-x-feed-market-updates">
    <TableHeaderSticky top={ top }>
      <TableRow>
        <TableColumnHeader width="22%">Update</TableColumnHeader>
        <TableColumnHeader width="12%">Block</TableColumnHeader>
        <TableColumnHeader width="12%">Age</TableColumnHeader>
        <TableColumnHeader width="17%" isNumeric>Index price</TableColumnHeader>
        <TableColumnHeader width="12%" isNumeric>Change</TableColumnHeader>
        <TableColumnHeader width="17%" isNumeric>Mark price</TableColumnHeader>
        <TableColumnHeader width="8%" isNumeric>Sequence</TableColumnHeader>
      </TableRow>
    </TableHeaderSticky>
    <TableBody>
      { items.map((item, index) => {
        const change = priceChange(item.index_price, items[index + 1]?.index_price);
        const changeText = change ? formatChange(change) : null;

        return (
          <TableRow key={ item.update_hash + (isLoading ? `-${ index }` : '') } data-update={ item.update_hash }>
            <TableCell verticalAlign="middle">
              <Skeleton loading={ isLoading } fontWeight={ 600 }>
                <Link href={ route({ pathname: '/feed/update/[hash]', query: { hash: item.update_hash } }) }>
                  <HashStringShorten hash={ item.update_hash } type="long"/>
                </Link>
              </Skeleton>
            </TableCell>
            <TableCell verticalAlign="middle">
              <BlockEntity number={ item.block_number } isLoading={ isLoading } truncation="none" noIcon/>
            </TableCell>
            <TableCell verticalAlign="middle" color="text.secondary">
              <TimeWithTooltip timestamp={ item.timestamp } isLoading={ isLoading } enableIncrement/>
            </TableCell>
            <TableCell verticalAlign="middle" isNumeric>
              <PriceValue value={ item.index_price } isLoading={ isLoading }/>
            </TableCell>
            <TableCell verticalAlign="middle" isNumeric data-change>
              { changeText ? (
                <Skeleton loading={ isLoading } color={ changeColor(changeText) }>{ changeText }</Skeleton>
              ) : <Text color="text.secondary">—</Text> }
            </TableCell>
            <TableCell verticalAlign="middle" isNumeric>
              <PriceValue value={ item.mark_price } isLoading={ isLoading }/>
            </TableCell>
            <TableCell verticalAlign="middle" isNumeric>
              <Skeleton loading={ isLoading }>{ item.sequence }</Skeleton>
            </TableCell>
          </TableRow>
        );
      }) }
    </TableBody>
  </TableRoot>
);

export default PaxeerXFeedMarketUpdatesTable;
