/** Stable route identity; permissions describe personalization, never block public shell. */
export const routes = [
  { id: 'discovery', pathname: '/', label: 'Home', permission: 'public' },
  { id: 'new', pathname: '/new', label: 'New coins', permission: 'public' },
  { id: 'token-metadata', pathname: '/tokens', label: 'Coin details', permission: 'wallet' },
  { id: 'token', pathname: '/token', label: 'Coin', permission: 'public' },
  { id: 'coin', pathname: '/coin', label: 'Coin', permission: 'public' },
  { id: 'explore', pathname: '/explore', label: 'Explore', permission: 'public' },
  { id: 'leaderboard', pathname: '/leaderboard', label: 'Leaderboard', permission: 'public' },
  { id: 'holder-rewards', pathname: '/holder-rewards', label: 'Holder rewards', permission: 'public' },
  { id: 'bounties', pathname: '/bounties', label: 'Bounties', permission: 'public' },
  { id: 'live-all', pathname: '/live/all', label: 'All live streams', permission: 'public' },
  { id: 'watchlist', pathname: '/watchlist', label: 'Watchlist', permission: 'wallet' },
  { id: 'swap', pathname: '/meta-ag/swap', label: 'Spot swaps', permission: 'public' },
  { id: 'legacy-swap', pathname: '/swap', label: 'Swap', permission: 'public' },
  { id: 'orders', pathname: '/meta-ag/orders', label: 'Spot', permission: 'wallet' },
  { id: 'terminal', pathname: '/terminal', label: 'Terminal', permission: 'public' },
  { id: 'live', pathname: '/live', label: 'Live', permission: 'public' },
  { id: 'chat', pathname: '/chat', label: 'Chat', permission: 'session' },
  { id: 'profile', pathname: '/profile', label: 'Profile', permission: 'wallet' },
  { id: 'rewards', pathname: '/rewards', label: 'Rewards', permission: 'wallet' },
  { id: 'create', pathname: '/create', label: 'Create', permission: 'wallet' },
  { id: 'docs', pathname: '/docs', label: 'Docs', permission: 'public' },
  { id: 'search', pathname: '/search', label: 'Search', permission: 'public' },
  { id: 'pnl-share', pathname: '/pnl', label: 'Shared PnL', permission: 'public' },
  { id: 'referral', pathname: '/r', label: 'Referral', permission: 'public' },
  { id: 'auth-callback', pathname: '/auth/callback', label: 'Sign in', permission: 'session' },
] as const;
export type RouteId = typeof routes[number]['id'];
export function isRouteActive(pathname: string, href: string): boolean {
  return pathname === href || (href !== '/' && pathname.startsWith(href + '/'));
}
export function routeForPath(pathname: string) {
  if (typeof pathname !== 'string' || !pathname.startsWith('/') || /[\\%?#\u0000-\u0020]/.test(pathname) || pathname.includes('//')) return undefined;
  const exact = routes.find(route => pathname === route.pathname);
  if (exact && exact.id !== 'token-metadata') return exact;
  const segments = pathname.split('/');
  if (segments.length === 4 && segments[1] === 'tokens' && /^0x[0-9a-fA-F]{40}$/.test(segments[2]) && segments[3] === 'metadata') return routes.find(route => route.id === 'token-metadata');
  if (segments.length !== 3) return undefined;
  const [, family, identifier] = segments;
  if ((family === 'token' || family === 'profile' || family === 'coin') && /^0x[0-9a-fA-F]{40}$/.test(identifier)) return routes.find(route => route.pathname === '/' + family);
  if (family === 'bounties' && /^[a-zA-Z0-9_-]{1,128}$/.test(identifier)) return routes.find(route => route.id === 'bounties');
  if (family === 'terminal' && (/^0x[0-9a-fA-F]{40}$/.test(identifier) || ['trending', 'portfolio', 'track', 'rewards', 'trenches'].includes(identifier))) return routes.find(route => route.id === 'terminal');
  if (family === 'docs' && ['terms', 'privacy', 'trademark-guidelines', 'dmca', 'do-not-sell'].includes(identifier)) return routes.find(route => route.id === 'docs');
  if (['chat', 'pnl', 'r'].includes(family) && /^[a-zA-Z0-9_-]{1,128}$/.test(identifier)) return routes.find(route => route.pathname === '/' + family);
  return undefined;
}
export function normalizeAddress(address: string): string {
  if (!/^0x[0-9a-fA-F]{40}$/.test(address)) throw new Error('Invalid address');
  return address.toLowerCase();
}
export function poolPath(chainId: number, poolAddress: string): string {
  if (chainId !== 125) throw new Error('Unsupported chain');
  return '/coin/' + normalizeAddress(poolAddress);
}
export interface ReturnContext { filters: Record<string, string>; scrollY: number }
export interface NavigationContext { routeId: RouteId; chainId: 125; poolAddress?: string; tokenAddress?: string; selectedTab?: string; returnContext?: ReturnContext }
export const focusRules = {
  route: 'Focus main landmark after pathname changes; preserve browser history scroll restoration.',
  search: 'Escape closes and restores input focus; Tab uses native button order.',
  dialog: 'Trap Tab in dialog, Escape closes, restore opener focus.',
} as const;

export function safeExternalDestination(value: string): string | null {
  if (typeof value !== 'string' || value.length > 2048 || /[\\\u0000-\u0020]/.test(value)) return null;
  try {
    const url = new URL(value);
    if (url.protocol !== 'https:' || url.username || url.password || url.search || url.hash || url.port) return null;
    if ((url.origin === 'https://x.com' || url.origin === 'https://t.me') && /^\/[A-Za-z0-9_.~+\-\/]{1,256}$/.test(url.pathname)) return url.href;
    if (url.origin === 'https://www.allaboutcookies.org' && url.pathname === '/') return url.href;
    if (url.origin === 'https://paxscan.paxeer.app' && (url.pathname === '/' || (/^(?:\/tx\/0x[0-9a-fA-F]{64}|\/address\/0x[0-9a-fA-F]{40})$/).test(url.pathname))) return url.href;
    return null;
  } catch { return null; }
}
