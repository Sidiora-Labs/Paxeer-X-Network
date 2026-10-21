'use client';
import { useCallback, useEffect, useId, useRef, useState, type ReactNode } from 'react';
import Link from 'next/link';
import type { LucideIcon } from 'lucide-react';
import { Bell, Flame, Globe, Layers, LineChart, Megaphone, Radar, Rss, Settings, Star, Wallet, X } from 'lucide-react';
import { cn } from '@/lib/utils';
import { DropdownMenu, IconButton } from '@/ui/primitives';
import { HotCalloutsPanel } from '@/widgets/social/hot-callouts-panel';
import { AlertsPanel } from './alerts-panel';
import { BalancesCluster, CommunityLinks } from './balances';
import { terminalViewPath } from '../sub-nav';
import { ConnectionPill } from './connection';
import { isTerminalPanel, terminalOpenPanelEvent, terminalResetColumnsEvent, type TerminalPanel } from './model';
import { FeedPanel, MissingOperationPanel, PnlPanel, TrendingPanel, WatchlistPanel } from './panels';

type Tool = { kind: 'panel'; id: TerminalPanel; label: string; icon: LucideIcon } | { kind: 'link'; href: string; label: string; icon: LucideIcon };

const tools: readonly Tool[] = [
  { kind: 'panel', id: 'watchlist', label: 'Watchlist', icon: Star },
  { kind: 'panel', id: 'wallets', label: 'Wallets tracker', icon: Wallet },
  { kind: 'panel', id: 'pnl', label: 'PnL tracker', icon: LineChart },
  { kind: 'panel', id: 'feed', label: 'Feed', icon: Rss },
  { kind: 'link', href: terminalViewPath('trenches'), label: 'Trenches', icon: Layers },
  { kind: 'panel', id: 'alpha', label: 'Alpha tracker', icon: Radar },
  { kind: 'panel', id: 'alerts', label: 'Price alerts', icon: Bell },
  { kind: 'panel', id: 'trending', label: 'Trending', icon: Flame },
  { kind: 'panel', id: 'callouts', label: 'Callouts', icon: Megaphone },
];

const panelTitles: Record<TerminalPanel, string> = {
  watchlist: 'Watchlist', wallets: 'Wallets tracker', pnl: 'PnL tracker', feed: 'Feed', alpha: 'Alpha tracker',
  alerts: 'Price alerts', trending: 'Trending', callouts: 'Callouts',
};

const PANEL_WIDTH = 360;
const toolClass = 'inline-flex h-6 shrink-0 items-center gap-1 rounded-sm px-1.5 text-xs font-semibold text-text-muted transition-colors duration-fast hover:bg-surface-hover hover:text-text [&_svg]:size-icon-sm';

function panelBody(id: TerminalPanel, pool: string | null): ReactNode {
  switch (id) {
    case 'watchlist': return <WatchlistPanel />;
    case 'pnl': return <PnlPanel />;
    case 'feed': return <FeedPanel />;
    case 'trending': return <TrendingPanel />;
    case 'alerts': return <AlertsPanel poolAddress={pool} />;
    case 'callouts': return <HotCalloutsPanel poolAddress={pool} initialFeed={pool ? 'coin' : 'hot'} />;
    case 'wallets': return <MissingOperationPanel title="Wallets tracker" operation="tracked-wallet activity (no contract operation)" message="Wallet tracking is coming soon." />;
    case 'alpha': return <MissingOperationPanel title="Alpha tracker" operation="smart-money signals (no contract operation)" message="Alpha tracking is coming soon." />;
  }
}

/**
 * Terminal status bar (28 tall, surface-2): connection pill, settings, tool
 * buttons that open one popover panel at a time above the bar, wallet
 * balances and community links. Other packages open a panel with
 * `window.dispatchEvent(new CustomEvent('sidiora:terminal-open-panel', { detail: 'alerts' }))`.
 */
