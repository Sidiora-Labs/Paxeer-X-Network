import React from 'react';

import type { PaxeerXAnchorsItem } from 'types/api/paxeerXLists';

import { TableBody, TableColumnHeader, TableHeaderSticky, TableRoot, TableRow } from 'toolkit/chakra/table';
import TimeFormatToggle from 'ui/shared/time/TimeFormatToggle';

import PaxeerXAnchorsTableItem from './PaxeerXAnchorsTableItem';

interface Props {
  items: Array<PaxeerXAnchorsItem>;
  top?: number;
  isLoading?: boolean;
}

const PaxeerXAnchorsTable = ({ items, top = 0, isLoading }: Props) => {
  return (
    <TableRoot variant="scan" minW="1000px" data-label="paxeer-x-anchors">
      <TableHeaderSticky top={ top }>
        <TableRow>
          <TableColumnHeader width="150px">Checkpoint height</TableColumnHeader>
          <TableColumnHeader width="150px">Sealed height</TableColumnHeader>
          <TableColumnHeader width="30%">State root</TableColumnHeader>
          <TableColumnHeader width="15%">Block</TableColumnHeader>
          <TableColumnHeader width="15%">
            Age
            <TimeFormatToggle/>
          </TableColumnHeader>
          <TableColumnHeader width="130px">Settlement</TableColumnHeader>
        </TableRow>
      </TableHeaderSticky>
      <TableBody>
        { items.map((item, index) => (
          <PaxeerXAnchorsTableItem
            key={ item.checkpoint_id + (isLoading ? index : '') }
            item={ item }
            isLoading={ isLoading }
          />
        )) }
      </TableBody>
    </TableRoot>
  );
};

export default PaxeerXAnchorsTable;
