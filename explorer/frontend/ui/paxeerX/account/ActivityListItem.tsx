import { Flex, Text } from '@chakra-ui/react';
import React from 'react';

import type { PaxeerXActivityItem } from 'types/api/paxeerX';

import { Skeleton } from 'toolkit/chakra/skeleton';
import { TableCell, TableRow } from 'toolkit/chakra/table';
import BlockEntity from 'ui/shared/entities/block/BlockEntity';
import TxEntity from 'ui/shared/entities/tx/TxEntity';
import { ScanMethodChip } from 'ui/shared/scan';
import StatusLadderBadge from 'ui/shared/statusLadder/StatusLadderBadge';
import TimeWithTooltip from 'ui/shared/time/TimeWithTooltip';

import { activityKindLabel, assetLabel, formatAmount } from './utils';

export interface Props {
  item: PaxeerXActivityItem;
  isLoading?: boolean;
}

const SIDE_LABELS: Record<PaxeerXActivityItem['side'], string> = {
  chain: 'Chain',
  kernel: 'Kernel',
};

const ActivityListItem = ({ item, isLoading }: Props) => {
  const amount = item.amount === null ?
    null :
    `${ formatAmount(item.amount, item.asset) } ${ assetLabel(item.asset) }`;

  return (
    <TableRow data-activity={ item.hash }>
      <TableCell verticalAlign="middle">
        <Flex flexDirection="column" rowGap={ 1 } alignItems="flex-start">
          <ScanMethodChip method={ activityKindLabel(item.kind) } isLoading={ isLoading }/>
          <Skeleton loading={ isLoading } color="text.secondary" textStyle="xs">{ SIDE_LABELS[item.side] }</Skeleton>
        </Flex>
      </TableCell>
      <TableCell verticalAlign="middle">
        <TxEntity
          hash={ item.hash }
          isLoading={ isLoading }
          truncation="constant_long"
          fontWeight={ 600 }
          noIcon
        />
      </TableCell>
      <TableCell verticalAlign="middle">
        <BlockEntity
          number={ item.block_number }
          isLoading={ isLoading }
          truncation="none"
          fontWeight={ 500 }
          noIcon
        />
        <TimeWithTooltip timestamp={ item.timestamp } isLoading={ isLoading } color="text.secondary" textStyle="xs" display="block"/>
      </TableCell>
      <TableCell verticalAlign="middle" isNumeric>
        <Skeleton loading={ isLoading } display="inline-block">
          { amount === null ? <Text as="span" color="text.secondary">—</Text> : amount }
        </Skeleton>
      </TableCell>
      <TableCell verticalAlign="middle">
        <StatusLadderBadge rung={ item.status } isLoading={ isLoading }/>
      </TableCell>
    </TableRow>
  );
};

export default React.memo(ActivityListItem);
