'use client';

import { useState } from 'react';
import { TriangleAlert } from 'lucide-react';
import { Button, ModalFrame } from '@/ui/primitives';
import { safeFixed } from '@/utils/format';
import { minimumOutput } from '@/widgets/trading/transaction-controls/domain';
import { TradeSettingsPopover } from '@/widgets/trading/trade-settings-popover';

export const DEFAULT_SLIPPAGE_BPS = 100;

export function SlippageSelector({ bps, onChange }: { bps: number; onChange: (v: number) => void }) {
  return <TradeSettingsPopover slippageBps={bps} onSlippageChange={onChange} />;
}

export function computeMinOut(quotedOut: bigint, slippageBps: number): bigint {
  return minimumOutput(quotedOut, slippageBps);
}

export function HighImpactWarning({ priceImpactBps, onConfirm, onCancel }: { priceImpactBps: number; onConfirm: () => void; onCancel: () => void }) {
  const pct = safeFixed(Number(priceImpactBps) / 100, 2);
  return (
    <ModalFrame
      open
      onClose={onCancel}
      size="sm"
      closeOnScrim={false}
      title="High price impact"
      footer={(
        <>
          <Button variant="secondary" onClick={onCancel}>Cancel</Button>
          <Button variant="danger" onClick={onConfirm}>Trade anyway</Button>
        </>
      )}
    >
      <div className="flex items-start gap-3 rounded-sm bg-danger-soft p-3">
        <TriangleAlert aria-hidden="true" className="mt-0.5 size-icon-md shrink-0 text-danger" />
        <p className="text-sm text-text">
          This trade moves the price by <span className="font-bold text-danger tabular-nums">{pct}%</span>. You may receive far less than the quoted amount.
        </p>
      </div>
    </ModalFrame>
  );
}

export function TradeRiskReview({ slippageBps, impactBps, onConfirm, onCancel }: { slippageBps: number; impactBps: number; onConfirm: (slippage: boolean, impact: boolean) => void; onCancel: () => void }) {
  const [slippage, setSlippage] = useState(false);
  const [impact, setImpact] = useState(false);
  const needsSlippage = slippageBps > 500;
  const needsImpact = impactBps > 1000;
  const ready = (!needsSlippage || slippage) && (!needsImpact || impact);
  return (
    <ModalFrame
      open
      onClose={onCancel}
      size="sm"
      closeOnScrim={false}
      title="Before you trade"
      subtitle="This trade could cost you more than usual."
      footer={(
        <>
          <Button variant="secondary" onClick={onCancel}>Cancel</Button>
          <Button variant="danger" disabled={!ready} onClick={() => onConfirm(slippage, impact)}>Continue</Button>
        </>
      )}
    >
      <div className="space-y-2">
        {needsSlippage ? (
          <label className="flex cursor-pointer items-start gap-3 rounded-sm bg-warning-soft p-3 text-sm text-text">
            <input type="checkbox" checked={slippage} onChange={event => setSlippage(event.target.checked)} className="mt-0.5 size-4 shrink-0 accent-[var(--sl-color-warning)]" />
            <span>I&apos;m okay with <span className="font-bold tabular-nums">{safeFixed(slippageBps / 100, 2)}%</span> slippage</span>
          </label>
        ) : null}
        {needsImpact ? (
          <label className="flex cursor-pointer items-start gap-3 rounded-sm bg-danger-soft p-3 text-sm text-text">
            <input type="checkbox" checked={impact} onChange={event => setImpact(event.target.checked)} className="mt-0.5 size-4 shrink-0 accent-[var(--sl-color-danger)]" />
            <span>I&apos;m okay with a <span className="font-bold tabular-nums">{safeFixed(impactBps / 100, 2)}%</span> price impact</span>
          </label>
        ) : null}
      </div>
    </ModalFrame>
  );
}
