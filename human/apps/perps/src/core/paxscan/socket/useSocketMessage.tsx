import { useEffect, useRef } from 'react';

import type { SocketMessageParams } from './types';

export function usePaxscanSocketMessage({ channel, event, handler }: SocketMessageParams) {
  const handlerRef = useRef(handler);
  handlerRef.current = handler;

  useEffect(() => {
    if (channel === undefined || event === undefined) {
      return;
    }

    const ref = channel.on(event, (message) => {
      handlerRef.current?.(message);
    });

    return () => {
      channel.off(event, ref);
    };
  }, [ channel, event ]);
}
