'use client';
import { useEffect, useState } from 'react';
import { Button, FormField, ModalFrame, SegmentedControl } from '@/ui/primitives';
import { maxSlippageBps, minSlippageBps, presetIds, validBuyAmount, validSellPercent, validSlippage, type PresetId, type TradePreset } from '../presets-store';

interface Draft { buyAmounts: string[]; sellPercents: string[]; slippageBps: string }

function toDraft(preset: TradePreset): Draft {
  return { buyAmounts: [...preset.buyAmounts], sellPercents: preset.sellPercents.map(String), slippageBps: String(preset.slippageBps) };
}

export interface DraftErrors { buy: (string | null)[]; sell: (string | null)[]; slippage: string | null }

/** Field-level validation of one preset draft; returns the parsed preset when every field is valid. */
export function validateDraft(draft: Draft): { preset: TradePreset | null; errors: DraftErrors } {
  const buy = draft.buyAmounts.map(value => (validBuyAmount(value.trim()) ? null : 'Enter a USDL amount above 0'));
  const sell = draft.sellPercents.map(value => (/^\d+$/.test(value.trim()) && validSellPercent(Number(value)) ? null : 'Enter a whole number from 1 to 100'));
  const slippage = /^\d+$/.test(draft.slippageBps.trim()) && validSlippage(Number(draft.slippageBps)) ? null : `Enter a number from ${minSlippageBps} to ${maxSlippageBps} (100 = 1%)`;
  const errors = { buy, sell, slippage };
  if (buy.some(Boolean) || sell.some(Boolean) || slippage) return { preset: null, errors };
  return {
    preset: {
      buyAmounts: draft.buyAmounts.map(value => value.trim()) as TradePreset['buyAmounts'],
      sellPercents: draft.sellPercents.map(Number) as TradePreset['sellPercents'],
      slippageBps: Number(draft.slippageBps),
    },
    errors,
  };
}

export function PresetEditor({ open, onClose, presets, initial, onSave }: { open: boolean; onClose: () => void; presets: Record<PresetId, TradePreset>; initial: PresetId; onSave: (id: PresetId, preset: TradePreset) => void }) {
  const [editing, setEditing] = useState<PresetId>(initial);
  const [draft, setDraft] = useState<Draft>(() => toDraft(presets[initial]));
  const [showErrors, setShowErrors] = useState(false);
  useEffect(() => { if (open) { setEditing(initial); setDraft(toDraft(presets[initial])); setShowErrors(false); } }, [open, initial, presets]);
  const switchTo = (id: PresetId) => { setEditing(id); setDraft(toDraft(presets[id])); setShowErrors(false); };
  const { preset, errors } = validateDraft(draft);
  const save = () => {
    if (!preset) { setShowErrors(true); return; }
    onSave(editing, preset);
    onClose();
  };
  const setAt = (field: 'buyAmounts' | 'sellPercents', index: number, value: string) => setDraft(previous => ({ ...previous, [field]: previous[field].map((item, i) => (i === index ? value : item)) }));

  return (
    <ModalFrame
      open={open}
      onClose={onClose}
      title="Trade presets"
      subtitle="Quick buy and sell buttons and slippage, saved on this device."
      size="sm"
      footer={(
        <div className="flex justify-end gap-2">
          <Button variant="ghost" size="md" onClick={onClose}>Cancel</Button>
          <Button variant="primary" size="md" onClick={save}>Save P{editing}</Button>
        </div>
      )}
    >
      <div className="flex flex-col gap-4">
        <SegmentedControl<`${PresetId}`> options={presetIds.map(id => ({ value: `${id}` as `${PresetId}`, label: `P${id}` }))} value={`${editing}`} onChange={value => switchTo(Number(value) as PresetId)} label="Preset to edit" size="sm" fullWidth />
        <fieldset className="m-0 flex flex-col gap-2 border-0 p-0">
          <legend className="mb-2 text-xs font-bold text-text-muted">Buy amounts (USDL)</legend>
          <div className="grid grid-cols-2 gap-2">
            {draft.buyAmounts.map((value, index) => (
              <FormField key={index} id={`preset-buy-${index}`} label={`Buy ${index + 1}`} surface="surface-2" error={showErrors ? errors.buy[index] : null} inputProps={{ value, inputMode: 'decimal', autoComplete: 'off', onChange: event => setAt('buyAmounts', index, event.target.value) }} />
            ))}
          </div>
        </fieldset>
        <fieldset className="m-0 flex flex-col gap-2 border-0 p-0">
          <legend className="mb-2 text-xs font-bold text-text-muted">Sell amounts (% of your coins)</legend>
          <div className="grid grid-cols-2 gap-2">
            {draft.sellPercents.map((value, index) => (
              <FormField key={index} id={`preset-sell-${index}`} label={`Sell ${index + 1}`} surface="surface-2" error={showErrors ? errors.sell[index] : null} inputProps={{ value, inputMode: 'numeric', autoComplete: 'off', onChange: event => setAt('sellPercents', index, event.target.value) }} />
            ))}
          </div>
        </fieldset>
        <FormField id="preset-slippage" label="Slippage" helper="100 = 1%. Above 500 you'll be asked to confirm each trade." surface="surface-2" error={showErrors ? errors.slippage : null} inputProps={{ value: draft.slippageBps, inputMode: 'numeric', autoComplete: 'off', onChange: event => setDraft(previous => ({ ...previous, slippageBps: event.target.value })) }} />
      </div>
    </ModalFrame>
  );
}
