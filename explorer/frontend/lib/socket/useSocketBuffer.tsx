import React from 'react';

// The chain seals a block every 150-200ms, so a live list that applied every socket message to React
// state would re-render itself several times a second and leave nothing on the page clickable. Every
// live list collects what it receives in a ref instead and hands the collection over on this cadence.
export const SOCKET_FLUSH_INTERVAL_MS = 2000;

export interface SocketBufferParams<T> {
  onFlush: (items: Array<T>) => void;
  isPaused?: boolean;
  limit?: number;
  intervalMs?: number;
}

export interface SocketBufferHoverProps {
  onMouseEnter: () => void;
  onMouseLeave: () => void;
}

export interface SocketBuffer<T> {
  push: (item: T) => void;
  hoverProps: SocketBufferHoverProps;
  isHeld: boolean;
}

export function useIsDocumentVisible() {
  const [ isVisible, setIsVisible ] = React.useState(true);

  React.useEffect(() => {
    const handleVisibilityChange = () => {
      setIsVisible(document.visibilityState !== 'hidden');
    };

    handleVisibilityChange();
    document.addEventListener('visibilitychange', handleVisibilityChange);

    return () => {
      document.removeEventListener('visibilitychange', handleVisibilityChange);
    };
  }, []);

  return isVisible;
}

export default function useSocketBuffer<T>({ onFlush, isPaused, limit, intervalMs = SOCKET_FLUSH_INTERVAL_MS }: SocketBufferParams<T>): SocketBuffer<T> {
  const bufferRef = React.useRef<Array<T>>([]);
  const onFlushRef = React.useRef(onFlush);
  onFlushRef.current = onFlush;

  const [ isHovered, setIsHovered ] = React.useState(false);
  const isDocumentVisible = useIsDocumentVisible();
  const isHeld = isHovered || Boolean(isPaused) || !isDocumentVisible;

  const push = React.useCallback((item: T) => {
    const buffer = bufferRef.current;

    buffer.push(item);

    if (limit !== undefined && buffer.length > limit) {
      buffer.splice(0, buffer.length - limit);
    }
  }, [ limit ]);

  const hoverProps = React.useMemo(() => ({
    onMouseEnter: () => setIsHovered(true),
    onMouseLeave: () => setIsHovered(false),
  }), []);

  React.useEffect(() => {
    if (isHeld) {
      return;
    }

    const intervalId = window.setInterval(() => {
      if (bufferRef.current.length === 0) {
        return;
      }

      const items = bufferRef.current;
      bufferRef.current = [];
      onFlushRef.current(items);
    }, intervalMs);

    return () => {
      window.clearInterval(intervalId);
    };
  }, [ isHeld, intervalMs ]);

  return React.useMemo(() => ({ push, hoverProps, isHeld }), [ push, hoverProps, isHeld ]);
}
