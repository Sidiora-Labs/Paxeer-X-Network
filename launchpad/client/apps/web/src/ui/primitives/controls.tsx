'use client';

import {
  useCallback,
  useEffect,
  useId,
  useRef,
  useState,
  type ChangeEvent,
  type InputHTMLAttributes,
  type KeyboardEvent,
  type PointerEvent as ReactPointerEvent,
  type ReactNode,
  type TextareaHTMLAttributes,
} from 'react';
import { createPortal } from 'react-dom';
import { Check, ChevronDown, ChevronsUpDown, ChevronUp, CircleAlert, LoaderCircle, Lock } from 'lucide-react';
import { cn } from '@/lib/utils';
import { formatCount } from '@/utils/format';
import { useDismiss, useMounted, usePopoverPosition } from './use-popover';

/* ---------------------------------------------------- SegmentedControl */

export interface SegmentOption<T extends string> {
  value: T;
  label: ReactNode;
  count?: number;
  icon?: ReactNode;
  disabled?: boolean;
  /** Accessible name; set it when `icon` only or when `label` is a short form of a longer name. */
  ariaLabel?: string;
  /** Hover text; defaults to `ariaLabel` so a shortened label still shows its full name. */
  title?: string;
}

export interface SegmentedControlProps<T extends string> {
  options: SegmentOption<T>[];
  value: T;
  onChange: (value: T) => void;
  label: string;
  size?: 'sm' | 'md';
  shape?: 'pill' | 'sm';
  /** Trade tone colours the selected segment accent (first option) or danger (second). */
  tone?: 'neutral' | 'trade';
  fullWidth?: boolean;
  /** Tighter segment padding (6 px a side) for narrow containers (dashboard column headers). */
  compact?: boolean;
  className?: string;
}

export function SegmentedControl<T extends string>({ options, value, onChange, label, size = 'sm', shape = 'pill', tone = 'neutral', fullWidth = false, compact = false, className }: SegmentedControlProps<T>) {
  const refs = useRef<Array<HTMLButtonElement | null>>([]);

  function move(from: number, delta: number) {
    const enabled = options.map((o, i) => (o.disabled ? -1 : i)).filter(i => i >= 0);
    if (enabled.length === 0) return;
    const pos = enabled.indexOf(from);
    const next = enabled[(pos + delta + enabled.length) % enabled.length];
    onChange(options[next].value);
    refs.current[next]?.focus();
  }

  function onKeyDown(event: KeyboardEvent<HTMLButtonElement>, index: number) {
    if (event.key === 'ArrowRight' || event.key === 'ArrowDown') { event.preventDefault(); move(index, 1); }
    else if (event.key === 'ArrowLeft' || event.key === 'ArrowUp') { event.preventDefault(); move(index, -1); }
    else if (event.key === 'Home') { event.preventDefault(); move(-1, 1); }
    else if (event.key === 'End') { event.preventDefault(); move(options.length, -1); }
  }

  const radius = shape === 'pill' ? 'rounded-pill' : 'rounded-sm';
  return (
    <div role="radiogroup" aria-label={label} className={cn('inline-flex bg-surface-2 p-0.5', radius, fullWidth && 'flex w-full', className)}>
      {options.map((option, index) => {
        const selected = option.value === value;
        const tradeTone = tone === 'trade' ? (index === 0 ? 'bg-accent text-on-accent' : 'bg-danger text-on-danger') : 'bg-surface-active text-text';
        return (
          <button
            key={option.value}
            ref={el => { refs.current[index] = el; }}
            type="button"
            role="radio"
            aria-checked={selected}
            aria-label={option.ariaLabel}
            title={option.title ?? option.ariaLabel}
            tabIndex={selected ? 0 : -1}
            disabled={option.disabled}
            onClick={() => onChange(option.value)}
            onKeyDown={event => onKeyDown(event, index)}
            className={cn(
              'inline-flex items-center justify-center gap-1.5 whitespace-nowrap text-sm font-bold transition-colors duration-fast',
              compact ? 'px-1.5' : 'px-3',
              radius,
              size === 'sm' ? 'h-[24px]' : 'h-[32px]',
              fullWidth && 'flex-1',
              selected ? tradeTone : 'text-text-muted hover:bg-surface-hover hover:text-text',
              option.disabled && 'cursor-not-allowed text-text-disabled hover:bg-transparent',
            )}
          >
            {option.icon ? <span aria-hidden="true" className="inline-flex [&>svg]:size-icon-sm">{option.icon}</span> : null}
            {option.label}
            {option.count !== undefined ? <span className={cn('text-xs tabular-nums', selected ? 'opacity-80' : 'text-text-faint')} data-numeric>{formatCount(option.count)}</span> : null}
          </button>
        );
      })}
    </div>
  );
}

