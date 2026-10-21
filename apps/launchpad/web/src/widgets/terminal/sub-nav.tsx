import Link from 'next/link';
import { normalizeAddress } from '@/core/app-foundation/navigation';
import { cn } from '@/lib/utils';

export type TerminalView = 'trending' | 'portfolio' | 'track' | 'rewards' | 'trenches' | 'coin';
export type TerminalTabView = Exclude<TerminalView, 'coin'>;

export const terminalViews: ReadonlyArray<{ id: TerminalTabView; label: string }> = Object.freeze([
  { id: 'trending', label: 'Trending' },
  { id: 'portfolio', label: 'Portfolio' },
  { id: 'track', label: 'Track' },
  { id: 'rewards', label: 'Rewards' },
  { id: 'trenches', label: 'Trenches' },
]);

const namedViews: ReadonlySet<string> = new Set(['trending', 'portfolio', 'track', 'rewards', 'trenches']);
const POOL = /^0x[0-9a-fA-F]{40}$/;

export function terminalViewPath(view: TerminalTabView): string {
  return view === 'trending' ? '/terminal' : `/terminal/${view}`;
}

/** The terminal coin view for a pool; throws on anything but a 0x40hex address. */
export function terminalPoolPath(pool: string): string {
  return `/terminal/${normalizeAddress(pool)}`;
}

export type TerminalSlug =
  | { kind: 'view'; view: TerminalTabView }
  | { kind: 'pool'; pool: string }
  | { kind: 'invalid' };

/** The `[slug]` segment of /terminal/<slug>: none is the trending view, a named view, or a lowercased pool address. */
export function parseTerminalSlug(slug: string | null | undefined): TerminalSlug {
  if (slug === null || slug === undefined || slug === '') return { kind: 'view', view: 'trending' };
  if (namedViews.has(slug)) return { kind: 'view', view: slug as TerminalTabView };
  if (POOL.test(slug)) return { kind: 'pool', pool: slug.toLowerCase() };
  return { kind: 'invalid' };
}

/** Which terminal view a pathname shows; null outside the terminal or for an unknown segment. */
export function terminalViewForPath(pathname: string | null): TerminalView | null {
  if (!pathname) return null;
  if (pathname === '/terminal') return 'trending';
  const match = /^\/terminal\/([^/]+)$/.exec(pathname);
  if (!match) return null;
  const parsed = parseTerminalSlug(match[1]);
  return parsed.kind === 'view' ? parsed.view : parsed.kind === 'pool' ? 'coin' : null;
}

export default function TerminalSubNav({ active }: { active: TerminalView }) {
  return (
    <nav aria-label="Terminal views" className="min-w-0 overflow-x-auto scrollbar-none">
      <ul className="flex h-10 items-stretch">
        {terminalViews.map(view => {
          const selected = view.id === active;
          return (
            <li key={view.id} className="flex">
              <Link
                href={terminalViewPath(view.id)}
                aria-current={selected ? 'page' : undefined}
                className={cn(
                  'relative inline-flex items-center whitespace-nowrap rounded-sm px-3 text-sm font-bold transition-colors duration-fast hover:bg-surface-hover',
                  selected ? 'text-text' : 'text-text-muted hover:text-text',
                )}
              >
                {view.label}
                {selected ? <span aria-hidden="true" data-indicator className="absolute inset-x-3 bottom-0 h-0.5 rounded-pill bg-accent" /> : null}
              </Link>
            </li>
          );
        })}
      </ul>
    </nav>
  );
}
