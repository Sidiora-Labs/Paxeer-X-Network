'use client';

import {
  cloneElement,
  isValidElement,
  useCallback,
  useEffect,
  useId,
  useRef,
  useState,
  type HTMLAttributes,
  type KeyboardEvent,
  type ReactElement,
  type ReactNode,
} from 'react';
import { createPortal } from 'react-dom';
import Link from 'next/link';
import { Check, ChevronDown, ChevronRight, X } from 'lucide-react';
import { cn } from '@/lib/utils';
import { IconButton } from './button';
import { useDismiss, useMounted, usePopoverPosition } from './use-popover';

/* -------------------------------------------------------- DropdownMenu */

export interface MenuItem {
  id: string;
  label: ReactNode;
  onSelect?: () => void;
  href?: string;
  icon?: ReactNode;
  destructive?: boolean;
  disabled?: boolean;
  /** Defined => rendered as menuitemradio with a check when true. */
  selected?: boolean;
  /** Optional group label rendered above this item. */
  group?: string;
  /** Marks an item that opens a nested surface (chevron). */
  submenu?: boolean;
}

export interface DropdownMenuProps {
  items: MenuItem[];
  /** Accessible name of the trigger, e.g. "More actions for hdegroot". */
  label: string;
  /** Trigger body. Defaults to a caret-only trigger when omitted. */
  trigger?: ReactNode;
  /** Shows the trigger label visually (title caret style). */
  showLabel?: boolean;
  triggerClassName?: string;
  align?: 'start' | 'end';
  size?: 'sm' | 'md';
  menuLabel?: string;
  className?: string;
}