/* ------------------------------------------------------------ TabStrip */

export interface TabItem<T extends string> {
  id: T;
  label: ReactNode;
  count?: number;
  disabled?: boolean;
  /** Auth gated: shows a lock, the caller opens sign-in in onChange. */
  locked?: boolean;
}

export interface TabStripProps<T extends string> {
  tabs: TabItem<T>[];
  active: T;
  onChange: (id: T) => void;
  label: string;
  variant?: 'underline' | 'filled';
  /** Manual activation: arrow keys move focus, Enter/Space activate. Default automatic. */
  manual?: boolean;
  trailing?: ReactNode;
  /** Shared id prefix so tabpanels can point at tabs: `${idPrefix}-tab-${id}` / `${idPrefix}-panel-${id}`. */
  idPrefix?: string;
  className?: string;
}

export function tabIds(idPrefix: string, id: string) {
  return { tab: `${idPrefix}-tab-${id}`, panel: `${idPrefix}-panel-${id}` };
}

export function TabStrip<T extends string>({ tabs, active, onChange, label, variant = 'underline', manual = false, trailing, idPrefix, className }: TabStripProps<T>) {
  const generated = useId();
  const prefix = idPrefix ?? `tabs${generated}`;
  const refs = useRef<Array<HTMLButtonElement | null>>([]);

  function focusIndex(index: number, activate: boolean) {
    const enabled = tabs.map((t, i) => (t.disabled ? -1 : i)).filter(i => i >= 0);
    if (enabled.length === 0) return;
    const target = enabled.includes(index) ? index : enabled[0];
    refs.current[target]?.focus();
    if (activate) onChange(tabs[target].id);
  }

  function onKeyDown(event: KeyboardEvent<HTMLButtonElement>, index: number) {
    const enabled = tabs.map((t, i) => (t.disabled ? -1 : i)).filter(i => i >= 0);
    const pos = enabled.indexOf(index);
    let next: number | null = null;
    if (event.key === 'ArrowRight') next = enabled[(pos + 1) % enabled.length];
    else if (event.key === 'ArrowLeft') next = enabled[(pos - 1 + enabled.length) % enabled.length];
    else if (event.key === 'Home') next = enabled[0];
    else if (event.key === 'End') next = enabled[enabled.length - 1];
    else if (manual && (event.key === 'Enter' || event.key === ' ')) {
      event.preventDefault();
      onChange(tabs[index].id);
      return;
    }
    if (next !== null) {
      event.preventDefault();
      focusIndex(next, !manual);
    }
  }

  return (
    <div className={cn('flex items-center gap-2', className)}>
      <div role="tablist" aria-label={label} className={cn('flex min-w-0 flex-1 items-end overflow-x-auto scrollbar-none', variant === 'filled' && 'gap-1')}>
        {tabs.map((tab, index) => {
          const selected = tab.id === active;
          const ids = tabIds(prefix, tab.id);
          return (
            <button
              key={tab.id}
              ref={el => { refs.current[index] = el; }}
              id={ids.tab}
              type="button"
              role="tab"
              aria-selected={selected}
              aria-controls={ids.panel}
              aria-disabled={tab.disabled || undefined}
              tabIndex={selected ? 0 : -1}
              disabled={tab.disabled}
              onClick={() => onChange(tab.id)}
              onKeyDown={event => onKeyDown(event, index)}
              className={cn(
                'relative inline-flex shrink-0 items-center gap-1.5 whitespace-nowrap px-3 font-bold transition-colors duration-fast',
                variant === 'underline' ? 'h-11 text-base' : 'h-control-height-md rounded-sm text-sm',
                selected
                  ? variant === 'underline' ? 'text-text' : 'bg-surface-3 text-text'
                  : 'text-text-muted hover:text-text',
                variant === 'filled' && !selected && 'hover:bg-surface-hover',
                tab.disabled && 'cursor-not-allowed text-text-disabled hover:text-text-disabled',
              )}
            >
              {tab.locked ? <Lock aria-hidden="true" className="size-icon-sm" /> : null}
              <span>{tab.label}</span>
              {tab.count !== undefined ? (
                <span data-numeric className="inline-flex h-5 items-center rounded-pill bg-surface-3 px-1.5 text-xs tabular-nums text-text-muted">{formatCount(tab.count)}</span>
              ) : null}
              {tab.locked ? <span className="sr-only">, sign in required</span> : null}
              {variant === 'underline' && selected ? <span aria-hidden="true" className="absolute inset-x-3 bottom-0 h-0.5 rounded-pill bg-accent" /> : null}
            </button>
          );
        })}
      </div>
      {trailing ? <div className="shrink-0">{trailing}</div> : null}
    </div>
  );
}

