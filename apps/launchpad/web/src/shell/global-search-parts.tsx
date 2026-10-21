'use client';
import { useEffect, useState, type KeyboardEvent, type ReactNode, type Ref } from 'react';
import { ClipboardPaste, Search, User, Wallet, X } from 'lucide-react';
import { cn } from '@/lib/utils';
import { Avatar, IconButton, MoneyValue, UnavailableState } from '@/ui/primitives';
import { shortAddress } from '@/utils/format';
import type { HotCoin, RecentItem, SearchResult } from './use-global-search';
import type { UserSearch } from '@/core/app-foundation/search-client';

export interface SelectArgs { poolAddress: string | undefined; tokenAddress: string | undefined; name: string; symbol: string; logo: string | null; marketCap: string }

/** Platform key label for the shortcut chip; resolved after mount so the server render never guesses. */
export function useShortcutLabel(): string {
  const [label, setLabel] = useState('Ctrl K');
  useEffect(() => {
    const platform = (navigator as Navigator & { userAgentData?: { platform?: string } }).userAgentData?.platform ?? navigator.platform ?? '';
    if (/mac|iphone|ipad/i.test(platform)) setLabel('Cmd K');
  }, []);
  return label;
}

export function ShortcutChip({ label, className }: { label: string; className?: string }) {
  return <kbd className={cn('inline-flex h-5 shrink-0 items-center rounded-xs bg-surface-3 px-1.5 font-sans text-2xs font-bold text-text-faint', className)}>{label}</kbd>;
}

/** Top-bar trigger shaped like an input; the real combobox lives inside the palette dialog. */
export function SearchTrigger({ onOpen, shortcut, triggerRef, compact = false }: { onOpen: () => void; shortcut: string; triggerRef: Ref<HTMLButtonElement>; compact?: boolean }) {
  return (
    <button ref={triggerRef} type="button" onClick={onOpen} aria-haspopup="dialog" aria-label={`Search coins and users (${shortcut})`}
      className={cn('flex h-control-height-md w-full min-w-0 items-center gap-2 rounded-md bg-surface-2 px-3 text-left text-sm text-text-faint transition-colors duration-fast hover:bg-surface-hover', compact && 'w-control-height-md justify-center px-0')}>
      <Search aria-hidden="true" className="size-icon-md shrink-0" />
      {compact ? null : <>
        <span className="min-w-0 flex-1 truncate">Search coins and users</span>
        <ShortcutChip label={shortcut} className="hidden sm:inline-flex" />
      </>}
    </button>
  );
}

export function PaletteInput({ value, onChange, inputRef, onKeyDown, listId, activeId, onPaste, pasteState }: {
  value: string; onChange: (value: string) => void; inputRef: Ref<HTMLInputElement>;
  onKeyDown: (event: KeyboardEvent<HTMLInputElement>) => void; listId: string; activeId: string | undefined;
  onPaste: () => void; pasteState: 'idle' | 'denied' | 'empty';
}) {
  return (
    <div className="flex items-center gap-2 rounded-md bg-surface-3 px-3">
      <Search aria-hidden="true" className="size-icon-md shrink-0 text-text-faint" />
      <input ref={inputRef} role="combobox" aria-label="Search coins and users by name, symbol, wallet or address" aria-autocomplete="list"
        aria-expanded="true" aria-controls={listId} aria-activedescendant={activeId} data-autofocus
        value={value} onChange={event => onChange(event.target.value)} onKeyDown={onKeyDown} autoComplete="off" spellCheck={false} maxLength={80}
        placeholder="Search coins and users" className="h-control-height-lg min-w-0 flex-1 bg-transparent text-base text-text placeholder:text-text-faint focus:outline-none" />
      {value ? <IconButton label="Clear search" icon={<X />} size="sm" onClick={() => onChange('')} /> : null}
      <IconButton label={pasteState === 'denied' ? "Can't read your clipboard, paste with your keyboard" : pasteState === 'empty' ? 'Clipboard has no text' : 'Paste from clipboard'} icon={<ClipboardPaste />} size="sm" onClick={onPaste} />
    </div>
  );
}

export function ResultGroup({ label, children, trailing }: { label: string; children: ReactNode; trailing?: ReactNode }) {
  return (
    <section aria-label={label} className="py-1">
      <div className="flex items-center justify-between px-3 py-1.5">
        <span className="text-2xs font-bold uppercase tracking-[.04em] text-text-faint">{label}</span>
        {trailing}
      </div>
      {children}
    </section>
  );
}

function rowClass(active: boolean, disabled = false) {
  return cn('flex h-12 w-full items-center gap-3 rounded-sm px-3 text-left transition-colors duration-fast', active ? 'bg-surface-3' : 'hover:bg-surface-hover', disabled && 'cursor-not-allowed');
}

