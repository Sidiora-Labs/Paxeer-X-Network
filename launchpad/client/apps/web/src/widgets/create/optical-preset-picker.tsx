'use client';

import { useId, useState } from 'react';
import { Lock, ShieldCheck } from 'lucide-react';
import { cn } from '@/lib/utils';
import { DisclosureRow, ShortAddress } from '@/ui/primitives';
import { useReadOpticalRegistryGetOpticalMetadata } from '@/core/network/contracts';

export const STANDARD_OPTICAL = 'standard';

interface OpticalMetadata {
  name: string;
  description: string;
  riskLevel: number;
  auditor: string;
}

export function opticalMetadata(data: unknown): OpticalMetadata | null {
  if (!data || typeof data !== 'object') return null;
  const value = data as Record<string, unknown>;
  if (typeof value.name !== 'string' || typeof value.description !== 'string') return null;
  const riskLevel = typeof value.riskLevel === 'number' ? value.riskLevel : typeof value.riskLevel === 'bigint' ? Number(value.riskLevel) : Number.NaN;
  return { name: value.name, description: value.description, riskLevel, auditor: typeof value.auditor === 'string' ? value.auditor : '' };
}

/** `message` is for diagnostics only; raw RPC text is never rendered. */
export type OpticalRegistryState = { state: 'loading' } | { state: 'ready' } | { state: 'error'; message: string };

export interface OpticalPresetPickerProps {
  value: string;
  onChange: (value: string) => void;
  registered: readonly string[];
  registry: OpticalRegistryState;
  /** Real admission reason for a non-standard preset; null means selectable. */
  reasonFor: (presetId: string) => string | null;
  disabled?: boolean;
  className?: string;
}

const cardClass = (selected: boolean, blocked: boolean, nested = false) => cn(
  'flex w-full items-start gap-3 rounded-md p-3 text-left transition-colors duration-fast',
  selected ? 'bg-accent-soft' : nested ? 'bg-surface-3' : 'bg-surface-2',
  !selected && !blocked && 'hover:bg-surface-hover',
  blocked && 'cursor-not-allowed',
);

function RegisteredOpticalCard({ address, reason, selected, disabled, onSelect }: { address: string; reason: string | null; selected: boolean; disabled: boolean; onSelect: () => void }) {
  const { data, isPending, error } = useReadOpticalRegistryGetOpticalMetadata({ optical: address as `0x${string}` });
  const meta = opticalMetadata(data);
  const blocked = disabled || reason !== null;
  return (
    <button
      type="button"
      role="radio"
      aria-checked={selected}
      aria-disabled={blocked || undefined}
      tabIndex={-1}
      onClick={() => { if (!blocked) onSelect(); }}
      className={cardClass(selected, blocked, true)}
    >
      <span aria-hidden="true" className="flex size-8 shrink-0 items-center justify-center rounded-pill bg-surface-2 text-text-faint">
        <Lock className="size-icon-md" />
      </span>
      <span className="min-w-0 flex-1">
        <span className="block text-sm font-bold text-text-disabled">
          {meta ? meta.name : isPending ? 'Loading...' : 'Custom rules'}
        </span>
        {meta ? (
          <span className="block text-xs text-text-faint">
            {meta.description}
            {Number.isFinite(meta.riskLevel) ? ` Risk level ${meta.riskLevel}.` : ''}
            {meta.auditor ? ` Audited by ${meta.auditor}.` : ''}
          </span>
        ) : error ? (
          <span className="block text-xs text-text-faint">Details can&apos;t load right now</span>
        ) : null}
        <span className="mt-1 block"><ShortAddress address={address} copy={false} /></span>
        {reason ? <span role="note" className="mt-1 block text-xs text-text-faint">Coming soon</span> : null}
      </span>
    </button>
  );
}

export function OpticalPresetPicker({ value, onChange, registered, registry, reasonFor, disabled = false, className }: OpticalPresetPickerProps) {
  const id = useId();
  const standardSelected = value === STANDARD_OPTICAL;
  const [userOpen, setUserOpen] = useState(false);
  // A selected non-standard preset is never hidden behind a closed disclosure.
  const registeredOpen = userOpen || !standardSelected;
  const reasons = new Map(registered.map(address => [address, reasonFor(address)] as const));
  const lockedCount = [...reasons.values()].filter(reason => reason !== null).length;
  return (
    <div className={cn('flex flex-col gap-2', className)}>
      <p id={`${id}-label`} className="text-sm font-bold text-text">Trading rules</p>
      <div role="radiogroup" aria-labelledby={`${id}-label`} aria-describedby={`${id}-note`} className="flex flex-col gap-2">
        <button
          type="button"
          role="radio"
          aria-checked={standardSelected}
          tabIndex={0}
          disabled={disabled}
          onClick={() => onChange(STANDARD_OPTICAL)}
          className={cardClass(standardSelected, disabled)}
        >
          <span aria-hidden="true" className={cn('flex size-8 shrink-0 items-center justify-center rounded-pill', standardSelected ? 'bg-accent-soft-strong text-accent' : 'bg-surface-3 text-text-muted')}>
            <ShieldCheck className="size-icon-md" />
          </span>
          <span className="min-w-0">
            <span className={cn('block text-sm font-bold', disabled ? 'text-text-disabled' : 'text-text')}>Standard</span>
            <span className={cn('block text-xs', disabled ? 'text-text-disabled' : 'text-text-muted')}>No taxes, cooldowns, wallet limits or anti-snipe rules.</span>
          </span>
        </button>
        {registered.length > 0 ? (
          <DisclosureRow
            id={`${id}-registered`}
            label="Custom rules"
            icon={Lock}
            open={registeredOpen}
            onToggle={next => setUserOpen(next)}
            trailing={<span className="text-xs font-semibold text-text-faint">{lockedCount === registered.length ? `${registered.length}, coming soon` : `${registered.length}, ${lockedCount} coming soon`}</span>}
          >
            <div className="flex flex-col gap-2">
              {registered.map(address => (
                <RegisteredOpticalCard
                  key={address}
                  address={address}
                  reason={reasons.get(address) ?? null}
                  selected={value === address}
                  disabled={disabled}
                  onSelect={() => onChange(address)}
                />
              ))}
            </div>
          </DisclosureRow>
        ) : null}
      </div>
      <p id={`${id}-note`} role="status" className="text-xs text-text-faint">
        {registry.state === 'loading'
          ? 'Loading...'
          : registry.state === 'error'
            ? "Custom rules can't load right now, so only Standard is available. This can't be changed after the coin is created."
            : registered.length === 0
              ? "Only Standard is available for now. This can't be changed after the coin is created."
              : "Custom rules are coming soon. This can't be changed after the coin is created."}
      </p>
    </div>
  );
}
