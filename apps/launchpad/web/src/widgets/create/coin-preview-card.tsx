'use client';

import { cn } from '@/lib/utils';
import { CoinCard } from '@/ui/primitives';
import { feeStrategyLabel } from './strategy-picker';
import type { FeeStrategy } from './use-create-prefill';

export interface CoinPreviewCardProps {
  name: string;
  symbol: string;
  description: string;
  image: string | null;
  strategy: FeeStrategy;
  socialLinks: readonly string[];
  className?: string;
}

function hostLabel(link: string): string {
  try {
    return new URL(link).hostname.replace(/^www\./, '');
  } catch {
    return link;
  }
}

export function CoinPreviewCard({ name, symbol, description, image, strategy, socialLinks, className }: CoinPreviewCardProps) {
  const empty = !name.trim() && !symbol.trim() && !image;
  return (
    <section aria-labelledby="create-preview-title" className={cn('flex flex-col gap-3', className)}>
      <h2 id="create-preview-title" className="text-md font-semibold text-text">Preview</h2>
      {empty ? (
        <div data-create-preview-frame className="flex min-h-40 items-center justify-center rounded-lg bg-surface-1 p-6 lg:min-h-72 text-center">
          <p className="text-sm text-text-muted">A preview of how your coin will look</p>
        </div>
      ) : (
        <div data-create-preview-frame className="pointer-events-none flex flex-col gap-3 lg:min-h-72 rounded-lg bg-surface-1 p-3" aria-label="Coin card preview">
          <CoinCard
            image={image}
            name={name.trim() || 'Untitled coin'}
            symbol={symbol.trim() || 'TICKER'}
            mcapRaw={null}
            percentChange={null}
            createdAt={null}
            href="/create"
            surface="surface-1"
            footerStats={[
              { label: 'Rewards to', value: feeStrategyLabel(strategy) },
              { label: 'Paired with', value: 'USDL' },
              { label: 'Rules', value: 'Standard' },
            ]}
          />
          {description.trim() ? <p className="line-clamp-4 text-xs text-text-muted">{description}</p> : null}
          {socialLinks.length > 0 ? (
            <ul className="flex flex-wrap gap-1.5">
              {socialLinks.map(link => (
                <li key={link} className="inline-flex h-[22px] items-center rounded-pill bg-surface-2 px-2 text-xs text-text-muted">{hostLabel(link)}</li>
              ))}
            </ul>
          ) : null}
          <p className="mt-auto text-2xs text-text-faint">Market cap shows up after the first trade</p>
        </div>
      )}
    </section>
  );
}
