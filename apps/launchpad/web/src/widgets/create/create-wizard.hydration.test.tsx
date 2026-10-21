// Runs in the node environment so the first half is a genuine server render
// (`typeof window === 'undefined'`) in one long-lived module graph, as the
// Next server is. The browser half installs vitest's own jsdom globals, loads
// the app modules afresh (a page load starts with new module state) and
// hydrates the HTML the server sent.
import { act, type ComponentType, type ReactNode } from 'react';
import { renderToString } from 'react-dom/server';
import { afterAll, expect, it, vi } from 'vitest';
import { builtinEnvironments } from 'vitest/environments';
import { cookieToInitialState, serialize, type State } from 'wagmi';
import { AppRouterContext, type AppRouterInstance } from 'next/dist/shared/lib/app-router-context.shared-runtime';
import { PathnameContext, SearchParamsContext } from 'next/dist/shared/lib/hooks-client-context.shared-runtime';

type AppModules = {
  createWagmiConfig: typeof import('@/core/wagmi-config').createWagmiConfig;
  AppProviders: typeof import('@/ui/providers/app-providers').default;
  CreateModule: ComponentType;
};

async function loadApp(): Promise<AppModules> {
  const [wagmi, providers, page] = await Promise.all([import('@/core/wagmi-config'), import('@/ui/providers/app-providers'), import('@/page-modules/create')]);
  return { createWagmiConfig: wagmi.createWagmiConfig, AppProviders: providers.default, CreateModule: page.default };
}

const visitor = '0x1111111111111111111111111111111111111111';
const connectedCookie = `wagmi.store=${serialize({
  state: {
    chainId: 125,
    current: 'visitor',
    connections: new Map([['visitor', { accounts: [visitor], chainId: 125, connector: { id: 'injected', name: 'Injected', type: 'injected', uid: 'visitor' } }]]),
  },
  version: 2,
})}`;

const disconnectedNotice = 'You can fill in everything now. A wallet is only needed when you create.';
const connectedNotice = 'Getting ready. You can create in a moment.';

// Navigation calls are recorded, not performed: nothing in this render navigates.
const navigations: string[] = [];
const router: AppRouterInstance = {
  back: () => navigations.push('back'),
  forward: () => navigations.push('forward'),
  refresh: () => navigations.push('refresh'),
  push: href => navigations.push(`push ${href}`),
  replace: href => navigations.push(`replace ${href}`),
  prefetch: () => {},
};

function NextAppRouter({ children }: { children: ReactNode }) {
  return (
    <AppRouterContext.Provider value={router}>
      <PathnameContext.Provider value="/create">
        <SearchParamsContext.Provider value={new URLSearchParams()}>{children}</SearchParamsContext.Provider>
      </PathnameContext.Provider>
    </AppRouterContext.Provider>
  );
}

/** The providers and page module app/layout.tsx renders for /create, with wagmi state derived from the Cookie header as RootLayout does. */
function createPage(app: AppModules, cookie: string) {
  const initialState: State | undefined = cookieToInitialState(app.createWagmiConfig(), cookie);
  const { AppProviders, CreateModule } = app;
  return (
    <NextAppRouter>
      <AppProviders wagmiInitialState={initialState}>
        <CreateModule />
      </AppProviders>
    </NextAppRouter>
  );
}

let teardown: ((global: typeof globalThis) => unknown) | undefined;
afterAll(async () => { await teardown?.(globalThis); });

it('renders /create for a cookie-less request the same way regardless of earlier visitors, and hydrates it without a mismatch', async () => {
  expect(typeof window).toBe('undefined');
  const server = await loadApp();

  const fresh = renderToString(createPage(server, ''));
  const otherVisitor = renderToString(createPage(server, connectedCookie));
  const afterVisitor = renderToString(createPage(server, ''));

  // The connected visitor's request does render the connected branches...
  expect(otherVisitor).toContain(connectedNotice);
  expect(otherVisitor).not.toContain(disconnectedNotice);
  // ...but none of that state reaches the next, cookie-less request.
  expect(afterVisitor).toContain(disconnectedNotice);
  expect(afterVisitor).toContain('Connect wallet to create');
  expect(afterVisitor).not.toContain(connectedNotice);
  expect(afterVisitor).toBe(fresh);

  // Browser: a new page load for the cookie-less visitor hydrates that HTML.
  const env = await builtinEnvironments.jsdom.setup(globalThis, { jsdom: { url: 'http://localhost/create' } });
  teardown = env.teardown;
  (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
  vi.resetModules();
  const browser = await loadApp();
  const { hydrateRoot } = await import('react-dom/client');

  const container = document.createElement('div');
  container.innerHTML = afterVisitor;
  document.body.appendChild(container);

  const consoleErrors: string[] = [];
  const recoverable: string[] = [];
  const original = console.error;
  console.error = (...args: unknown[]) => { consoleErrors.push(args.map(String).join(' ')); };
  try {
    let root: ReturnType<typeof hydrateRoot> | undefined;
    await act(async () => {
      root = hydrateRoot(container, createPage(browser, document.cookie), {
        onRecoverableError: error => { recoverable.push(error instanceof Error ? error.message : String(error)); },
      });
    });
    expect(container.textContent).toContain(disconnectedNotice);
    expect(container.textContent).toContain('Connect wallet to create');
    await act(async () => { root?.unmount(); });
  } finally {
    console.error = original;
    container.remove();
  }

  expect(recoverable).toEqual([]);
  expect(consoleErrors.filter(message => /hydrat|did not match|server rendered|#418/i.test(message))).toEqual([]);
  expect(navigations).toEqual([]);
}, 60_000);
