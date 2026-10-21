'use client';
import { Sparkline, UnavailableState } from '@/ui/primitives';
import { useSparkline } from '@/core/market/use-sparkline';
import { toUnsupported } from '@/core/market/unsupported';
import { cn } from '@/lib/utils';

export interface StreamSparklineProps {
  poolAddress: string;
  /** `overlay`: 90x30 decorative mark on a thumbnail. `hero`: 460x120 chart scaled to its container. */
  variant?: 'overlay' | 'hero';
  className?: string;
}

const sizes = { overlay: { width: 90, height: 30 }, hero: { width: 460, height: 120 } } as const;

/** 24 x 1h closes for the stream's pool through IF_CANDLES (shared useSparkline cache). */
export function StreamSparkline({ poolAddress, variant = 'overlay', className }: StreamSparklineProps) {
  const query = useSparkline(poolAddress);
  const { width, height } = sizes[variant];
  const decorative = variant === 'overlay';
  const sizing = variant === 'hero' ? 'h-auto w-full' : undefined;

  if (query.isPending) return <Sparkline data={[]} width={width} height={height} loading className={cn(sizing, className)} />;
  if (query.isError) {
    if (decorative) return null;
    const marker = toUnsupported('IF_CANDLES', query.error);
    return (
      <UnavailableState
        variant="inline"
        title="Price chart"
        operation="IF_CANDLES"
        status={marker?.status ?? null}
        message={marker?.message ?? query.error.message}
        onRetry={() => { void query.refetch(); }}
        className={className}
      />
    );
  }
  return (
    <Sparkline
      data={query.data.closes}
      width={width}
      height={height}
      label="24h price"
      decorative={decorative}
      className={cn(sizing, className)}
    />
  );
}
