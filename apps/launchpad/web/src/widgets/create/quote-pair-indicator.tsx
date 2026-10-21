'use client';

import { Coins } from 'lucide-react';
import { cn } from '@/lib/utils';
import { ShortAddress } from '@/ui/primitives';
import { CORE_DEPLOYMENT } from '@/core/network/deployment';
import { safeExternalDestination } from '@/core/app-foundation/navigation';
import { InfoTip } from './info-tip';

export const QUOTE_PAIR_LABEL = 'Paired with USDL';

export function QuotePairIndicator({ className }: { className?: string }) {
  const usdl = CORE_DEPLOYMENT.usdl;
  const explorer = safeExternalDestination(`https://paxscan.paxeer.app/address/${usdl}`);
  const detail = 'Every coin on Sidiora trades against USDL, so there is nothing to pick here.';
  return (
    <div className={cn('flex flex-col gap-2', className)}>
      <p className="flex items-center gap-1 text-sm font-bold text-text">
        Pool liquidity pair
        <InfoTip label="About the pair" content={detail} />
      </p>
      <div className="flex flex-wrap items-center gap-3">
        <span className="inline-flex h-control-height-lg items-center gap-2 rounded-md bg-accent-soft px-3 text-sm font-bold text-text">
          <Coins aria-hidden="true" className="size-icon-md text-accent" />
          {QUOTE_PAIR_LABEL}
        </span>
        <ShortAddress address={usdl} explorerHref={explorer} />
      </div>
    </div>
  );
}