export function UnderlineTabs<T extends string>(props: Omit<TabStripProps<T>, 'variant'>) {
  return <TabStrip {...props} variant="underline" />;
}

/* ---------------------------------------------------------- SelectPill */

export interface SelectOption<T extends string> {
  value: T;
  label: ReactNode;
  /** Plain text used when the option is selected and shown in the trigger. */
  text?: string;
  disabled?: boolean;
  icon?: ReactNode;
}

export interface SelectPillProps<T extends string> {
  options: SelectOption<T>[];
  value: T | null;
  onChange: (value: T) => void;
  /** Accessible name of the control, also shown as the "label:" prefix when `showLabel`. */
  label: string;
  showLabel?: boolean;
  placeholder?: string;
  size?: 'sm' | 'md';
  disabled?: boolean;
  loading?: boolean;
  error?: string | null;
  /**
   * Value-only trigger for narrow rows (dashboard column headers): tighter padding and no chevron,
   * so the pill is just the selected value. The menu, keyboard handling and accessible name are unchanged,
   * and the title spells out `label: value`.
   */
  compact?: boolean;
  className?: string;
}

export function SelectPill<T extends string>({ options, value, onChange, label, showLabel = false, placeholder = 'Select', size = 'sm', disabled = false, loading = false, error = null, compact = false, className }: SelectPillProps<T>) {
  const [open, setOpen] = useState(false);
  const [activeIndex, setActiveIndex] = useState(0);
  const triggerRef = useRef<HTMLButtonElement | null>(null);
  const listRef = useRef<HTMLUListElement | null>(null);
  const mounted = useMounted();
  const listId = useId();
  const position = usePopoverPosition(triggerRef, listRef, open);
  const close = useCallback(() => setOpen(false), []);
  useDismiss(open, close, [triggerRef, listRef]);

  const selected = options.find(option => option.value === value) ?? null;

  useEffect(() => {
    if (!open) return;
    const index = Math.max(0, options.findIndex(option => option.value === value));
    setActiveIndex(index);
    const frame = requestAnimationFrame(() => listRef.current?.focus());
    return () => cancelAnimationFrame(frame);
  }, [open, options, value]);

  function choose(index: number) {
    const option = options[index];
    if (!option || option.disabled) return;
    onChange(option.value);
    setOpen(false);
    triggerRef.current?.focus();
  }

  function onListKey(event: KeyboardEvent<HTMLUListElement>) {
    const enabled = options.map((o, i) => (o.disabled ? -1 : i)).filter(i => i >= 0);
    const pos = enabled.indexOf(activeIndex);
    if (event.key === 'ArrowDown') { event.preventDefault(); setActiveIndex(enabled[(pos + 1) % enabled.length] ?? 0); }
    else if (event.key === 'ArrowUp') { event.preventDefault(); setActiveIndex(enabled[(pos - 1 + enabled.length) % enabled.length] ?? 0); }
    else if (event.key === 'Home') { event.preventDefault(); setActiveIndex(enabled[0] ?? 0); }
    else if (event.key === 'End') { event.preventDefault(); setActiveIndex(enabled[enabled.length - 1] ?? 0); }
    else if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); choose(activeIndex); }
    else if (event.key === 'Tab') { setOpen(false); }
    else if (event.key.length === 1 && /\S/.test(event.key)) {
      const needle = event.key.toLowerCase();
      const found = options.findIndex((option, i) => !option.disabled && i > activeIndex && (option.text ?? String(option.label)).toLowerCase().startsWith(needle));
      const wrap = found === -1 ? options.findIndex(option => !option.disabled && (option.text ?? String(option.label)).toLowerCase().startsWith(needle)) : found;
      if (wrap >= 0) setActiveIndex(wrap);
    }
  }

  return (
    <div className={cn('inline-flex flex-col gap-1', className)}>
      <button
        ref={triggerRef}
        type="button"
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-controls={open ? listId : undefined}
        aria-label={showLabel ? undefined : label}
        aria-invalid={error ? true : undefined}
        title={compact ? `${label}: ${selected?.text ?? (typeof selected?.label === 'string' ? selected.label : placeholder)}` : undefined}
        disabled={disabled}
        onClick={() => setOpen(v => !v)}
        onKeyDown={event => {
          if (event.key === 'ArrowDown' || event.key === 'ArrowUp') { event.preventDefault(); setOpen(true); }
        }}
        className={cn(
          'inline-flex items-center gap-1.5 whitespace-nowrap rounded-pill text-sm font-bold transition-colors duration-fast',
          compact ? 'px-2' : 'px-3',
          size === 'sm' ? 'h-control-height-sm' : 'h-control-height-md',
          error ? 'bg-danger-soft text-text' : open ? 'bg-surface-3 text-text' : 'bg-surface-2 text-text hover:bg-surface-hover',
          disabled && 'cursor-not-allowed text-text-disabled hover:bg-surface-2',
        )}
      >
        {showLabel ? <span className="font-semibold text-text-faint">{label}:</span> : null}
        {selected?.icon ? <span aria-hidden="true" className="inline-flex [&>svg]:size-icon-sm">{selected.icon}</span> : null}
        <span className={cn(!selected && 'text-text-faint')}>{selected ? selected.text ?? selected.label : placeholder}</span>
        {loading
          ? <LoaderCircle aria-hidden="true" className="size-icon-sm animate-spin text-text-faint" />
          : compact ? null : <ChevronDown aria-hidden="true" className={cn('size-icon-sm text-text-faint transition-transform duration-fast', open && 'rotate-180')} />}
      </button>
      {error ? (
        <span role="alert" className="inline-flex items-center gap-1 text-xs text-danger">
          <CircleAlert aria-hidden="true" className="size-icon-sm" />
          {error}
        </span>
      ) : null}
      {mounted && open && position
        ? createPortal(
          <ul
            ref={listRef}
            id={listId}
            role="listbox"
            tabIndex={-1}
            aria-label={label}
            aria-activedescendant={`${listId}-opt-${activeIndex}`}
            onKeyDown={onListKey}
            style={{ top: position.top, left: position.left, minWidth: Math.max(position.minWidth, 160) }}
            className="fixed z-[var(--sl-z-dropdown)] max-h-80 overflow-y-auto rounded-md bg-surface-3 p-1 shadow-pop outline-none"
          >
            {options.length === 0 ? <li className="px-3 py-2 text-sm text-text-faint">No matches</li> : null}
            {options.map((option, index) => {
              const isSelected = option.value === value;
              const isActive = index === activeIndex;
              return (
                <li
                  key={option.value}
                  id={`${listId}-opt-${index}`}
                  role="option"
                  aria-selected={isSelected}
                  aria-disabled={option.disabled || undefined}
                  onMouseEnter={() => !option.disabled && setActiveIndex(index)}
                  onClick={() => choose(index)}
                  className={cn(
                    'flex h-10 cursor-pointer items-center gap-2 rounded-sm px-3 text-sm',
                    isActive && 'bg-surface-hover',
                    isSelected && 'bg-surface-active text-text',
                    option.disabled && 'cursor-not-allowed text-text-disabled',
                  )}
                >
                  {option.icon ? <span aria-hidden="true" className="inline-flex [&>svg]:size-icon-sm">{option.icon}</span> : null}
                  <span className="flex-1">{option.label}</span>
                  {isSelected ? <Check aria-hidden="true" className="size-icon-sm text-accent" /> : null}
                </li>
              );
            })}
          </ul>,
          document.body,
        )
        : null}
    </div>
  );
}