export function CoinRow({ id, active, name, symbol, logo, mcapRaw, address, kind, disabled, onSelect }: {
  id: string; active: boolean; name: string; symbol: string; logo: string | null; mcapRaw: string | null | undefined;
  address: string | undefined; kind: 'pool' | 'token'; disabled?: boolean; onSelect: () => void;
}) {
  const title = symbol || name || (address ? shortAddress(address) : 'Unnamed');
  return (
    <button id={id} type="button" role="option" aria-selected={active} tabIndex={-1} disabled={disabled} onClick={onSelect} className={rowClass(active, disabled)}>
      <Avatar src={logo} name={title} size="sm" shape="square" decorative />
      <span className="min-w-0 flex-1">
        <span className="flex items-baseline gap-1.5">
          <span className="truncate text-sm font-bold text-text">{name || title}</span>
          {symbol ? <span className="shrink-0 text-xs text-text-muted">${symbol}</span> : null}
        </span>
        <span className="block truncate text-2xs text-text-faint">Coin{address ? ` ${shortAddress(address, 6, 4)}` : ''}{disabled ? ' (not trading yet)' : ''}</span>
      </span>
      {mcapRaw !== undefined ? <MoneyValue raw={mcapRaw} compact size="sm" className={cn(disabled && 'text-text-disabled')} /> : null}
    </button>
  );
}

export function UserRow({ id, active, label, wallet, onSelect }: { id: string; active: boolean; label: string; wallet: string; onSelect: () => void }) {
  return (
    <button id={id} type="button" role="option" aria-selected={active} tabIndex={-1} onClick={onSelect} className={rowClass(active)}>
      <Avatar src={null} name={label || wallet} size="sm" decorative />
      <span className="min-w-0 flex-1">
        <span className="block truncate text-sm font-bold text-text">{label || shortAddress(wallet)}</span>
        <span className="block truncate text-2xs text-text-faint">{shortAddress(wallet, 6, 4)}</span>
      </span>
    </button>
  );
}

export function ActionRow({ id, active, icon, title, detail, onSelect }: { id: string; active: boolean; icon: 'wallet' | 'user'; title: string; detail: string; onSelect: () => void }) {
  const Icon = icon === 'wallet' ? Wallet : User;
  return (
    <button id={id} type="button" role="option" aria-selected={active} tabIndex={-1} onClick={onSelect} className={rowClass(active)}>
      <span className="flex size-avatar-sm items-center justify-center rounded-pill bg-surface-3 text-text-muted"><Icon aria-hidden="true" className="size-icon-sm" /></span>
      <span className="min-w-0 flex-1">
        <span className="block truncate text-sm font-bold text-text">{title}</span>
        <span className="block truncate text-2xs text-text-faint">{detail}</span>
      </span>
    </button>
  );
}

export function UsersGroupBody({ users, onRetry }: { users: UserSearch; onRetry: () => void }) {
  if (users.state === 'unsupported') {
    return <div className="px-3 pb-2"><UnavailableState variant="inline" title="User search" operation={users.operation} status={users.status} message={users.message} onRetry={onRetry} /></div>;
  }
  if (users.items.length === 0) return <p className="px-3 pb-2 text-xs text-text-faint">No users found</p>;
  return null;
}

/** Input problems the viewer can fix (partial address, too long); shown as a hint, never as a failure. */
export function isSearchHint(error: Error | null): boolean {
  return error instanceof TypeError && /^(Enter a complete|Search is limited)/.test(error.message);
}

export function PaletteStatus({ loading, error, coinCount, userCount, onRetry, query }: { loading: boolean; error: Error | null; coinCount: number; userCount: number; onRetry: () => void; query?: string }) {
  const hint = isSearchHint(error);
  const trimmed = query?.trim() ?? '';
  const text = loading
    ? 'Searching'
    : error
      ? hint ? error.message : "Search isn't working right now"
      : coinCount + userCount === 0 && trimmed
        ? `No coins or users match "${trimmed}"`
        : `${coinCount} ${coinCount === 1 ? 'coin' : 'coins'}, ${userCount} ${userCount === 1 ? 'user' : 'users'}`;
  return (
    <div role="status" aria-live="polite" className="flex items-center justify-between gap-2 px-3 py-1.5 text-xs text-text-faint">
      <span>{text}</span>
      {error && !hint ? <button type="button" onClick={onRetry} className="text-xs font-bold text-accent hover:underline">Try again</button> : null}
    </div>
  );
}

export function recentToSelect(item: RecentItem): SelectArgs {
  return { poolAddress: item.address, tokenAddress: undefined, name: item.name, symbol: item.symbol, logo: item.logo, marketCap: item.marketCap };
}
export function hotToSelect(coin: HotCoin): SelectArgs {
  return { poolAddress: coin.poolAddress, tokenAddress: coin.tokenAddress, name: coin.name || '', symbol: coin.symbol || '', logo: coin.logo || null, marketCap: coin.marketCap || '0' };
}
export function resultToSelect(row: SearchResult, marketCap: string | null | undefined): SelectArgs {
  return { poolAddress: row.pool_address, tokenAddress: row.token_address, name: row.name, symbol: row.symbol, logo: row.images.logo, marketCap: marketCap ?? '0' };
}
