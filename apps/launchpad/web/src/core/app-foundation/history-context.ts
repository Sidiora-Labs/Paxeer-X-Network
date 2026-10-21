import { normalizeAddress, routeForPath, routes, type NavigationContext } from './navigation';
const HISTORY_KEY = 'kindleFoundationV1';
const FILTERS = ['category', 'sort', 'timeframe', 'query', 'tab'] as const;
export function captureNavigationContext(location: Location, scrollY: number): NavigationContext | null {
  const route = routeForPath(location.pathname);
  if (!route || !Number.isFinite(scrollY)) return null;
  const params = new URLSearchParams(location.search);
  const filters: Record<string, string> = {};
  for (const field of FILTERS) {
    const value = params.get(field);
    if (value !== null && value.length <= 256) filters[field] = value;
  }
  let poolAddress: string | undefined;
  if (route.id === 'token' && location.pathname.startsWith('/token/')) {
    try { poolAddress = normalizeAddress(location.pathname.split('/')[2]); } catch { return null; }
  }
  return { routeId: route.id, chainId: 125, poolAddress, tokenAddress: route.id === 'token-metadata' ? normalizeAddress(location.pathname.split('/')[2]) : undefined, selectedTab: filters.tab,
    returnContext: { filters, scrollY: Math.max(0, Math.round(scrollY)) } };
}
export function readNavigationContext(value: unknown): NavigationContext | null {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return null;
  const candidate = value as Partial<NavigationContext>;
  if (Object.keys(value).some(key => !['routeId', 'chainId', 'poolAddress', 'tokenAddress', 'selectedTab', 'returnContext'].includes(key))) return null;
  if (candidate.chainId !== 125 || !candidate.routeId || !routes.some(route => route.id === candidate.routeId)) return null;
  if (!candidate.returnContext || Array.isArray(candidate.returnContext) || Object.keys(candidate.returnContext).some(key => !['filters', 'scrollY'].includes(key)) || !Number.isSafeInteger(candidate.returnContext.scrollY) || candidate.returnContext.scrollY < 0) return null;
  if (!candidate.returnContext.filters || typeof candidate.returnContext.filters !== 'object' || Array.isArray(candidate.returnContext.filters)) return null;
  if (Object.entries(candidate.returnContext.filters).some(([key, val]) => !FILTERS.includes(key as typeof FILTERS[number]) || typeof val !== 'string' || val.length > 256)) return null;
  if (candidate.selectedTab !== undefined && (typeof candidate.selectedTab !== 'string' || candidate.selectedTab.length > 256 || candidate.selectedTab !== candidate.returnContext.filters.tab)) return null;
  if (candidate.tokenAddress !== undefined) { try { normalizeAddress(candidate.tokenAddress); } catch { return null; } }
  if (candidate.poolAddress !== undefined) { try { normalizeAddress(candidate.poolAddress); } catch { return null; } }
  return candidate as NavigationContext;
}
export function installNavigationHistory(): () => void {
  let restoring = false;
  let frame = 0;
  const snapshot = () => {
    if (restoring) return;
    const context = captureNavigationContext(window.location, window.scrollY);
    if (!context) return;
    const previous: unknown = window.history.state;
    if (previous !== null && typeof previous !== 'object') return;
    window.history.replaceState({ ...(previous || {}), [HISTORY_KEY]: context }, '', window.location.href);
  };
  const restore = (event: PopStateEvent) => {
    const state: unknown = event.state;
    if (!state || typeof state !== 'object') return;
    const context = readNavigationContext((state as Record<string, unknown>)[HISTORY_KEY]);
    const current = captureNavigationContext(window.location, window.scrollY);
    if (!context || !current || current.routeId !== context.routeId || current.poolAddress !== context.poolAddress || current.tokenAddress !== context.tokenAddress || JSON.stringify(current.returnContext?.filters) !== JSON.stringify(context.returnContext?.filters)) return;
    cancelAnimationFrame(frame);
    restoring = true;
    window.dispatchEvent(new CustomEvent<NavigationContext>('kindle:restore-navigation', { detail: context }));
    frame = requestAnimationFrame(() => { frame = requestAnimationFrame(() => {
      window.scrollTo({ top: context.returnContext!.scrollY, behavior: 'instant' }); restoring = false;
    }); });
  };
  snapshot();
  window.addEventListener('scroll', snapshot, { passive: true });
  document.addEventListener('click', snapshot, true);
  window.addEventListener('popstate', restore);
  window.addEventListener('kindle:navigation-complete', snapshot);
  return () => { restoring = false; cancelAnimationFrame(frame); window.removeEventListener('scroll', snapshot); document.removeEventListener('click', snapshot, true); window.removeEventListener('popstate', restore); window.removeEventListener('kindle:navigation-complete', snapshot); };
}