/* -------------------------------------------------------------- Toggle */

export interface ToggleProps {
  checked: boolean;
  onChange: (checked: boolean) => void;
  /** Accessible name (or use `labelledBy`). */
  label?: string;
  labelledBy?: string;
  describedBy?: string;
  size?: 'sm' | 'md';
  disabled?: boolean;
  loading?: boolean;
  className?: string;
}

export function Toggle({ checked, onChange, label, labelledBy, describedBy, size = 'md', disabled = false, loading = false, className }: ToggleProps) {
  const track = size === 'md' ? 'h-6 w-10' : 'h-5 w-8';
  const thumb = size === 'md' ? 'size-5' : 'size-4';
  const shift = size === 'md' ? 'translate-x-4' : 'translate-x-3';
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      aria-labelledby={labelledBy}
      aria-describedby={describedBy}
      aria-busy={loading || undefined}
      disabled={disabled || loading}
      onClick={() => onChange(!checked)}
      className={cn(
        'relative inline-flex shrink-0 items-center rounded-pill p-0.5 transition-colors duration-fast',
        track,
        disabled ? 'bg-surface-3' : checked ? 'bg-accent' : 'bg-surface-active hover:bg-surface-hover',
        disabled && 'cursor-not-allowed',
        className,
      )}
    >
      <span
        aria-hidden="true"
        className={cn(
          'inline-flex items-center justify-center rounded-pill transition-transform duration-fast ease-sl',
          thumb,
          disabled ? 'bg-text-disabled' : checked ? 'bg-on-accent' : 'bg-text-muted',
          checked && shift,
        )}
      >
        {loading ? <LoaderCircle className="size-3 animate-spin text-text" /> : null}
      </span>
    </button>
  );
}

