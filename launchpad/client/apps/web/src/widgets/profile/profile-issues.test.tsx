import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import { createRequire } from 'node:module';
import React, { act } from 'react';
import type { Root } from 'react-dom/client';
import type { QueryClient as QueryClientType } from '@tanstack/react-query';
import { afterAll, describe, expect, it } from 'vitest';

// Same environment recipe as leaderboard-panel.test.tsx: node fetch/AbortController with the browser
// globals Node lacks taken from a real jsdom window.
interface BrowserDom { window: Window & typeof globalThis }
type JsdomModule = { JSDOM: new (html: string, options: { url: string; pretendToBeVisual: boolean }) => BrowserDom };
const { JSDOM } = createRequire(import.meta.url)('jsdom') as JsdomModule;
const dom = new JSDOM('<!DOCTYPE html><html><body></body></html>', { url: 'http://localhost:3000', pretendToBeVisual: true });
const browserGlobals = globalThis as unknown as Record<string, unknown>;
for (const key of Object.getOwnPropertyNames(dom.window)) {
  if (key in globalThis || key.startsWith('_')) continue;
  const value = (dom.window as unknown as Record<string, unknown>)[key];
  browserGlobals[key] = typeof value === 'function' && !/^[A-Z]/.test(key) ? value.bind(dom.window) : value;
}
browserGlobals.window = dom.window;
browserGlobals.document = dom.window.document;
browserGlobals.IS_REACT_ACT_ENVIRONMENT = true;

type Responder = (url: string) => { status: number; body?: unknown };
function serve(responder: Responder): Promise<{ server: Server; origin: string; paths: string[] }> {
  const paths: string[] = [];
  const server = createServer((request, response) => {
    paths.push(request.url ?? '');
    const answer = responder(request.url ?? '');
    response.statusCode = answer.status;
    if (answer.body === undefined) { response.end(); return; }
    response.setHeader('Content-Type', 'application/json');
    response.end(JSON.stringify(answer.body));
  });
  return new Promise(resolve => {
    server.listen(0, '127.0.0.1', () => resolve({ server, origin: `http://127.0.0.1:${(server.address() as AddressInfo).port}`, paths }));
  });
}


const broken = '0x' + '77'.repeat(20);
/** interfaces.Error as l17 writeFailure encodes it, marked non-retryable so the read fails at once. */
const failure = { code: 'INTERNAL', message: 'Portfolio request failed', requestId: 'AAAAAAAAAAAAAAAAAAAAAA', retryable: false, fieldErrors: [], details: null, retryAfterMs: null };
const pnl = await serve(() => ({ status: 500, body: failure }));
process.env.NEXT_PUBLIC_PNL_API_URL = pnl.origin;

const { createRoot } = await import('react-dom/client');
const { QueryClient, QueryClientProvider } = await import('@tanstack/react-query');
const { WagmiProvider, createConfig, http } = await import('wagmi');
const { plainCopy } = await import('@/core/user-facing-error');
const { defineChain } = await import('viem');
const { BalanceVisibilityProvider } = await import('./balance-visibility');
const { PortfolioBlock } = await import('./portfolio-block');
const { PnlSummary } = await import('./pnl-summary');
const { TopTrades } = await import('./top-trades');
const { ProfileIssuesProvider, ProfileIssuesNotice, profileIssueCopy, joinLabels } = await import('./profile-issues');

afterAll(() => { pnl.server.close(); });

const paxeer = defineChain({ id: 125, name: 'Paxeer', nativeCurrency: { name: 'PAX', symbol: 'PAX', decimals: 18 }, rpcUrls: { default: { http: ['http://127.0.0.1:1'] } } });
const wagmiConfig = createConfig({ chains: [paxeer], transports: { [paxeer.id]: http() }, ssr: true, multiInjectedProviderDiscovery: false });

