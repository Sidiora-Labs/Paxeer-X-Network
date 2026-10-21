'use client';

import type { ReactNode } from 'react';
import { Button } from '@/ui/primitives';

export const launchHolderRewardsHref = '/create?strategy=airdrop';

/**
 * Page hero: one surface-1 panel spanning the content width that carries the
 * title, two lines of copy, the launch CTA and (as children) the stat bar.
 * The panel separates from the canvas by tone alone.
 */
export default function HolderRewardsHeader({ children }: { children?: ReactNode }) {
  return (
    <header className="flex flex-col gap-5 rounded-lg bg-surface-1 p-4 sm:p-6">
      <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
        <div className="flex min-w-0 flex-col gap-2">
          <h1 className="text-[28px] font-medium leading-[34px] tracking-[-.01em] text-text sm:text-[40px] sm:leading-[46px]">Holder rewards coins</h1>
          <p className="max-w-[62ch] text-base text-text-muted sm:text-md">
            Coins that pay their trading fees out to holders. Rewards are paid in USDL
          </p>
        </div>
        <Button variant="primary" size="lg" pill href={launchHolderRewardsHref} className="shrink-0 self-start">
          Launch a holder rewards coin
        </Button>
      </div>
      {children}
    </header>
  );
}
