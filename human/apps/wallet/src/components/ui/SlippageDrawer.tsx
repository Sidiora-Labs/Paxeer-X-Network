'use client';

import { useState, useEffect } from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import { AlertTriangle, Settings2 } from 'lucide-react';
import { SvgIcon } from '@/components/ui/SvgIcon';

const PRESET_BPS = [25, 50, 100, 200];

interface SlippageDrawerProps {
  open: boolean;
  onClose: () => void;
  slippageBps: number;
  onChange: (bps: number) => void;
}

export function SlippageDrawer({
  open,
  onClose,
  slippageBps,
  onChange,
}: SlippageDrawerProps) {
  const [customRaw, setCustomRaw] = useState('');

  // Sync custom field when drawer opens with a non-preset value
  useEffect(() => {
    if (open && !PRESET_BPS.includes(slippageBps)) {
      setCustomRaw((slippageBps / 100).toFixed(2).replace(/\.?0+$/, ''));
    } else if (open) {
      setCustomRaw('');
    }
  }, [open, slippageBps]);

  const isCustom = !PRESET_BPS.includes(slippageBps);

  const handleCustomChange = (value: string) => {
    setCustomRaw(value);
    const v = parseFloat(value);
    if (!isNaN(v) && v > 0 && v < 50) {
      onChange(Math.round(v * 100));
    }
  };

  const handlePreset = (bps: number) => {
    onChange(bps);
    setCustomRaw('');
  };

  const riskLevel = slippageBps < 50 ? 'low' : slippageBps > 300 ? 'high' : 'normal';

  return (
    <AnimatePresence>
      {open && (
        <div className="fixed inset-0 z-[60] flex items-end justify-center">
          {/* Backdrop */}
          <motion.div
            key="slippage-backdrop"
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.2 }}
            className="absolute inset-0 bg-black/60 backdrop-blur-sm"
            onClick={onClose}
          />

          {/* Sheet */}
          <motion.div
            key="slippage-sheet"
            initial={{ y: '100%', opacity: 0 }}
            animate={{ y: 0, opacity: 1 }}
            exit={{ y: '100%', opacity: 0 }}
            transition={{ type: 'spring', stiffness: 300, damping: 30, mass: 0.8 }}
            className="relative w-full max-w-md bg-pax-card rounded-t-3xl p-5"
            style={{ paddingBottom: 'max(5.5rem, calc(env(safe-area-inset-bottom, 0px) + 5.5rem))' }}
          >
            <div className="w-10 h-1 rounded-full bg-white/10 absolute top-2.5 left-1/2 -translate-x-1/2" />

            {/* Header */}
            <div className="flex items-center justify-between mb-5 pt-2">
              <div className="flex items-center gap-2">
                <Settings2 className="w-4 h-4 text-pax-muted" />
                <h3 className="text-base font-bold">Swap Settings</h3>
              </div>
              <button
                onClick={onClose}
                className="p-1.5 rounded-full bg-white/5 press-scale"
              >
                <SvgIcon name="x" className="w-4 h-4" style={{ filter: 'brightness(0) invert(0.6)' }} />
              </button>
            </div>

            {/* Slippage section */}
            <div className="space-y-3">
              <div className="flex items-center justify-between">
                <p className="text-sm font-medium">Slippage Tolerance</p>
                <span className={`text-sm font-semibold ${
                  riskLevel === 'high' ? 'text-red-400' : riskLevel === 'low' ? 'text-amber-400' : 'text-pax-accent'
                }`}>
                  {slippageBps / 100}%
                </span>
              </div>

              {/* Preset buttons */}
              <div className="grid grid-cols-4 gap-2">
                {PRESET_BPS.map((bps) => (
                  <button
                    key={bps}
                    onClick={() => handlePreset(bps)}
                    className={`py-2.5 rounded-xl text-xs font-semibold transition-all press-scale ${
                      slippageBps === bps && !isCustom
                        ? 'bg-pax-accent/15 text-pax-accent'
                        : 'bg-white/[0.06] text-pax-muted hover:bg-white/10'
                    }`}
                  >
                    {bps / 100}%
                  </button>
                ))}
              </div>

              {/* Custom input */}
              <div className="relative">
                <input
                  type="text"
                  inputMode="decimal"
                  value={customRaw}
                  onChange={(e) => handleCustomChange(e.target.value)}
                  onFocus={() => {
                    if (!isCustom) setCustomRaw('');
                  }}
                  placeholder="Custom %"
                  className={`w-full px-3 py-2.5 rounded-xl bg-white/[0.06]  text-sm outline-none transition-colors placeholder:text-white/20 ${
                    isCustom
                      ? ' text-white'
                      : ' text-pax-muted '
                  }`}
                />
                {customRaw && (
                  <span className="absolute right-3 top-1/2 -translate-y-1/2 text-xs text-pax-muted">%</span>
                )}
              </div>

              {/* Warnings */}
              {riskLevel === 'high' && (
                <div className="flex items-start gap-2 rounded-xl bg-red-500/8   px-3 py-2.5">
                  <AlertTriangle className="w-3.5 h-3.5 text-red-400 mt-0.5 shrink-0" />
                  <p className="text-[11px] text-red-300/80 leading-relaxed">
                    High slippage ({slippageBps / 100}%) may result in unfavorable swap rates or front-running.
                  </p>
                </div>
              )}
              {riskLevel === 'low' && (
                <div className="flex items-start gap-2 rounded-xl bg-amber-500/8   px-3 py-2.5">
                  <AlertTriangle className="w-3.5 h-3.5 text-amber-400 mt-0.5 shrink-0" />
                  <p className="text-[11px] text-amber-300/80 leading-relaxed">
                    Very low slippage may cause your transaction to fail if the price moves slightly.
                  </p>
                </div>
              )}
            </div>

            <button
              onClick={onClose}
              className="w-full mt-5 py-3.5 rounded-2xl bg-pax-accent text-black font-semibold text-sm press-scale"
            >
              Done
            </button>
          </motion.div>
        </div>
      )}
    </AnimatePresence>
  );
}