/* -------------------------------------------------------------- Slider */

export interface SliderProps {
  value: number;
  onChange: (value: number) => void;
  min?: number;
  max?: number;
  step?: number;
  label: string;
  /** Spoken unit for aria-valuetext, e.g. "%". */
  unit?: string;
  ticks?: number[];
  disabled?: boolean;
  outOfRange?: boolean;
  showValue?: boolean;
  className?: string;
}

export function Slider({ value, onChange, min = 0, max = 100, step = 1, label, unit = '', ticks, disabled = false, outOfRange = false, showValue = true, className }: SliderProps) {
  const pct = max === min ? 0 : Math.max(0, Math.min(100, ((value - min) / (max - min)) * 100));
  return (
    <div className={cn('flex items-center gap-3', className)}>
      <div className="relative flex h-4 flex-1 items-center">
        <span aria-hidden="true" className={cn('absolute inset-x-0 h-1 rounded-pill', disabled ? 'bg-surface-3' : 'bg-track')} />
        <span aria-hidden="true" style={{ width: `${pct}%` }} className={cn('absolute left-0 h-1 rounded-pill', disabled ? 'bg-surface-3' : outOfRange ? 'bg-warning' : 'bg-track-fill')} />
        <input
          type="range"
          min={min}
          max={max}
          step={step}
          value={value}
          disabled={disabled}
          aria-label={label}
          aria-valuetext={`${value}${unit}`}
          onChange={(event: ChangeEvent<HTMLInputElement>) => onChange(Number(event.target.value))}
          onKeyDown={event => {
            if (event.shiftKey && (event.key === 'ArrowRight' || event.key === 'ArrowUp')) { event.preventDefault(); onChange(Math.min(max, value + step * 10)); }
            if (event.shiftKey && (event.key === 'ArrowLeft' || event.key === 'ArrowDown')) { event.preventDefault(); onChange(Math.max(min, value - step * 10)); }
          }}
          className={cn(
            'relative h-4 w-full cursor-pointer appearance-none bg-transparent outline-none',
            '[&::-webkit-slider-runnable-track]:bg-transparent [&::-moz-range-track]:bg-transparent',
            '[&::-webkit-slider-thumb]:size-4 [&::-webkit-slider-thumb]:appearance-none [&::-webkit-slider-thumb]:rounded-pill [&::-webkit-slider-thumb]:transition-transform',
            '[&::-moz-range-thumb]:size-4 [&::-moz-range-thumb]:rounded-pill [&::-moz-range-thumb]:border-0',
            'focus-visible:[&::-webkit-slider-thumb]:outline-2 focus-visible:[&::-webkit-slider-thumb]:outline-focus focus-visible:[&::-webkit-slider-thumb]:outline-offset-2',
            disabled
              ? 'cursor-not-allowed [&::-webkit-slider-thumb]:bg-text-disabled [&::-moz-range-thumb]:bg-text-disabled'
              : '[&::-webkit-slider-thumb]:bg-accent hover:[&::-webkit-slider-thumb]:scale-110 active:[&::-webkit-slider-thumb]:bg-accent-active [&::-moz-range-thumb]:bg-accent',
          )}
        />
        {ticks && ticks.length > 0 ? (
          <div aria-hidden="true" className="pointer-events-none absolute inset-x-0 top-full h-1.5">
            {ticks.map(tick => (
              <span key={tick} style={{ left: `${max === min ? 0 : ((tick - min) / (max - min)) * 100}%` }} className="absolute h-1.5 w-0.5 -translate-x-1/2 rounded-pill bg-surface-active" />
            ))}
          </div>
        ) : null}
      </div>
      {showValue ? <span data-numeric className={cn('w-10 text-right text-xs tabular-nums', outOfRange ? 'text-warning' : 'text-text-muted')}>{value}{unit}</span> : null}
    </div>
  );
}

