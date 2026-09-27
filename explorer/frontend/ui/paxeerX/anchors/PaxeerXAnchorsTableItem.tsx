import { Flex, Text } from '@chakra-ui/react';
import React from 'react';

import type { PaxeerXAnchorsItem, PaxeerXStatusRung } from 'types/api/paxeerXLists';

import { Skeleton } from 'toolkit/chakra/skeleton';
import { TableCell, TableRow } from 'toolkit/chakra/table';
import CopyToClipboard from 'ui/shared/CopyToClipboard';
import BlockEntity from 'ui/shared/entities/block/BlockEntity';
import HashStringShorten from 'ui/shared/HashStringShorten';
import StatusLadderBadge from 'ui/shared/statusLadder/StatusLadderBadge';
import TimeWithTooltip from 'ui/shared/time/TimeWithTooltip';

// A row of the anchor log is a checkpoint the chain has already accepted, and the payload carries no
// finality flag of its own, so every row sits on the rung the ladder spells for a submitted checkpoint.
export const ANCHOR_SETTLEMENT_RUNG: PaxeerXStatusRung = 'sealed';

interface Props {
  item: PaxeerXAnchorsItem;
  isLoading?: boolean;
}

const PaxeerXAnchorsTableItem = ({ item, isLoading }: Props) => {
  return (
    <TableRow data-anchor={ item.checkpoint_id }>
      <TableCell verticalAlign="middle">
        <Skeleton loading={ isLoading } display="inline-block" fontWeight={ 600 } data-label="checkpoint-height">
          { item.checkpoint_height ?? <Text as="span" color="text.secondary">—</Text> }
        </Skeleton>
      </TableCell>
      <TableCell verticalAlign="middle">
        <Skeleton loading={ isLoading } display="inline-block" data-label="sealed-height">
          { item.sealed_height ?? <Text as="span" color="text.secondary">—</Text> }
        </Skeleton>
      </TableCell>
      <TableCell verticalAlign="middle">
        { item.state_root === null ? (
          <Text color="text.secondary">—</Text>
        ) : (
          <Flex overflow="hidden" w="100%" alignItems="center">
            <Skeleton loading={ isLoading }>
              <HashStringShorten hash={ item.state_root } type="long"/>
            </Skeleton>
            <CopyToClipboard text={ item.state_root } ml={ 2 } isLoading={ isLoading }/>
          </Flex>
        ) }
      </TableCell>
      <TableCell verticalAlign="middle">
        <BlockEntity
          isLoading={ isLoading }
          number={ item.block_number }
          truncation="none"
          fontWeight={ 600 }
          noIcon
        />
      </TableCell>
      <TableCell verticalAlign="middle">
        <TimeWithTooltip
          timestamp={ item.timestamp }
          isLoading={ isLoading }
          display="inline-block"
          color="text.secondary"
        />
      </TableCell>
      <TableCell verticalAlign="middle">
        <StatusLadderBadge rung={ ANCHOR_SETTLEMENT_RUNG } isLoading={ isLoading }/>
      </TableCell>
    </TableRow>
  );
};

export default PaxeerXAnchorsTableItem;
