'use client';

import { useId, useRef, useState, type KeyboardEvent } from 'react';
import { Crown, Droplets, Flame, Users, type LucideIcon } from 'lucide-react';
import { cn } from '@/lib/utils';
import { Button } from '@/ui/primitives';
import type { FeeStrategy } from './use-create-prefill';

export interface FeeStrategyMeta {
  value: FeeStrategy;
  label: string;
  summary: string;
  icon: LucideIcon;
  primary: boolean;
}

export const feeStrategies: readonly FeeStrategyMeta[] = [
  { value: 0, label: 'Creator', summary: 'You claim the trading fees from your coin.', icon: Crown, primary: true },
  { value: 2, label: 'Holders', summary: 'Trading fees are shared with everyone who holds the coin.', icon: Users, primary: true },
  { value: 1, label: 'Burn', summary: 'Trading fees are burned.', icon: Flame, primary: false },
  { value: 3, label: 'LP rewards', summary: 'Trading fees go to liquidity providers.', icon: Droplets, primary: false },
];

export const STRATEGY_MUTABILITY_NOTE = 'You can change this later from the coin page.';

export function feeStrategyLabel(value: FeeStrategy): string {
  return feeStrategies.find(strategy => strategy.value === value)?.label ?? 'Unknown';
}

export interface StrategyPickerProps {
  value: FeeStrategy;
  onChange: (value: FeeStrategy) => void;
  disabled?: boolean;
  className?: string;
}

export function StrategyPicker({ value, onChange, disabled = false, className }: StrategyPickerProps) {
  const id = useId();
  const noteId = `${id}-note`;
  const [more, setMore] = useState(() => feeStrategies.some(strategy => strategy.value === value && !strategy.primary));
  const refs = useRef<Array<HTMLButtonElement | null>>([]);
  const visible = feeStrategies.filter(strategy => strategy.primary || more);

  function move(from: number, delta: number) {
    if (disabled || visible.length === 0) return;
    const next = (from + delta + visible.length) % visible.length;
    onChange(visible[next].value);
    refs.current[next]?.focus();
  }

  function onKeyDown(event: KeyboardEvent<HTMLButtonElement>, index: number) {
    if (event.key === 'ArrowRight' || event.key === 'ArrowDown') { event.preventDefault(); move(index, 1); }
    else if (event.key === 'ArrowLeft' || event.key === 'ArrowUp') { event.preventDefault(); move(index, -1); }
    else if (event.key === 'Home') { event.preventDefault(); move(-1, 1); }
    else if (event.key === 'End') { event.preventDefault(); move(visible.length, -1); }
  }

  const selected = feeStrategies.find(strategy => strategy.value === value);
  return (
    <div className={cn('flex flex-col gap-2', className)}>
      <div className="flex items-center justify-between gap-3">
        <p id={`${id}-label`} className="text-sm font-bold text-text">Send creator rewards to</p>
        <Button variant="ghost" size="sm" aria-expanded={more} onClick={() => setMore(open => !open)} disabled={disabled}>
          {more ? 'Fewer options' : 'More options'}
        </Button>
      </div>
      <div role="radiogroup" aria-labelledby={`${id}-label`} aria-describedby={noteId} className={cn('grid gap-1 rounded-md bg-surface-2 p-1', visible.length > 2 ? 'grid-cols-2 sm:grid-cols-4' : 'grid-cols-2')}>
        {visible.map((strategy, index) => {
          const checked = strategy.value === value;
          const Icon = strategy.icon;
          return (
            <button
              key={strategy.value}
              ref={el => { refs.current[index] = el; }}
              type="button"
              role="radio"
              aria-checked={checked}
              tabIndex={checked ? 0 : -1}
              disabled={disabled}
              title={strategy.summary}
              onClick={() => onChange(strategy.value)}
              onKeyDown={event => onKeyDown(event, index)}
              className={cn(
                'inline-flex h-control-height-md items-center justify-center gap-2 rounded-sm px-3 text-sm font-bold transition-colors duration-fast',
                checked ? 'bg-surface-3 text-text' : 'text-text-muted hover:bg-surface-hover hover:text-text',
                disabled && 'cursor-not-allowed text-text-disabled hover:bg-transparent hover:text-text-disabled',
              )}
            >
              <Icon aria-hidden="true" className={cn('size-icon-md', checked && !disabled ? 'text-accent' : undefined)} />
              {strategy.label}
            </button>
          );
        })}
      </div>
      <p id={noteId} className="text-xs text-text-faint">
        {selected ? <span className="text-text-muted">{selected.summary} </span> : null}
        {STRATEGY_MUTABILITY_NOTE}
      </p>
    </div>
  );
}
