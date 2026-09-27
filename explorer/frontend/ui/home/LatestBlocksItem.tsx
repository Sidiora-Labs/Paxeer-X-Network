import { Box, Center, chakra, Flex } from '@chakra-ui/react';
import React from 'react';

import type { Block } from 'types/api/block';

import config from 'configs/app';
import getBlockTotalReward from 'lib/block/getBlockTotalReward';
import { currencyUnits } from 'lib/units';
import { Skeleton } from 'toolkit/chakra/skeleton';
import { Tag } from 'toolkit/chakra/tag';
import { Tooltip } from 'toolkit/chakra/tooltip';
import { thinsp } from 'toolkit/utils/htmlEntities';
import BlockEntity, { Link as BlockEntityLink } from 'ui/shared/entities/block/BlockEntity';
import HashStringShorten from 'ui/shared/HashStringShorten';
import IconSvg from 'ui/shared/IconSvg';
import TimeWithTooltip from 'ui/shared/time/TimeWithTooltip';
import SimpleValue from 'ui/shared/value/SimpleValue';

type Props = {
  block: Block;
  isLoading?: boolean;
};

const hasReward = !config.features.rollup.isEnabled && !config.UI.views.block.hiddenFields?.total_reward;

const LatestBlocksItem = ({ block, isLoading }: Props) => {
  const totalReward = getBlockTotalReward(block);

  return (
    <Flex
      data-latest-block={ block.height }
      alignItems="center"
      columnGap={ 3 }
      px={{ base: 3, lg: 4 }}
      py={ 3 }
      borderBottomWidth="1px"
      borderStyle="solid"
      borderColor="border.divider"
    >
      <Center
        boxSize={ 9 }
        flexShrink={ 0 }
        borderRadius="md"
        borderWidth="1px"
        borderStyle="solid"
        borderColor="border.divider"
      >
        <IconSvg name="block" boxSize={ 5 } color="icon.secondary" isLoading={ isLoading }/>
      </Center>
      <Box minW={ 0 } flexShrink={ 0 } w={{ base: '96px', lg: '116px' }}>
        <BlockEntity
          isLoading={ isLoading }
          number={ block.height }
          noIcon
          tailLength={ 2 }
          textStyle="sm"
          fontWeight="500"
        />
        <TimeWithTooltip
          timestamp={ block.timestamp }
          enableIncrement={ !isLoading }
          timeFormat="relative"
          isLoading={ isLoading }
          color="text.secondary"
          textStyle="xs"
          display="block"
          mt="2px"
        />
      </Box>
      <Box minW={ 0 } flexGrow={ 1 }>
        <Flex alignItems="center" columnGap={ 1 } minW={ 0 }>
          <Skeleton loading={ isLoading } textStyle="sm" fontWeight="500" flexShrink={ 0 }>Hash</Skeleton>
          <BlockEntityLink hash={ block.hash } isLoading={ isLoading } textStyle="sm" overflow="hidden">
            <HashStringShorten hash={ block.hash } type="long"/>
          </BlockEntityLink>
        </Flex>
        <Skeleton loading={ isLoading } textStyle="xs" color="text.secondary" w="fit-content" mt="2px">
          <chakra.span>{ block.transactions_count } { block.transactions_count === 1 ? 'txn' : 'txns' }</chakra.span>
        </Skeleton>
      </Box>
      { block.celo?.l1_era_finalized_epoch_number && (
        <Tooltip content={ `Finalized epoch #${ block.celo.l1_era_finalized_epoch_number }` }>
          <IconSvg name="checkered_flag" boxSize={ 5 } p="1px" isLoading={ isLoading } flexShrink={ 0 }/>
        </Tooltip>
      ) }
      { hasReward && (
        <Tag variant="outlined" loading={ isLoading } flexShrink={ 0 } data-label="block-reward">
          <SimpleValue
            value={ totalReward }
            loading={ isLoading }
            endElement={ `${ thinsp }${ currencyUnits.ether }` }
          />
        </Tag>
      ) }
    </Flex>
  );
};

// The list hands its rows back through the React Query cache, which rebuilds the array positionally, so a
// block that only moved one place down arrives as an equal object under a new identity. The row compares the
// fields it renders instead of the object, so a flush that adds one block renders that one row.
const rewardsKey = (block: Block) => block.rewards?.map(({ type, reward }) => `${ type }:${ reward }`).join(',') ?? '';

const areRowPropsEqual = (prev: Props, next: Props) => (
  prev.isLoading === next.isLoading &&
  prev.block.height === next.block.height &&
  prev.block.hash === next.block.hash &&
  prev.block.timestamp === next.block.timestamp &&
  prev.block.transactions_count === next.block.transactions_count &&
  prev.block.celo?.l1_era_finalized_epoch_number === next.block.celo?.l1_era_finalized_epoch_number &&
  rewardsKey(prev.block) === rewardsKey(next.block)
);

export default React.memo(LatestBlocksItem, areRowPropsEqual);
