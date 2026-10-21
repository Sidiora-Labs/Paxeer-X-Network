'use client';

import {
  type ButtonHTMLAttributes,
  type InputHTMLAttributes,
  type ReactNode,
  useEffect,
  useId,
  useRef,
  useState,
} from 'react';
import { createPortal } from 'react-dom';
import { AlertTriangle, ChevronDown, X } from 'lucide-react';
import { cn } from '@/lib/cn';

export function Button({
  variant = 'primary',
  className,
  type = 'button',
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: 'primary' | 'secondary' | 'danger' | 'quiet';
}) {
  const styles = {
    primary: 'bg-pax-accent text-[var(--color-action-on-primary)]',
    secondary: 'bg-[var(--color-surface-control)] text-pax-light',
    danger: 'bg-red-950 text-red-100',
    quiet: 'bg-transparent text-pax-light hover:bg-white/[0.06]',
  };
  return (
    <button
      type={type}
      className={cn(
        'inline-flex min-h-[var(--touch-target)] items-center justify-center gap-2 rounded-xl px-4 py-2 text-sm font-semibold transition-[background-color,transform,opacity] disabled:cursor-not-allowed disabled:opacity-40 active:scale-[0.98]',
        styles[variant],
        className,
      )}
      {...props}
    />
  );
}

export function TextField({
  label,
  error,
  hint,
  className,
  id: suppliedId,
  ...props
}: InputHTMLAttributes<HTMLInputElement> & {
  label: string;
  error?: string;
  hint?: string;
}) {
  const generatedId = useId();
  const id = suppliedId ?? generatedId;
  const descriptionId = `${id}-description`;
  return (
    <label htmlFor={id} className="block space-y-2">
      <span className="block text-sm font-semibold text-pax-light">{label}</span>
      <input
        id={id}
        aria-invalid={Boolean(error)}
        aria-describedby={error || hint ? descriptionId : undefined}
        className={cn(
          'min-h-[var(--touch-target)] w-full rounded-xl bg-[var(--color-surface-control)] px-3 py-2 text-base text-pax-light outline-none placeholder:text-pax-mid focus:bg-[var(--color-surface-overlay)]',
          className,
        )}
        {...props}
      />
      {(error || hint) && (
        <span
          id={descriptionId}
          className={cn(
            'block text-xs',
            error ? 'text-pax-error' : 'text-pax-muted',
          )}
        >
          {error ?? hint}
        </span>
      )}
    </label>
  );
}

export function AmountText({
  value,
  symbol,
  label,
  className,
}: {
  value: string;
  symbol?: string;
  label: string;
  className?: string;
}) {
  return (
    <span
      aria-label={label}
      className={cn('tabular-nums break-words font-semibold', className)}
      title={`${value}${symbol ? ` ${symbol}` : ''}`}
    >
      {value}
      {symbol && <span className="ml-1 text-pax-muted">{symbol}</span>}
    </span>
  );
}

export function SegmentedControl<T extends string>({
  label,
  value,
  options,
  onChange,
}: {
  label: string;
  value: T;
  options: readonly { value: T; label: string }[];
  onChange: (value: T) => void;
}) {
  return (
    <div
      role="radiogroup"
      aria-label={label}
      className="flex gap-1 rounded-xl bg-[var(--color-surface-raised)] p-1"
    >
      {options.map((option) => (
        <button
          key={option.value}
          type="button"
          role="radio"
          aria-checked={value === option.value}
          onClick={() => onChange(option.value)}
          className={cn(
            'min-h-[var(--touch-target)] flex-1 rounded-lg px-3 text-sm font-medium',
            value === option.value
              ? 'bg-[var(--color-surface-overlay)] text-pax-light'
              : 'text-pax-muted',
          )}
        >
          {option.label}
        </button>
      ))}
    </div>
  );
}

export function Switch({
  checked,
  onCheckedChange,
  label,
  description,
}: {
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
  label: string;
  description?: string;
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      onClick={() => onCheckedChange(!checked)}
      className="flex min-h-[var(--touch-target)] w-full items-center justify-between gap-4 text-left"
    >
      <span>
        <span className="block text-sm font-medium">{label}</span>
        {description && (
          <span className="mt-0.5 block text-xs text-pax-muted">
            {description}
          </span>
        )}
      </span>
      <span
        aria-hidden="true"
        className={cn(
          'flex h-7 w-12 shrink-0 items-center rounded-full p-1 transition-colors',
          checked ? 'bg-pax-accent' : 'bg-[var(--color-surface-control)]',
        )}
      >
        <span
          className={cn(
            'h-5 w-5 rounded-full bg-white transition-transform',
            checked && 'translate-x-5',
          )}
        />
      </span>
    </button>
  );
}

function focusable(container: HTMLElement): HTMLElement[] {
  return [...container.querySelectorAll<HTMLElement>(
    'button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), a[href], [tabindex]:not([tabindex="-1"])',
  )];
}