/* ----------------------------------------------------------- FormField */

type InputProps = Omit<InputHTMLAttributes<HTMLInputElement>, 'id' | 'className' | 'aria-describedby' | 'aria-invalid'>;
type TextareaProps = Omit<TextareaHTMLAttributes<HTMLTextAreaElement>, 'id' | 'className' | 'aria-describedby' | 'aria-invalid'>;

export interface FormFieldProps {
  id: string;
  label: string;
  optional?: boolean;
  helper?: ReactNode;
  error?: string | null;
  multiline?: boolean;
  size?: 'md' | 'lg';
  /** Surface the field sits on; the control takes the next step. */
  surface?: 'canvas' | 'surface-1' | 'surface-2';
  leading?: ReactNode;
  trailing?: ReactNode;
  counter?: { count: number; max: number };
  loading?: boolean;
  mono?: boolean;
  uppercase?: boolean;
  inputProps?: InputProps;
  textareaProps?: TextareaProps;
  className?: string;
}

const controlSurface = { canvas: 'bg-surface-1', 'surface-1': 'bg-surface-2', 'surface-2': 'bg-surface-3' } as const;

export function FormField({ id, label, optional = false, helper, error = null, multiline = false, size = 'md', surface = 'surface-1', leading, trailing, counter, loading = false, mono = false, uppercase = false, inputProps, textareaProps, className }: FormFieldProps) {
  const helperId = `${id}-helper`;
  const errorId = `${id}-error`;
  const counterId = `${id}-counter`;
  const describedBy = [error ? errorId : helper ? helperId : null, counter ? counterId : null].filter(Boolean).join(' ') || undefined;
  const disabled = Boolean(inputProps?.disabled ?? textareaProps?.disabled);
  const readOnly = Boolean(inputProps?.readOnly ?? textareaProps?.readOnly);
  const counterTone = counter
    ? counter.count >= counter.max ? 'text-danger' : counter.count >= counter.max * 0.9 ? 'text-warning' : 'text-text-faint'
    : '';
  const shell = cn(
    'flex items-center gap-2 rounded-sm px-3 transition-colors duration-fast',
    error ? 'bg-danger-soft' : readOnly ? 'bg-transparent' : disabled ? 'bg-surface-2' : controlSurface[surface],
    'focus-within:outline-2 focus-within:outline-focus',
    multiline ? 'items-start py-2' : size === 'lg' ? 'h-control-height-lg' : 'h-control-height-md',
  );
  const control = cn(
    'min-w-0 flex-1 bg-transparent text-base text-text outline-none placeholder:text-text-faint',
    'disabled:text-text-disabled read-only:text-text-muted',
    mono && 'font-[family-name:var(--sl-font-mono)]',
    uppercase && 'uppercase',
    multiline && 'min-h-24 resize-y leading-5',
  );

  return (
    <div className={cn('flex flex-col gap-1.5', className)}>
      <label htmlFor={id} className="text-sm font-bold text-text">
        {label}
        {optional ? <span className="ml-1 font-semibold text-text-faint">(Optional)</span> : null}
      </label>
      <div className={shell}>
        {leading ? <span aria-hidden="true" className="inline-flex shrink-0 text-text-faint [&>svg]:size-icon-md">{leading}</span> : null}
        {multiline ? (
          <textarea id={id} aria-describedby={describedBy} aria-invalid={error ? true : undefined} className={control} {...textareaProps} />
        ) : (
          <input id={id} aria-describedby={describedBy} aria-invalid={error ? true : undefined} className={control} {...inputProps} />
        )}
        {loading ? <LoaderCircle aria-hidden="true" className="size-icon-md shrink-0 animate-spin text-text-faint" /> : null}
        {trailing ? <span className="inline-flex shrink-0 items-center">{trailing}</span> : null}
      </div>
      <div className="flex items-start justify-between gap-3">
        {error ? (
          <p id={errorId} role="alert" className="inline-flex items-center gap-1 text-xs text-danger">
            <CircleAlert aria-hidden="true" className="size-icon-sm shrink-0" />
            {error}
          </p>
        ) : helper ? (
          <p id={helperId} className="text-xs text-text-faint">{helper}</p>
        ) : <span />}
        {counter ? (
          <span id={counterId} data-numeric aria-live={counter.count >= counter.max * 0.9 ? 'polite' : 'off'} className={cn('shrink-0 text-xs tabular-nums', counterTone)}>
            {counter.count}/{counter.max}
          </span>
        ) : null}
      </div>
    </div>
  );
}

