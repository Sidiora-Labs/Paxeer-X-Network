'use client';

import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react';
import { Eye, EyeOff } from 'lucide-react';
import { cn } from '@/lib/utils';
import { IconButton } from '@/ui/primitives';

const STORAGE_KEY = 'sidiora_balances_hidden';
const MASK = '••••••';

interface BalanceVisibility {
  hidden: boolean;
  setHidden: (hidden: boolean) => void;
  toggle: () => void;
}

const BalanceVisibilityContext = createContext<BalanceVisibility | null>(null);

export function readStoredBalanceVisibility(): boolean {
  if (typeof window === 'undefined') return false;
  try {
    return window.localStorage.getItem(STORAGE_KEY) === '1';
  } catch {
    return false;
  }
}

function persistBalanceVisibility(hidden: boolean) {
  try {
    if (hidden) window.localStorage.setItem(STORAGE_KEY, '1');
    else window.localStorage.removeItem(STORAGE_KEY);
  } catch {
    /* device storage unavailable; the preference lives for this page only */
  }
}

export function BalanceVisibilityProvider({ children }: { children: ReactNode }) {
  const [hidden, setHiddenState] = useState(false);
  useEffect(() => { setHiddenState(readStoredBalanceVisibility()); }, []);
  const setHidden = useCallback((next: boolean) => {
    setHiddenState(next);
    persistBalanceVisibility(next);
  }, []);
  const toggle = useCallback(() => {
    setHiddenState(previous => {
      const next = !previous;
      persistBalanceVisibility(next);
      return next;
    });
  }, []);
  const value = useMemo(() => ({ hidden, setHidden, toggle }), [hidden, setHidden, toggle]);
  return <BalanceVisibilityContext.Provider value={value}>{children}</BalanceVisibilityContext.Provider>;
}

const unmasked: BalanceVisibility = { hidden: false, setHidden: () => undefined, toggle: () => undefined };

/** Outside a provider amounts are never masked and the toggle is inert. */
export function useBalanceVisibility(): BalanceVisibility {
  return useContext(BalanceVisibilityContext) ?? unmasked;
}

export interface MaskedAmountProps {
  children: ReactNode;
  className?: string;
}

/**
 * Wraps any amount. While balances are hidden the amount is replaced by dots of
 * the same width so the layout does not shift; the real value is removed from
 * the accessibility tree instead of being read aloud.
 */
export function MaskedAmount({ children, className }: MaskedAmountProps) {
  const { hidden } = useBalanceVisibility();
  if (!hidden) return <span className={className}>{children}</span>;
  return (
    <span className={cn('relative inline-block align-baseline', className)} data-masked="true">
      <span aria-hidden="true" className="invisible">{children}</span>
      <span aria-label="Hidden" className="absolute inset-0 flex items-center justify-end overflow-hidden tabular-nums text-text-muted">{MASK}</span>
    </span>
  );
}

export function BalanceVisibilityToggle({ size = 'sm', className }: { size?: 'sm' | 'md'; className?: string }) {
  const { hidden, toggle } = useBalanceVisibility();
  return (
    <IconButton
      label={hidden ? 'Show balances' : 'Hide balances'}
      icon={hidden ? <EyeOff /> : <Eye />}
      size={size}
      pressed={hidden}
      onClick={toggle}
      className={className}
    />
  );
}