export function Dialog({
  open,
  onClose,
  title,
  description,
  children,
  variant = 'dialog',
}: {
  open: boolean;
  onClose: () => void;
  title: string;
  description?: string;
  children: ReactNode;
  variant?: 'dialog' | 'sheet';
}) {
  const titleId = useId();
  const descriptionId = useId();
  const panelRef = useRef<HTMLDivElement>(null);
  const [portal, setPortal] = useState<HTMLDivElement | null>(null);

  useEffect(() => {
    if (!open) return;
    const node = document.createElement('div');
    node.dataset.paxportModalRoot = 'true';
    document.body.appendChild(node);
    setPortal(node);
    const previous = document.activeElement as HTMLElement | null;
    const siblings = [...document.body.children].filter((child) => child !== node);
    siblings.forEach((child) => {
      (child as HTMLElement).inert = true;
    });
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose();
      if (event.key !== 'Tab' || !panelRef.current) return;
      const controls = focusable(panelRef.current);
      if (controls.length === 0) {
        event.preventDefault();
        panelRef.current.focus();
        return;
      }
      const first = controls[0];
      const last = controls[controls.length - 1];
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first.focus();
      }
    };
    document.addEventListener('keydown', onKeyDown);
    requestAnimationFrame(() => {
      const first = focusable(panelRef.current ?? node)[0];
      if (first) first.focus();
      else panelRef.current?.focus();
    });
    return () => {
      document.removeEventListener('keydown', onKeyDown);
      siblings.forEach((child) => {
        (child as HTMLElement).inert = false;
      });
      node.remove();
      previous?.focus();
      setPortal(null);
    };
  }, [open, onClose]);

  if (!open || !portal) return null;
  return createPortal(
    <div className="fixed inset-0 z-[100] flex items-end justify-center bg-black/70 p-0 sm:items-center sm:p-4">
      <div
        ref={panelRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        aria-describedby={description ? descriptionId : undefined}
        tabIndex={-1}
        className={cn(
          'max-h-[min(90dvh,48rem)] w-full overflow-y-auto bg-[var(--color-surface-overlay)] p-5 shadow-[var(--elevation-sheet)]',
          variant === 'sheet'
            ? 'rounded-t-3xl sm:max-w-lg sm:rounded-3xl'
            : 'max-w-lg rounded-t-3xl sm:rounded-3xl',
        )}
      >
        <div className="mb-4 flex items-start justify-between gap-4">
          <div>
            <h2 id={titleId} className="text-lg font-semibold">
              {title}
            </h2>
            {description && (
              <p id={descriptionId} className="mt-1 text-sm text-pax-muted">
                {description}
              </p>
            )}
          </div>
          <Button variant="quiet" aria-label="Close dialog" onClick={onClose}>
            <X aria-hidden="true" className="h-5 w-5" />
          </Button>
        </div>
        {children}
      </div>
    </div>,
    portal,
  );
}

export const Sheet = Dialog;

export function Menu({
  label,
  children,
}: {
  label: string;
  children: ReactNode;
}) {
  const [open, setOpen] = useState(false);
  return (
    <div className="relative">
      <Button
        variant="secondary"
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setOpen((current) => !current)}
      >
        {label}
        <ChevronDown aria-hidden="true" className="h-4 w-4" />
      </Button>
      {open && (
        <div
          role="menu"
          className="absolute right-0 z-50 mt-2 min-w-48 rounded-xl bg-[var(--color-surface-overlay)] p-1 shadow-[var(--elevation-menu)]"
          onKeyDown={(event) => {
            if (event.key === 'Escape') setOpen(false);
          }}
        >
          {children}
        </div>
      )}
    </div>
  );
}

export function ToastRegion({ children }: { children: ReactNode }) {
  return (
    <div
      aria-live="polite"
      aria-relevant="additions"
      className="pointer-events-none fixed inset-x-3 top-[calc(var(--safe-area-top)+0.75rem)] z-[110] mx-auto max-w-md space-y-2"
    >
      {children}
    </div>
  );
}

export function Skeleton({ className }: { className?: string }) {
  return (
    <span
      aria-hidden="true"
      className={cn('shimmer block rounded-lg', className)}
    />
  );
}

export function ErrorState({
  title,
  message,
  onRetry,
}: {
  title: string;
  message: string;
  onRetry?: () => void;
}) {
  return (
    <div role="alert" className="rounded-2xl bg-red-950 p-4 text-red-50">
      <AlertTriangle aria-hidden="true" className="mb-3 h-5 w-5" />
      <h2 className="font-semibold">{title}</h2>
      <p className="mt-1 text-sm text-red-100/80">{message}</p>
      {onRetry && (
        <Button variant="danger" className="mt-4" onClick={onRetry}>
          Retry
        </Button>
      )}
    </div>
  );
}

export function ApprovalSummary({
  rows,
}: {
  rows: readonly { label: string; value: ReactNode; emphasis?: boolean }[];
}) {
  return (
    <dl className="space-y-2 rounded-2xl bg-[var(--color-surface-raised)] p-4">
      {rows.map((row) => (
        <div
          key={row.label}
          className="flex items-start justify-between gap-4 rounded-xl bg-[var(--color-surface-card)] px-3 py-2.5"
        >
          <dt className="text-sm text-pax-muted">{row.label}</dt>
          <dd
            className={cn(
              'max-w-[65%] break-words text-right text-sm',
              row.emphasis && 'font-semibold text-pax-light',
            )}
          >
            {row.value}
          </dd>
        </div>
      ))}
    </dl>
  );
}