/* ---------------------------------------------------------- SortHeader */

export type SortDirection = 'asc' | 'desc' | null;

export interface SortHeaderProps {
  label: ReactNode;
  /** Current direction for this column; null when another column (or none) is active. */
  sort?: SortDirection;
  /** Omit to render a static header. */
  onSort?: () => void;
  align?: 'left' | 'right' | 'center';
  width?: number | string;
  className?: string;
}

export function SortHeader({ label, sort = null, onSort, align = 'left', width, className }: SortHeaderProps) {
  const alignClass = align === 'right' ? 'justify-end text-right' : align === 'center' ? 'justify-center text-center' : 'justify-start text-left';
  const ariaSort = sort === 'asc' ? 'ascending' : sort === 'desc' ? 'descending' : onSort ? 'none' : undefined;
  const content = (
    <span className={cn('inline-flex items-center gap-1', alignClass)}>
      <span>{label}</span>
      {onSort ? (
        sort === 'asc' ? <ChevronUp aria-hidden="true" className="size-3 text-accent" />
          : sort === 'desc' ? <ChevronDown aria-hidden="true" className="size-3 text-accent" />
            : <ChevronsUpDown aria-hidden="true" className="size-3 opacity-60" />
      ) : null}
    </span>
  );
  return (
    <th
      scope="col"
      aria-sort={ariaSort}
      style={width !== undefined ? { width } : undefined}
      className={cn('h-table-header-height px-3 text-2xs font-bold uppercase tracking-[.04em]', sort ? 'text-text' : 'text-text-faint', className)}
    >
      {onSort ? (
        <button type="button" onClick={onSort} className={cn('flex h-full w-full items-center rounded-xs uppercase tracking-[.04em] transition-colors hover:text-text', alignClass)}>
          {content}
          <span className="sr-only">{sort === 'asc' ? ', sorted ascending' : sort === 'desc' ? ', sorted descending' : ', sortable'}</span>
        </button>
      ) : (
        <div className={cn('flex h-full items-center', alignClass)}>{content}</div>
      )}
    </th>
  );
}