export function DropdownMenu({ items, label, trigger, showLabel = false, triggerClassName, align = 'start', size = 'md', menuLabel, className }: DropdownMenuProps) {
  const [open, setOpen] = useState(false);
  const [activeIndex, setActiveIndex] = useState(-1);
  const triggerRef = useRef<HTMLButtonElement | null>(null);
  const menuRef = useRef<HTMLDivElement | null>(null);
  const itemRefs = useRef<Array<HTMLElement | null>>([]);
  const mounted = useMounted();
  const menuId = useId();
  const position = usePopoverPosition(triggerRef, menuRef, open, align);
  const close = useCallback(() => {
    setOpen(false);
  }, []);
  useDismiss(open, close, [triggerRef, menuRef]);

  const enabledIndexes = items.map((item, i) => (item.disabled ? -1 : i)).filter(i => i >= 0);
  const firstEnabledRef = useRef(-1);
  firstEnabledRef.current = enabledIndexes[0] ?? -1;

  useEffect(() => {
    if (!open) {
      setActiveIndex(-1);
      return;
    }
    const first = firstEnabledRef.current;
    setActiveIndex(first);
    const frame = requestAnimationFrame(() => itemRefs.current[first]?.focus());
    return () => cancelAnimationFrame(frame);
  }, [open]);

  function focusItem(index: number) {
    setActiveIndex(index);
    itemRefs.current[index]?.focus();
  }

  function closeAndRestore() {
    setOpen(false);
    triggerRef.current?.focus();
  }

  function select(item: MenuItem) {
    if (item.disabled) return;
    item.onSelect?.();
    closeAndRestore();
  }

  function onMenuKey(event: KeyboardEvent<HTMLDivElement>) {
    if (enabledIndexes.length === 0) return;
    const pos = enabledIndexes.indexOf(activeIndex);
    if (event.key === 'ArrowDown') { event.preventDefault(); focusItem(enabledIndexes[(pos + 1) % enabledIndexes.length]); }
    else if (event.key === 'ArrowUp') { event.preventDefault(); focusItem(enabledIndexes[(pos - 1 + enabledIndexes.length) % enabledIndexes.length]); }
    else if (event.key === 'Home') { event.preventDefault(); focusItem(enabledIndexes[0]); }
    else if (event.key === 'End') { event.preventDefault(); focusItem(enabledIndexes[enabledIndexes.length - 1]); }
    else if (event.key === 'Escape') { event.preventDefault(); closeAndRestore(); }
    else if (event.key === 'Tab') { setOpen(false); }
    else if (event.key.length === 1 && /\S/.test(event.key)) {
      const needle = event.key.toLowerCase();
      const text = (item: MenuItem) => (typeof item.label === 'string' ? item.label : '').toLowerCase();
      const after = enabledIndexes.find(i => i > activeIndex && text(items[i]).startsWith(needle));
      const wrap = after ?? enabledIndexes.find(i => text(items[i]).startsWith(needle));
      if (wrap !== undefined) focusItem(wrap);
    }
  }

  return (
    <div className={cn('inline-flex', className)}>
      <button
        ref={triggerRef}
        type="button"
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={open ? menuId : undefined}
        aria-label={showLabel ? undefined : label}
        title={showLabel ? undefined : label}
        onClick={() => setOpen(v => !v)}
        onKeyDown={event => {
          if (event.key === 'ArrowDown' || event.key === 'ArrowUp') { event.preventDefault(); setOpen(true); }
        }}
        className={cn(
          'inline-flex items-center justify-center gap-1.5 rounded-sm font-bold text-text-muted transition-colors duration-fast hover:bg-surface-hover hover:text-text',
          open && 'bg-surface-3 text-text',
          trigger ? (size === 'sm' ? 'h-control-height-sm px-2 text-sm' : 'h-control-height-md px-3 text-base') : (size === 'sm' ? 'size-control-height-sm' : 'size-control-height-md'),
          triggerClassName,
        )}
      >
        {showLabel ? <span>{label}</span> : null}
        {trigger ?? <ChevronDown aria-hidden="true" className={cn('size-icon-md transition-transform duration-fast', open && 'rotate-180')} />}
        {showLabel && trigger ? <ChevronDown aria-hidden="true" className={cn('size-icon-sm transition-transform duration-fast', open && 'rotate-180')} /> : null}
      </button>
      {mounted && open && position
        ? createPortal(
          <div
            ref={menuRef}
            id={menuId}
            role="menu"
            aria-label={menuLabel ?? label}
            onKeyDown={onMenuKey}
            style={{ top: position.top, left: position.left }}
            className="fixed z-[var(--sl-z-dropdown)] min-w-[200px] rounded-md bg-surface-3 p-1 shadow-pop"
          >
            {items.map((item, index) => {
              const role = item.selected !== undefined ? 'menuitemradio' : 'menuitem';
              const common: HTMLAttributes<HTMLElement> & { ref: (el: HTMLElement | null) => void; tabIndex: number } = {
                ref: el => { itemRefs.current[index] = el; },
                role,
                tabIndex: index === activeIndex ? 0 : -1,
                'aria-disabled': item.disabled || undefined,
                'aria-checked': item.selected !== undefined ? item.selected : undefined,
                onMouseEnter: () => !item.disabled && setActiveIndex(index),
                className: cn(
                  'flex h-10 w-full items-center gap-2 rounded-sm px-3 text-left text-base transition-colors duration-fast outline-none',
                  index === activeIndex && !item.disabled && 'bg-surface-hover',
                  item.selected && 'bg-surface-active',
                  item.destructive ? 'text-danger' : 'text-text',
                  item.disabled && 'cursor-not-allowed text-text-disabled',
                ),
              };
              const body = (
                <>
                  {item.icon ? <span aria-hidden="true" className="inline-flex shrink-0 [&>svg]:size-icon-md">{item.icon}</span> : null}
                  <span className="flex-1">{item.label}</span>
                  {item.selected ? <Check aria-hidden="true" className="size-icon-md text-accent" /> : null}
                  {item.submenu ? <ChevronRight aria-hidden="true" className="size-icon-md text-text-faint" /> : null}
                </>
              );
              return (
                <div key={item.id}>
                  {item.group && (index === 0 || items[index - 1].group !== item.group) ? (
                    <div role="presentation" className="px-3 pb-1 pt-2 text-2xs font-bold uppercase tracking-[.04em] text-text-faint">{item.group}</div>
                  ) : null}
                  {item.href && !item.disabled ? (
                    <Link href={item.href} {...common} onClick={() => closeAndRestore()}>{body}</Link>
                  ) : (
                    <button type="button" {...common} disabled={item.disabled} onClick={() => select(item)}>{body}</button>
                  )}
                </div>
              );
            })}
          </div>,
          document.body,
        )
        : null}
    </div>
  );
}

/* ---------------------------------------------------------- ModalFrame */

const FOCUSABLE = 'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

export interface ModalFrameProps {
  open: boolean;
  onClose: () => void;
  title: ReactNode;
  subtitle?: ReactNode;
  size?: 'sm' | 'md' | 'lg';
  /** Scrim click closes; confirm dialogs set false (Escape still closes). */
  closeOnScrim?: boolean;
  footer?: ReactNode;
  children: ReactNode;
  closeLabel?: string;
  /** Hides the visible title (it stays as the accessible name). */
  hideTitle?: boolean;
  className?: string;
}

const modalWidth = { sm: 'max-w-modal-width-sm', md: 'max-w-modal-width-md', lg: 'max-w-modal-width-lg' } as const;

