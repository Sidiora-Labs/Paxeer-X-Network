import React from 'react';

import type { PaxeerXReceiptsItem } from 'types/api/paxeerXLists';

import { TableBody, TableColumnHeader, TableHeaderSticky, TableRoot, TableRow } from 'toolkit/chakra/table';

import PaxeerXReceiptsTableItem from './PaxeerXReceiptsTableItem';

interface Props {
  items: Array<PaxeerXReceiptsItem>;
  top?: number;
  isLoading?: boolean;
}

const PaxeerXReceiptsTable = ({ items, top = 0, isLoading }: Props) => {
  return (
    <TableRoot variant="scan" minW="900px" data-label="paxeer-x-receipts">
      <TableHeaderSticky top={ top }>
        <TableRow>
          <TableColumnHeader width="35%">Receipt ID</TableColumnHeader>
          <TableColumnHeader width="35%">Account</TableColumnHeader>
          <TableColumnHeader width="15%">Block</TableColumnHeader>
          <TableColumnHeader width="15%">Settlement</TableColumnHeader>
        </TableRow>
      </TableHeaderSticky>
      <TableBody>
        { items.map((item, index) => (
          <PaxeerXReceiptsTableItem
            key={ item.id + (isLoading ? index : '') }
            item={ item }
            isLoading={ isLoading }
          />
        )) }
      </TableBody>
    </TableRoot>
  );
};

export default PaxeerXReceiptsTable;