/* -------------------------------------------------------- ResizeHandle */

export interface ResizeHandleProps {
  value: number;
  min: number;
  max: number;
  onChange: (value: number) => void;
  /** Double-click / Enter resets to this value. */
  defaultValue?: number;
  step?: number;
  label?: string;
  className?: string;
  /** 'horizontal' (default) is a row divider dragged up/down; 'vertical' is a column divider dragged left/right. */
  orientation?: 'horizontal' | 'vertical';
  /** The resized element sits after the handle (below / right of it): dragging toward it, or ArrowUp / ArrowLeft, grows the value. */
  inverted?: boolean;
}

export function ResizeHandle({ value, min, max, onChange, defaultValue, step = 16, label = 'Resize chart', className, orientation = 'horizontal', inverted = false }: ResizeHandleProps) {
  const [dragging, setDragging] = useState(false);
  const start = useRef<{ at: number; value: number } | null>(null);
  const clamp = useCallback((v: number) => Math.max(min, Math.min(max, Math.round(v))), [min, max]);
  const vertical = orientation === 'vertical';
  const direction = inverted ? -1 : 1;
  const coordinate = (event: ReactPointerEvent<HTMLDivElement>) => (vertical ? event.clientX : event.clientY);

  function onPointerDown(event: ReactPointerEvent<HTMLDivElement>) {
    start.current = { at: coordinate(event), value };
    setDragging(true);
    event.currentTarget.setPointerCapture(event.pointerId);
  }
  function onPointerMove(event: ReactPointerEvent<HTMLDivElement>) {
    if (!start.current) return;
    onChange(clamp(start.current.value + direction * (coordinate(event) - start.current.at)));
  }
  function onPointerUp(event: ReactPointerEvent<HTMLDivElement>) {
    start.current = null;
    setDragging(false);
    if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId);
  }
  function onKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    if (event.key === 'ArrowUp' || event.key === 'ArrowLeft') { event.preventDefault(); onChange(clamp(value - direction * step)); }
    else if (event.key === 'ArrowDown' || event.key === 'ArrowRight') { event.preventDefault(); onChange(clamp(value + direction * step)); }
    else if (event.key === 'Home') { event.preventDefault(); onChange(min); }
    else if (event.key === 'End') { event.preventDefault(); onChange(max); }
    else if (event.key === 'Enter' && defaultValue !== undefined) { event.preventDefault(); onChange(clamp(defaultValue)); }
  }

  return (
    <div
      role="separator"
      aria-orientation={orientation}
      aria-label={label}
      aria-valuenow={value}
      aria-valuemin={min}
      aria-valuemax={max}
      tabIndex={0}
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={onPointerUp}
      onPointerCancel={onPointerUp}
      onDoubleClick={() => defaultValue !== undefined && onChange(clamp(defaultValue))}
      onKeyDown={onKeyDown}
      data-dragging={dragging ? '' : undefined}
      className={cn(
        'group flex touch-none select-none items-center justify-center',
        vertical ? 'h-full w-2 cursor-col-resize' : 'h-6 w-full cursor-row-resize',
        className,
      )}
    >
      <span aria-hidden="true" className={cn('rounded-pill transition-colors duration-fast', vertical ? 'h-8 w-1' : 'h-1 w-8', dragging ? 'bg-accent' : 'bg-surface-3 group-hover:bg-surface-hover')} />
    </div>
  );
}