export default function TerminalStatusBar({ poolAddress }: { poolAddress: string | null }) {
  const pool = poolAddress && /^0x[0-9a-fA-F]{40}$/.test(poolAddress) ? poolAddress.toLowerCase() : null;
  const [open, setOpen] = useState<TerminalPanel | null>(null);
  const [left, setLeft] = useState(4);
  const barRef = useRef<HTMLDivElement | null>(null);
  const panelRef = useRef<HTMLDivElement | null>(null);
  const triggers = useRef(new Map<TerminalPanel, HTMLButtonElement>());
  const baseId = useId();

  const show = useCallback((id: TerminalPanel) => {
    const bar = barRef.current, trigger = triggers.current.get(id);
    if (bar && trigger) {
      const barBox = bar.getBoundingClientRect(), box = trigger.getBoundingClientRect();
      const width = Math.min(PANEL_WIDTH, barBox.width - 8);
      setLeft(Math.max(4, Math.min(box.left - barBox.left, barBox.width - width - 4)));
    }
    setOpen(id);
  }, []);

  const close = useCallback((restoreFocus: boolean) => {
    setOpen(current => {
      if (current && restoreFocus) triggers.current.get(current)?.focus();
      return null;
    });
  }, []);

  useEffect(() => {
    const onOpen = (event: Event) => {
      const detail: unknown = (event as CustomEvent<unknown>).detail;
      if (isTerminalPanel(detail)) show(detail);
    };
    window.addEventListener(terminalOpenPanelEvent, onOpen);
    return () => window.removeEventListener(terminalOpenPanelEvent, onOpen);
  }, [show]);

  useEffect(() => {
    if (!open) return;
    panelRef.current?.focus();
    const onKey = (event: KeyboardEvent) => { if (event.key === 'Escape') { event.preventDefault(); close(true); } };
    const onPointer = (event: PointerEvent) => {
      const target = event.target as Node | null;
      if (!target || panelRef.current?.contains(target)) return;
      for (const trigger of triggers.current.values()) if (trigger.contains(target)) return;
      if (target instanceof Element && target.closest('[role="menu"],[role="dialog"][aria-modal="true"],[role="tooltip"]')) return;
      close(false);
    };
    document.addEventListener('keydown', onKey);
    document.addEventListener('pointerdown', onPointer);
    return () => { document.removeEventListener('keydown', onKey); document.removeEventListener('pointerdown', onPointer); };
  }, [open, close]);

  const panelId = (id: TerminalPanel) => `${baseId}-panel-${id}`;

  return (
    <div ref={barRef} role="toolbar" aria-label="Terminal status bar" className="relative flex h-7 w-full shrink-0 items-center gap-2 bg-surface-2 px-2">
      <div className="flex min-w-0 flex-1 items-center gap-1">
        <ConnectionPill poolAddress={pool} />
        <DropdownMenu label="Terminal settings" size="sm" trigger={<Settings aria-hidden="true" className="size-icon-sm" />} triggerClassName="h-6 px-1.5"
          items={[{ id: 'reset-columns', label: 'Reset column layout', onSelect: () => window.dispatchEvent(new CustomEvent(terminalResetColumnsEvent)) }]} />
        <div className="flex min-w-0 flex-1 items-center gap-0.5 overflow-x-auto [scrollbar-width:none]">
          {tools.map(tool => {
            const Icon = tool.icon;
            if (tool.kind === 'link') {
              return <Link key={tool.href} href={tool.href} className={toolClass}><Icon aria-hidden="true" />{tool.label}</Link>;
            }
            const pressed = open === tool.id;
            return (
              <button key={tool.id} type="button" ref={node => { if (node) triggers.current.set(tool.id, node); else triggers.current.delete(tool.id); }}
                aria-haspopup="dialog" aria-expanded={pressed} aria-controls={pressed ? panelId(tool.id) : undefined}
                onClick={() => (pressed ? close(false) : show(tool.id))}
                className={cn(toolClass, pressed && 'bg-surface-3 text-text hover:bg-surface-3')}>
                <Icon aria-hidden="true" />{tool.label}
              </button>
            );
          })}
        </div>
      </div>
      <div className="hidden shrink-0 items-center gap-3 lg:flex">
        <BalancesCluster />
        <div className="flex items-center gap-0.5">
          <CommunityLinks />
        </div>
        <span className="inline-flex items-center gap-1 text-xs text-text-muted" title="Only English is available">
          <Globe aria-hidden="true" className="size-icon-sm" />English
        </span>
      </div>
      {open ? (
        <div ref={panelRef} id={panelId(open)} role="dialog" aria-modal="false" aria-labelledby={`${panelId(open)}-title`} tabIndex={-1}
          style={{ left, width: `min(${PANEL_WIDTH}px, calc(100% - 8px))` }}
          className="absolute bottom-full z-[var(--sl-z-dropdown)] mb-1 flex max-h-[60vh] flex-col rounded-md bg-surface-3 shadow-pop focus-visible:outline-2 focus-visible:outline-focus">
          <header className="flex h-9 shrink-0 items-center gap-2 pl-3 pr-1">
            <h2 id={`${panelId(open)}-title`} className="min-w-0 flex-1 truncate text-sm font-bold text-text">{panelTitles[open]}</h2>
            <IconButton label={`Close ${panelTitles[open]}`} icon={<X />} size="sm" onClick={() => close(true)} />
          </header>
          <div className="min-h-0 flex-1 overflow-y-auto px-1 pb-2">{panelBody(open, pool)}</div>
        </div>
      ) : null}
    </div>
  );
}
