import { Box, chakra, Flex, Text } from '@chakra-ui/react';
import { useQueryClient } from '@tanstack/react-query';
import { upperFirst } from 'es-toolkit';
import React from 'react';

import type { SocketMessage } from 'lib/socket/types';
import type { Block } from 'types/api/block';

import { route } from 'nextjs-routes';

import config from 'configs/app';
import useApiQuery, { getResourceKey } from 'lib/api/useApiQuery';
import dayjs from 'lib/date/dayjs';
import useIsMobile from 'lib/hooks/useIsMobile';
import getNetworkUtilizationParams from 'lib/networks/getNetworkUtilizationParams';
import useSocketBuffer from 'lib/socket/useSocketBuffer';
import useSocketChannel from 'lib/socket/useSocketChannel';
import useSocketMessage from 'lib/socket/useSocketMessage';
import { BLOCK } from 'stubs/block';
import { HOMEPAGE_STATS } from 'stubs/stats';
import { Link } from 'toolkit/chakra/link';
import { Skeleton } from 'toolkit/chakra/skeleton';
import { Tooltip } from 'toolkit/chakra/tooltip';
import { nbsp } from 'toolkit/utils/htmlEntities';
import FallbackRpcIcon from 'ui/shared/fallbacks/FallbackRpcIcon';
import IconSvg from 'ui/shared/IconSvg';
import { ScanTableCard } from 'ui/shared/scan';

import LatestBlocksDegraded from './fallbacks/LatestBlocksDegraded';
import { useHomeRpcDataContext } from './fallbacks/rpcDataContext';
import LatestBlocksItem from './LatestBlocksItem';

const BLOCKS_MAX_COUNT_DESKTOP = 6;

function getBlockDurations(blocks: Array<Block>): Array<number | undefined> {
  return blocks.map((block, index) => {
    const parent = blocks[index + 1];

    if (!parent || parent.height !== block.height - 1) {
      return undefined;
    }

    const seconds = dayjs(block.timestamp).diff(dayjs(parent.timestamp), 'second');

    return seconds >= 0 ? seconds : undefined;
  });
}

const LatestBlocks = () => {
  const isMobile = useIsMobile();
  let blocksMaxCount: number;
  if (!isMobile) {
    blocksMaxCount = BLOCKS_MAX_COUNT_DESKTOP;
  } else if (config.features.rollup.isEnabled || config.UI.views.block.hiddenFields?.total_reward) {
    blocksMaxCount = 4;
  } else {
    blocksMaxCount = 2;
  }
  const { data, isPlaceholderData, isError } = useApiQuery('general:homepage_blocks', {
    queryOptions: {
      placeholderData: Array(blocksMaxCount).fill(BLOCK),
    },
  });

  const queryClient = useQueryClient();
  const statsQueryResult = useApiQuery('general:stats', {
    queryOptions: {
      refetchOnMount: false,
      placeholderData: HOMEPAGE_STATS,
    },
  });

  const rpcDataContext = useHomeRpcDataContext();
  const isRpcData = rpcDataContext.isEnabled && !rpcDataContext.isLoading && !rpcDataContext.isError && rpcDataContext.subscriptions.includes('latest-blocks');

  const handleFlush = React.useCallback((blocks: Array<Block>) => {
    queryClient.setQueryData(getResourceKey('general:homepage_blocks'), (prevData: Array<Block> | undefined) => {
      const prevLength = prevData?.length ?? 0;
      const nextData = prevData ? [ ...prevData ] : [];
      const heights = new Set(nextData.map((block) => block.height));

      blocks.forEach((block) => {
        if (heights.has(block.height)) {
          return;
        }

        heights.add(block.height);
        nextData.push(block);
      });

      if (nextData.length === prevLength) {
        return prevData;
      }

      return nextData.sort((b1, b2) => b2.height - b1.height).slice(0, blocksMaxCount);
    });
  }, [ queryClient, blocksMaxCount ]);

  const { push: pushBlock, hoverProps } = useSocketBuffer<Block>({
    onFlush: handleFlush,
    limit: blocksMaxCount,
  });

  const handleNewBlockMessage: SocketMessage.NewBlock['handler'] = React.useCallback((payload) => {
    pushBlock(payload.block);
  }, [ pushBlock ]);

  const channel = useSocketChannel({
    topic: 'blocks:new_block',
    isDisabled: isPlaceholderData || isError,
  });
  useSocketMessage({
    channel,
    event: 'new_block',
    handler: handleNewBlockMessage,
  });

  const dataToShow = React.useMemo(() => data?.slice(0, blocksMaxCount) ?? [], [ data, blocksMaxCount ]);
  const durations = React.useMemo(() => isPlaceholderData ? [] : getBlockDurations(dataToShow), [ dataToShow, isPlaceholderData ]);

  const networkUtilization = getNetworkUtilizationParams(statsQueryResult.data?.network_utilization_percentage ?? 0);

  const note = (
    <>
      { statsQueryResult.data?.network_utilization_percentage !== undefined && (
        <Skeleton loading={ statsQueryResult.isPlaceholderData } display="inline-block" textStyle="xs">
          <Text as="span" color="text.muted">
            Network utilization:{ nbsp }
          </Text>
          <Tooltip content={ `${ upperFirst(networkUtilization.load) } load` }>
            <Text as="span" color={ networkUtilization.color } fontWeight="700">
              { statsQueryResult.data?.network_utilization_percentage.toFixed(2) }%
            </Text>
          </Tooltip>
        </Skeleton>
      ) }
      { statsQueryResult.data?.celo && (
        <Box whiteSpace="pre-wrap" textStyle="xs" color="text.muted">
          <span>Current epoch: </span>
          <chakra.span fontWeight="700">#{ statsQueryResult.data.celo.epoch_number }</chakra.span>
        </Box>
      ) }
    </>
  );

  const content = (() => {
    if (isError) {
      return <Box px={{ base: 3, lg: 4 }} py={ 3 }><LatestBlocksDegraded maxNum={ blocksMaxCount }/></Box>;
    }

    if (dataToShow.length > 0) {
      return (
        <>
          <Box data-label="latest-blocks-rows" { ...hoverProps }>
            { dataToShow.map(((block, index) => (
              <LatestBlocksItem
                key={ String(block.height) + (isPlaceholderData ? String(index) : '') }
                block={ block }
                duration={ durations[index] }
                isLoading={ isPlaceholderData }
              />
            ))) }
          </Box>
          <Flex data-label="view-all-blocks" justifyContent="center" px={ 4 } py={ 3 } borderTopWidth="1px" borderStyle="solid" borderColor="border.divider">
            <Link
              textStyle="xs"
              fontWeight="600"
              textTransform="uppercase"
              letterSpacing="wide"
              href={ route({ pathname: '/blocks' }) }
              loading={ isPlaceholderData }
            >
              View all blocks<IconSvg name="arrows/east-mini" boxSize={ 4 } ml={ 1 }/>
            </Link>
          </Flex>
        </>
      );
    }

    return <Box px={{ base: 3, lg: 4 }} py={ 3 } textStyle="sm">No latest blocks found.</Box>;
  })();

  return (
    <ScanTableCard
      title="Latest blocks"
      note={ note }
      actions={ isRpcData ? <FallbackRpcIcon/> : undefined }
    >
      { content }
    </ScanTableCard>
  );
};

export default LatestBlocks;
