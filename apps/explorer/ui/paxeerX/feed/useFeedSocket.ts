import React from 'react';

import useSocketChannel from 'lib/socket/useSocketChannel';
import { SECOND } from 'toolkit/utils/consts';

// While the channel is joined the pages take every submission from the socket; until it joins, or once
// the socket closes or fails, they fall back to reading the endpoint on this cadence.
export const FEED_FALLBACK_REFETCH_INTERVAL = 5 * SECOND;

interface Params {
  topic: string;
  isDisabled: boolean;
}

export default function useFeedSocket({ topic, isDisabled }: Params) {
  const [ isJoined, setIsJoined ] = React.useState(false);

  const handleJoin = React.useCallback(() => {
    setIsJoined(true);
  }, []);

  const handleSocketIssue = React.useCallback(() => {
    setIsJoined(false);
  }, []);

  const channel = useSocketChannel({
    topic,
    isDisabled,
    onJoin: handleJoin,
    onSocketClose: handleSocketIssue,
    onSocketError: handleSocketIssue,
  });

  const refetchInterval: number | false = isDisabled || isJoined ? false : FEED_FALLBACK_REFETCH_INTERVAL;

  return { channel, refetchInterval, isSocketDown: !isDisabled && !isJoined };
}
