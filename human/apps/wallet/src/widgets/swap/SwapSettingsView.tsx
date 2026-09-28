'use client';

/**
 * Slippage-tolerance picker rendered when the swap widget enters `settings`
 * view. Quick presets at 0.25% / 0.5% / 1% / 2%, plus a custom-bps field.
 *
 * `slippageBps` is in basis points (1bp = 0.01%). Anything above 300 bps
 * (3%) gets a warning banner.
 */

import { AlertTriangle } from 'lucide-react';
import { SvgIcon } from '@/components/ui/SvgIcon';

const PRESETS_BPS = [25, 50, 100, 200] as const;
const HIGH_SLIP_THRESHOLD_BPS = 300;

export interface SwapSettingsViewProps {
  slippageBps: number;
  customSlippage: string;
  onChangeSlippageBps: (bps: number) => void;
  onChangeCustomSlippage: (raw: string) => void;
  onClose: () => void;
}

export function SwapSettingsView({
  slippageBps,
  customSlippage,
  onChangeSlippageBps,
  onChangeCustomSlippage,
  onClose,
}: SwapSettingsViewProps) {
  return (
    <div className="px-4 pt-4 pb-24">
      <div className="flex items-center justify-between mb-6">
        <h2 className="text-lg font-bold">Swap Settings</h2>
        <button onClick={onClose} className="p-1.5 rounded-full bg-white/5 press-scale">
          <SvgIcon
            name="x"
            className="w-4 h-4"
            style={{ filter: 'brightness(0) invert(0.6)' }}
          />
        </button>
      </div>

      <div className="glass-card p-4 space-y-4">
        <p className="text-sm font-medium">Slippage Tolerance</p>
        <div className="flex gap-2">
          {PRESETS_BPS.map((bps) => (
            <button
              key={bps}
              onClick={() => {
                onChangeSlippageBps(bps);
                onChangeCustomSlippage('');
              }}
              className={`flex-1 py-2 rounded-xl text-xs font-medium transition-all press-scale ${
                slippageBps === bps && !customSlippage
                  ? 'bg-pax-accent/15 text-pax-accent'
                  : 'bg-white/5 text-pax-muted'
              }`}
            >
              {bps / 100}%
            </button>
          ))}
        </div>
        <div className="flex items-center gap-2">
          <input
            type="text"
            inputMode="decimal"
            value={customSlippage}
            onChange={(e) => {
              onChangeCustomSlippage(e.target.value);
              const v = parseFloat(e.target.value);
              if (!isNaN(v) && v > 0 && v < 50) onChangeSlippageBps(Math.round(v * 100));
            }}
            placeholder="Custom %"
            className="flex-1 px-3 py-2.5 rounded-xl bg-white/5   text-sm outline-none  placeholder:text-white/20"
          />
        </div>
        {slippageBps > HIGH_SLIP_THRESHOLD_BPS && (
          <div className="flex items-start gap-2">
            <AlertTriangle className="w-3.5 h-3.5 text-amber-400 mt-0.5 shrink-0" />
            <p className="text-[11px] text-amber-400/80">
              High slippage may result in unfavorable rates
            </p>
          </div>
        )}
      </div>

      <button
        onClick={onClose}
        className="w-full mt-6 py-3.5 rounded-2xl bg-pax-accent text-black font-semibold text-sm press-scale"
      >
        Done
      </button>
    </div>
  );
}
