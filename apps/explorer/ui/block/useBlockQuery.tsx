import type { UseQueryResult } from '@tanstack/react-query';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import React from 'react';
import type { Chain, GetBlockReturnType } from 'viem';

import type { SocketMessage } from 'lib/socket/types';
import type { Block } from 'types/api/block';

import type { ResourceError } from 'lib/api/resources';
import useApiQuery, { getResourceKey } from 'lib/api/useApiQuery';
import { retry } from 'lib/api/useQueryClientConfig';
import { useMultichainContext } from 'lib/contexts/multichain';
import useSocketChannel from 'lib/socket/useSocketChannel';
import useSocketMessage from 'lib/socket/useSocketMessage';
import { publicClient } from 'lib/web3/client';
import formatBlockData from 'lib/web3/rpc/formatBlockData';
import { BLOCK } from 'stubs/block';
import { GET_BLOCK } from 'stubs/RPC';
import { SECOND } from 'toolkit/utils/consts';

type RpcResponseType = GetBlockReturnType<Chain, false, 'latest'> | null;

export type BlockQuery = UseQueryResult<Block, ResourceError<{ status: number }>> & {
  isDegradedData: boolean;
  isFutureBlock: boolean;
};

interface Params {
  heightOrHash: string;
}

export default function useBlockQuery({ heightOrHash }: Params): BlockQuery {
  const [ isRefetchEnabled, setRefetchEnabled ] = React.useState(false);

  const apiQuery = useApiQuery<'general:block', { status: number }>('general:block', {
    pathParams: { height_or_hash: heightOrHash },
    queryOptions: {
      enabled: Boolean(heightOrHash),
      placeholderData: BLOCK,
      refetchOnMount: false,
      retry: (failureCount, error) => {
        if (isRefetchEnabled) {
          return false;
        }

        return retry(failureCount, error);
      },
      refetchInterval: (): number | false => {
        return isRefetchEnabled ? 15 * SECOND : false;
      },
    },
  });

  const latestBlockQuery = useQuery({
    queryKey: [ 'RPC', 'block', 'latest' ],
    queryFn: async() => {
      if (!publicClient) {
        return null;
      }
      return publicClient.getBlock({ blockTag: 'latest' });
    },
    enabled: publicClient !== undefined && (apiQuery.isError || apiQuery.errorUpdateCount > 0),
  });

  const rpcQuery = useQuery<RpcResponseType, unknown, Block | null>({
    queryKey: [ 'RPC', 'block', { heightOrHash } ],
    queryFn: async() => {
      if (!publicClient) {
        return null;
      }

      const blockParams = heightOrHash.startsWith('0x') ? { blockHash: heightOrHash as `0x${ string }` } : { blockNumber: BigInt(heightOrHash) };
      return publicClient.getBlock(blockParams).catch(() => null);
    },
    select: (block) => {
      return formatBlockData(block);
    },
    placeholderData: GET_BLOCK,
    enabled: !latestBlockQuery.isPending,
    retry: false,
    refetchOnMount: false,
  });

  React.useEffect(() => {
    if (apiQuery.isPlaceholderData || !publicClient) {
      return;
    }

    if (apiQuery.isError && apiQuery.errorUpdateCount === 1) {
      setRefetchEnabled(true);
    } else if (!apiQuery.isError) {
      setRefetchEnabled(false);
    }
  }, [ apiQuery.errorUpdateCount, apiQuery.isError, apiQuery.isPlaceholderData ]);

  React.useEffect(() => {
    if (!rpcQuery.isPlaceholderData && !rpcQuery.data) {
      setRefetchEnabled(false);
    }
  }, [ rpcQuery.data, rpcQuery.isPlaceholderData ]);

  // Price updates are indexed after their block, so the block is sent again on blocks:new_block once
  // its submissions are stored; the open block takes the count from that message.
  const queryClient = useQueryClient();
  const multichainContext = useMultichainContext();
  const blockHeight = apiQuery.data?.height;

  const handleNewBlockMessage: SocketMessage.NewBlock['handler'] = React.useCallback((payload) => {
    if (payload.block.height !== blockHeight || payload.block.price_update_count === undefined) {
      return;
    }

    const queryKey = getResourceKey('general:block', {
      pathParams: { height_or_hash: heightOrHash },
      chainId: multichainContext?.chain?.id,
    });

    queryClient.setQueryData(queryKey, (prevData: Block | undefined) => {
      if (!prevData || prevData.height !== payload.block.height || prevData.price_update_count === payload.block.price_update_count) {
        return prevData;
      }

      return { ...prevData, price_update_count: payload.block.price_update_count };
    });
  }, [ blockHeight, heightOrHash, multichainContext?.chain?.id, queryClient ]);

  const channel = useSocketChannel({
    topic: 'blocks:new_block',
    isDisabled: apiQuery.isPlaceholderData || apiQuery.isError || blockHeight === undefined,
  });
  useSocketMessage({
    channel,
    event: 'new_block',
    handler: handleNewBlockMessage,
  });

  const isRpcQuery = Boolean(publicClient && (apiQuery.isError || apiQuery.isPlaceholderData) && apiQuery.errorUpdateCount > 0 && rpcQuery.data);
  const query = isRpcQuery ? rpcQuery as UseQueryResult<Block, ResourceError<{ status: number }>> : apiQuery;

  return {
    ...query,
    isDegradedData: isRpcQuery,
    isFutureBlock: Boolean(
      !heightOrHash.startsWith('0x') &&
      latestBlockQuery.data && Number(latestBlockQuery.data.number) < Number(heightOrHash),
    ),
  };
}