function mount(node: React.ReactElement): { root: Root; container: HTMLDivElement; queryClient: QueryClientType } {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);
  act(() => {
    root.render(
      <WagmiProvider config={wagmiConfig}>
        <QueryClientProvider client={queryClient}>
          <BalanceVisibilityProvider>{node}</BalanceVisibilityProvider>
        </QueryClientProvider>
      </WagmiProvider>,
    );
  });
  return { root, container, queryClient };
}

function unmount({ root, container, queryClient }: ReturnType<typeof mount>) {
  act(() => root.unmount());
  queryClient.clear();
  container.remove();
}

async function waitFor(check: () => boolean, timeoutMs = 6000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (!check()) {
    if (Date.now() > deadline) throw new Error(`Condition not met in time: ${document.body.textContent?.slice(0, 400)}`);
    await act(async () => { await new Promise(resolve => setTimeout(resolve, 20)); });
  }
}

describe('profile page notice against a failing pnl-tracker origin', () => {
  it('collapses every block failure into one notice, with dashes in the slots and no red paragraphs', async () => {
    const view = mount(
      <ProfileIssuesProvider>
        <ProfileIssuesNotice />
        <PortfolioBlock wallet={broken} isOwner={false} />
        <PnlSummary wallet={broken} isOwner={false} />
        <TopTrades wallet={broken} />
      </ProfileIssuesProvider>,
    );
    await waitFor(() => view.container.querySelector('[data-profile-notice]') !== null && view.container.textContent!.includes('Top trades'));
    await waitFor(() => (view.container.querySelector('[data-profile-notice]')?.textContent ?? '').includes('Top trades'));
    const notices = view.container.querySelectorAll('[data-profile-notice]');
    expect(notices).toHaveLength(1);
    const notice = notices[0].textContent ?? '';
    expect(notice).toContain("Some of this profile didn't load");
    expect(notice).toContain(plainCopy.server);
    expect(notice).toContain('Portfolio value, PnL and Top trades');
    expect(view.container.querySelectorAll('[role="alert"]')).toHaveLength(0);
    expect(view.container.querySelectorAll('.text-danger')).toHaveLength(0);
    expect(view.container.textContent).not.toMatch(/unavailable right now|INTERNAL|status 500/);
    expect(view.container.querySelector('[data-top-trades-placeholder]')).not.toBeNull();
    expect(view.container.textContent).toContain('—');
    expect(Array.from(view.container.querySelectorAll('button')).some(button => button.textContent === 'Try again')).toBe(true);
    unmount(view);
  });

  it('keeps each block\'s own inline error when no page notice collects it', async () => {
    const view = mount(<TopTrades wallet={broken} />);
    await waitFor(() => view.container.querySelector('[role="alert"]') !== null);
    expect(view.container.textContent).toContain("Can't load top trades right now");
    expect(view.container.querySelector('[data-top-trades-placeholder]')).toBeNull();
    unmount(view);
  });
});

describe('profile notice copy', () => {
  it('never prints a raw fetch failure and shares the reason when every block failed the same way', () => {
    const copy = profileIssueCopy([
      { label: 'Follower counts', error: new TypeError('Failed to fetch') },
      { label: 'Published profile', error: 'Failed to fetch' },
      { label: 'Top trades', error: new TypeError('fetch failed') },
    ]);
    expect(copy?.body).toBe(plainCopy.network);
    expect(JSON.stringify(copy)).not.toMatch(/fetch/i);
    expect(profileIssueCopy([])).toBeNull();
    expect(profileIssueCopy([{ label: 'A', error: new TypeError('Failed to fetch') }, { label: 'B', error: Object.assign(new Error('x'), { status: 500 }) }])?.body).toBe("Some of this profile didn't load, try again in a moment");
  });

  it('joins section names as a sentence', () => {
    expect(joinLabels(['A'])).toBe('A');
    expect(joinLabels(['A', 'B'])).toBe('A and B');
    expect(joinLabels(['A', 'B', 'C'])).toBe('A, B and C');
  });
});
