// @vitest-environment jsdom
import React, { act } from 'react';
import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { createRoot, type Root } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { WagmiProvider, createConfig, http } from 'wagmi';
import { defineChain } from 'viem';
import { AppRouterContext } from 'next/dist/shared/lib/app-router-context.shared-runtime';
import { SearchParamsContext } from 'next/dist/shared/lib/hooks-client-context.shared-runtime';
import { adaptForAppRouterInstance } from 'next/dist/shared/lib/router/adapters';
import singletonRouter from 'next/router';
import CreateWizard from './create-wizard';
import { FEE_RIGHTS_DETAIL, NAME_MAX_BYTES, SYMBOL_MAX_BYTES } from './create-form-fields';
import { feeStrategies } from './strategy-picker';
import { OpticalPresetPicker, STANDARD_OPTICAL } from './optical-preset-picker';
import { validateCreationDraft, type CreationDraftV2 } from './creation-storage';

beforeAll(() => { (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true; });

// Real wagmi + react-query + Next router contexts. Next's own adapter wraps its
// singleton router (any navigation would throw "No router instance found"; these
// tests never navigate). The RPC points at a closed local port, so every chain
// read fails honestly instead of returning fixtures.
const appRouter = adaptForAppRouterInstance(singletonRouter);
const chain = defineChain({ id: 125, name: 'Closed local boundary', nativeCurrency: { name: 'PAX', symbol: 'PAX', decimals: 18 }, rpcUrls: { default: { http: ['http://127.0.0.1:1'] } } });
const wagmiConfig = createConfig({ chains: [chain], transports: { 125: http('http://127.0.0.1:1', { retryCount: 0, timeout: 500 }) }, ssr: true, multiInjectedProviderDiscovery: false });

const NAME_ERROR = `Name must be 1 to ${NAME_MAX_BYTES} characters`;
const SYMBOL_ERROR = `Ticker must be 1 to ${SYMBOL_MAX_BYTES} characters`;

const mounted: Array<{ root: Root; container: HTMLElement; queryClient: QueryClient }> = [];

function mount(node: React.ReactElement) {
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  act(() => root.render(
    <WagmiProvider config={wagmiConfig}>
      <QueryClientProvider client={queryClient}>
        <AppRouterContext.Provider value={appRouter}>
          <SearchParamsContext.Provider value={new URLSearchParams()}>
            {node}
          </SearchParamsContext.Provider>
        </AppRouterContext.Provider>
      </QueryClientProvider>
    </WagmiProvider>,
  ));
  mounted.push({ root, container, queryClient });
  return container;
}

afterEach(() => {
  for (const { root, container, queryClient } of mounted.splice(0)) {
    act(() => root.unmount());
    container.remove();
    queryClient.clear();
  }
});

/** Types like a user: React tracks the native value setter, then sees the input event. */
function type(input: HTMLInputElement, value: string) {
  const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!;
  act(() => {
    setter.call(input, value);
    input.dispatchEvent(new Event('input', { bubbles: true }));
  });
}

const field = (container: HTMLElement, id: string) => container.querySelector<HTMLInputElement>(`#${id}`)!;

describe('CreateWizard pristine validation', () => {
  it('opens without error copy, alerts or aria-invalid on name and ticker', () => {
    const container = mount(<CreateWizard />);
    const name = field(container, 'create-name');
    const symbol = field(container, 'create-symbol');
    expect(name).not.toBeNull();
    expect(symbol).not.toBeNull();
    expect(name.getAttribute('aria-invalid')).toBeNull();
    expect(symbol.getAttribute('aria-invalid')).toBeNull();
    const text = container.textContent ?? '';
    expect(text).not.toContain(NAME_ERROR);
    expect(text).not.toContain(SYMBOL_ERROR);
    expect(text).not.toContain('Fix the highlighted fields first');
    expect(container.querySelectorAll('[role="alert"]')).toHaveLength(0);
  });

  it('shows the error after an invalid value is typed and keeps it after the field is cleared', () => {
    const container = mount(<CreateWizard />);
    const name = field(container, 'create-name');
    const symbol = field(container, 'create-symbol');

    type(name, 'Bad\u0007name');
    expect(name.getAttribute('aria-invalid')).toBe('true');
    expect(container.textContent).toContain(NAME_ERROR);
    // The ticker is still untouched and stays quiet.
    expect(symbol.getAttribute('aria-invalid')).toBeNull();
    expect(container.textContent).not.toContain(SYMBOL_ERROR);

    type(name, 'Paxe');
    expect(name.getAttribute('aria-invalid')).toBeNull();
    expect(container.textContent).not.toContain(NAME_ERROR);

    type(name, '');
    expect(name.getAttribute('aria-invalid')).toBe('true');
    expect(container.textContent).toContain(NAME_ERROR);

    type(symbol, 'PA​XE');
    expect(symbol.getAttribute('aria-invalid')).toBe('true');
    expect(container.textContent).toContain(SYMBOL_ERROR);
  });
});

describe('CreateWizard reference layout', () => {
  const classes = (node: Element | null | undefined) => (node?.getAttribute('class') ?? '').split(/\s+/);

  it('uses the compact heading scale and level Preview label', () => {
    const container = mount(<CreateWizard />);
    const h1 = container.querySelector('h1')!;
    expect(h1.textContent).toBe('Create new coin');
    expect(classes(h1)).toEqual(expect.arrayContaining(['text-md', 'font-semibold']));
    expect(classes(h1).some(name => /^text-(xl|2xl|display)$|^font-extrabold$/.test(name))).toBe(false);
    const preview = container.querySelector('#create-preview-title')!;
    expect(classes(preview)).toEqual(expect.arrayContaining(['text-md', 'font-semibold']));
    // The H1 and the Preview label are each the first child of their column, so they sit on one line.
    expect(h1.parentElement!.firstElementChild).toBe(h1);
    expect(preview.closest('aside')!.firstElementChild!.firstElementChild).toBe(preview);
  });

  it('puts name and ticker on one two-column row without empty counters or the ticker helper', () => {
    const container = mount(<CreateWizard />);
    const nameRow = field(container, 'create-name').closest('.grid');
    expect(nameRow).not.toBeNull();
    expect(nameRow!.contains(field(container, 'create-symbol'))).toBe(true);
    expect(classes(nameRow)).toContain('sm:grid-cols-2');
    const text = container.textContent ?? '';
    expect(text).not.toContain(`0/${NAME_MAX_BYTES}`);
    expect(text).not.toContain(`0/${SYMBOL_MAX_BYTES}`);
    expect(text).not.toContain('Case is kept exactly as typed');
    type(field(container, 'create-name'), 'Paxe');
    expect(container.querySelector('#create-name-count')!.textContent).toBe(`4/${NAME_MAX_BYTES}`);
  });

  it('renders a tall preview frame and keeps explainers to one line with the detail in a tooltip', () => {
    const container = mount(<CreateWizard />);
    const frame = container.querySelector('[data-create-preview-frame]');
    expect(classes(frame)).toContain('lg:min-h-72');
    expect(container.textContent).not.toContain(FEE_RIGHTS_DETAIL);
    expect(container.textContent).not.toContain('The factory takes no quote parameter');
    const info = container.querySelector<HTMLButtonElement>('button[aria-label="About creator rewards"]')!;
    act(() => info.focus());
    const tip = document.querySelector('[role="tooltip"]');
    expect(tip?.textContent).toBe(FEE_RIGHTS_DETAIL);
    expect(info.getAttribute('aria-describedby')).toBe(tip!.id);
    // The strategy picker is a segmented track; only the selected strategy's summary is spelled out.
    const radios = Array.from(container.querySelectorAll<HTMLElement>('[role="radiogroup"]')).find(group => group.textContent?.includes('Holders'))!;
    expect(classes(radios)).toContain('bg-surface-2');
    expect(container.textContent).toContain(feeStrategies[0].summary);
    expect(container.textContent).not.toContain(feeStrategies[1].summary);
  });

  it('never prints the raw RPC error when the optical registry read fails', async () => {
    const container = mount(<CreateWizard />);
    const deadline = Date.now() + 8000;
    while (!(container.textContent ?? '').includes("Custom rules can't load right now") && Date.now() < deadline) {
      await act(async () => { await new Promise(resolve => setTimeout(resolve, 100)); });
    }
    const text = container.textContent ?? '';
    expect(text).toContain("Custom rules can't load right now, so only Standard is available.");
    expect(text).not.toMatch(/HTTP request failed|Request body|viem@|eth_call/);
  }, 15_000);
});

describe('CreateWizard signed-out notice', () => {
  it('wraps the whole sentence instead of truncating it at narrow widths', () => {
    const container = mount(<CreateWizard />);
    const sentence = 'You can fill in everything now. A wallet is only needed when you create.';
    const notice = Array.from(container.querySelectorAll<HTMLElement>('[role="status"]')).find(node => node.textContent === sentence);
    expect(notice).toBeDefined();
    // No element between the notice and its text may clip to one line.
    const clipping = /^(truncate|whitespace-nowrap|text-ellipsis|overflow-hidden|line-clamp-\d+)$/;
    const chain = [notice!, ...Array.from(notice!.querySelectorAll<HTMLElement>('*'))];
    expect(chain.flatMap(node => (node.getAttribute('class') ?? '').split(/\s+/)).filter(name => clipping.test(name))).toEqual([]);
    // The bar keeps the icon on the first line rather than centring it against wrapped text.
    expect(notice!.className.split(/\s+/)).not.toContain('items-center');
  });
});

describe('OpticalPresetPicker registered presets', () => {
  const registered = [
    '0x1111111111111111111111111111111111111111',
    '0x2222222222222222222222222222222222222222',
    '0x3333333333333333333333333333333333333333',
    '0x4444444444444444444444444444444444444444',
    '0x5555555555555555555555555555555555555555',
  ];
  const blank: CreationDraftV2 = { draftId: 'd-1', version: 2, creator: '0x1234567890abcdef1234567890abcdef12345678', chainId: 125, name: 'Paxe', symbol: 'PAXE', description: '', tags: [], socialLinks: [], uploadRefs: [], firstBuyRaw: '0', opticalSelection: STANDARD_OPTICAL, updatedAt: Date.now(), feeStrategy: 0 };
  // Same admission rule the wizard passes in: the real draft validator.
  const reasonFor = (presetId: string) => (presetId === STANDARD_OPTICAL ? null : validateCreationDraft({ ...blank, opticalSelection: presetId }).opticalSelection ?? null);

  function Harness({ initial = STANDARD_OPTICAL }: { initial?: string }) {
    const [value, setValue] = React.useState(initial);
    return (
      <>
        <OpticalPresetPicker value={value} onChange={setValue} registered={registered} registry={{ state: 'ready' }} reasonFor={reasonFor} />
        <output data-testid="value">{value}</output>
      </>
    );
  }

  it('collapses the locked presets behind one disclosure while Standard AMM is selected', () => {
    const container = mount(<Harness />);
    const radios = Array.from(container.querySelectorAll<HTMLElement>('[role="radio"]'));
    expect(radios).toHaveLength(1);
    expect(radios[0].getAttribute('aria-checked')).toBe('true');
    expect(radios[0].textContent).toContain('Standard');
    const toggle = container.querySelector<HTMLButtonElement>('button[aria-expanded]')!;
    expect(toggle.getAttribute('aria-expanded')).toBe('false');
    expect(toggle.textContent).toContain('Custom rules');
    expect(toggle.textContent).toContain('5, coming soon');
    expect(container.textContent).toContain('Custom rules are coming soon');
  });

  it('shows muted locked notes when opened, and clicking a locked card keeps Standard AMM selected', () => {
    const container = mount(<Harness />);
    const toggle = container.querySelector<HTMLButtonElement>('button[aria-expanded]')!;
    act(() => toggle.click());
    expect(toggle.getAttribute('aria-expanded')).toBe('true');

    const locked = Array.from(container.querySelectorAll<HTMLElement>('[role="radio"][aria-disabled="true"]'));
    expect(locked).toHaveLength(5);
    const notes = Array.from(container.querySelectorAll<HTMLElement>('[role="note"]'));
    expect(notes).toHaveLength(5);
    for (const note of notes) {
      expect(note.textContent).toBe('Coming soon');
      expect(note.className).toContain('text-text-faint');
      expect(note.className).not.toContain('text-warning');
    }
    expect(container.textContent).not.toContain('Pick Standard, your other details are kept.');

    act(() => locked[2].click());
    expect(container.querySelector('[data-testid="value"]')!.textContent).toBe(STANDARD_OPTICAL);
    const standard = container.querySelector<HTMLElement>('[role="radio"]:not([aria-disabled])')!;
    expect(standard.getAttribute('aria-checked')).toBe('true');
    expect(locked.every(card => card.getAttribute('aria-checked') === 'false')).toBe(true);
  });

  it('keeps the disclosure open when a registered preset is already selected', () => {
    const container = mount(<Harness initial={registered[1]} />);
    const toggle = container.querySelector<HTMLButtonElement>('button[aria-expanded]')!;
    expect(toggle.getAttribute('aria-expanded')).toBe('true');
    const selected = container.querySelector<HTMLElement>('[role="radio"][aria-checked="true"]')!;
    expect(selected.textContent).toContain('Coming soon');
  });
});