export function ModalFrame({ open, onClose, title, subtitle, size = 'md', closeOnScrim = true, footer, children, closeLabel = 'Close dialog', hideTitle = false, className }: ModalFrameProps) {
  const mounted = useMounted();
  const panelRef = useRef<HTMLDivElement | null>(null);
  const openerRef = useRef<HTMLElement | null>(null);
  const titleId = useId();
  const subtitleId = useId();

  useEffect(() => {
    if (!open) return;
    openerRef.current = document.activeElement as HTMLElement | null;
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    const frame = requestAnimationFrame(() => {
      const panel = panelRef.current;
      if (!panel) return;
      const first = panel.querySelector<HTMLElement>('[data-autofocus]') ?? panel.querySelector<HTMLElement>(FOCUSABLE);
      (first ?? panel).focus();
    });
    return () => {
      cancelAnimationFrame(frame);
      document.body.style.overflow = previousOverflow;
      const opener = openerRef.current;
      if (opener && typeof opener.focus === 'function') opener.focus();
    };
  }, [open]);

  function onKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    if (event.key === 'Escape') {
      event.stopPropagation();
      onClose();
      return;
    }
    if (event.key !== 'Tab' || !panelRef.current) return;
    const focusable = Array.from(panelRef.current.querySelectorAll<HTMLElement>(FOCUSABLE)).filter(el => el.offsetParent !== null || el === document.activeElement);
    if (focusable.length === 0) {
      event.preventDefault();
      panelRef.current.focus();
      return;
    }
    const first = focusable[0];
    const last = focusable[focusable.length - 1];
    const active = document.activeElement;
    if (event.shiftKey && (active === first || active === panelRef.current)) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && active === last) {
      event.preventDefault();
      first.focus();
    }
  }

  if (!mounted || !open) return null;

  return createPortal(
    <div
      className="fixed inset-0 z-[var(--sl-z-modal)] flex items-end justify-center bg-[var(--sl-tint-scrim)] p-0 sm:items-center sm:p-6"
      onMouseDown={event => {
        if (closeOnScrim && event.target === event.currentTarget) onClose();
      }}
    >
      <div
        ref={panelRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        aria-describedby={subtitle ? subtitleId : undefined}
        tabIndex={-1}
        onKeyDown={onKeyDown}
        className={cn(
          'flex max-h-[calc(100dvh-24px)] w-full flex-col rounded-t-lg bg-surface-2 shadow-modal outline-none sm:max-h-[calc(100dvh-48px)] sm:rounded-lg',
          modalWidth[size],
          className,
        )}
      >
        <div className="flex items-start gap-3 px-6 pt-6">
          <div className="min-w-0 flex-1">
            <h2 id={titleId} className={cn('text-xl font-extrabold text-text', hideTitle && 'sr-only')}>{title}</h2>
            {subtitle ? <p id={subtitleId} className="mt-1 text-sm text-text-muted">{subtitle}</p> : null}
          </div>
          <IconButton label={closeLabel} icon={<X />} size="sm" onClick={onClose} className="-mr-2 -mt-1" />
        </div>
        <div className="min-h-0 flex-1 overflow-y-auto px-6 py-4">{children}</div>
        {footer ? <div className="flex flex-wrap items-center justify-end gap-2 px-6 pb-6">{footer}</div> : null}
      </div>
    </div>,
    document.body,
  );
}

/* ------------------------------------------------------------- Tooltip */

export interface TooltipProps {
  content: ReactNode;
  children: ReactElement<HTMLAttributes<HTMLElement>>;
  delay?: number;
  align?: 'start' | 'center' | 'end';
  className?: string;
}

export function Tooltip({ content, children, delay = 300, align = 'center', className }: TooltipProps) {
  const [visible, setVisible] = useState(false);
  const anchorRef = useRef<HTMLSpanElement | null>(null);
  const tipRef = useRef<HTMLDivElement | null>(null);
  const timer = useRef<number | null>(null);
  const mounted = useMounted();
  const id = useId();
  const position = usePopoverPosition(anchorRef, tipRef, visible, align, 6);

  const show = useCallback((immediate: boolean) => {
    if (timer.current) window.clearTimeout(timer.current);
    if (immediate) setVisible(true);
    else timer.current = window.setTimeout(() => setVisible(true), delay);
  }, [delay]);
  const hide = useCallback(() => {
    if (timer.current) window.clearTimeout(timer.current);
    timer.current = null;
    setVisible(false);
  }, []);

  useEffect(() => {
    if (!visible) return;
    function onKey(event: globalThis.KeyboardEvent) {
      if (event.key === 'Escape') hide();
    }
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [visible, hide]);

  useEffect(() => () => { if (timer.current) window.clearTimeout(timer.current); }, []);

  const child = isValidElement(children)
    ? cloneElement(children, { 'aria-describedby': visible ? id : children.props['aria-describedby'] })
    : children;

  return (
    <span
      ref={anchorRef}
      className={cn('inline-flex', className)}
      onMouseEnter={() => show(false)}
      onMouseLeave={hide}
      onFocus={() => show(true)}
      onBlur={hide}
    >
      {child}
      {mounted && visible && position
        ? createPortal(
          <div
            ref={tipRef}
            id={id}
            role="tooltip"
            style={{ top: position.top, left: position.left }}
            className="pointer-events-none fixed z-[var(--sl-z-tooltip)] max-w-60 rounded-sm bg-surface-3 px-2 py-1 text-xs text-text shadow-pop"
          >
            {content}
          </div>,
          document.body,
        )
        : null}
    </span>
  );
}
