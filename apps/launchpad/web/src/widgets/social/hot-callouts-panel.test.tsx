// @vitest-environment jsdom
/**
 * HotCalloutsPanel degraded states: the settled feed failure renders the compact chip inside the
 * surface-1 panel frame (the home board column pattern), never the large icon ErrorState panel, and
 * transport text never reaches the screen. Real panel, real query cache state.
 */
import React from 'react';
import { describe, expect, it } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { HttpError } from '@kindle/sdk';
import { queryKeys } from '@/core/query-keys';
import { HotCalloutsPanel } from './hot-callouts-panel';

const BORDER = /\b(border(?!-0\b|-none\b)(-[a-z0-9/\[\]-]+)?|divide-[a-z]|ring-(?!offset)[a-z0-9]|outline-(?!none|focus|2|offset)[a-z]+)\b|shadow/;

// retryOnMount: false keeps the settled error on the first render (otherwise the mount refetch shows the skeleton).
function renderSettled(error: Error) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, retryOnMount: false } } });
  const query = client.getQueryCache().build(client, { queryKey: queryKeys.feed('relevance', null, 50) });
  query.setState({ status: 'error', fetchStatus: 'idle', error, errorUpdateCount: 1, errorUpdatedAt: Date.now() });
  const html = renderToStaticMarkup(<QueryClientProvider client={client}><HotCalloutsPanel /></QueryClientProvider>);
  const host = document.createElement('div');
  host.innerHTML = html;
  return host;
}

describe('HotCalloutsPanel degraded states', () => {
  it('renders a transport failure as the compact error chip inside the panel frame', () => {
    const host = renderSettled(new TypeError('Failed to fetch'));
    const section = host.querySelector('section[aria-label="Hot callouts"]') as HTMLElement;
    expect(section).not.toBeNull();
    const chip = section.querySelector('[data-state="error"]') as HTMLElement;
    expect(chip).not.toBeNull();
    expect(chip.tagName).toBe('SPAN');
    expect(chip.textContent).toContain("Can't load callouts right now");
    expect(Array.from(chip.querySelectorAll('button')).map(button => button.textContent)).toEqual(['Try again']);
    // The frame keeps its surface-1 card; the pill takes surface-2 on it.
    const frame = chip.parentElement as HTMLElement;
    expect(frame.className).toContain('bg-surface-1');
    expect(frame.className).toContain('rounded-md');
    expect(chip.className).toContain('bg-surface-2');
    // No large icon panel, no raw transport text.
    expect(section.querySelector('h3')).toBeNull();
    expect(section.textContent).not.toMatch(/unavailable right now|Failed to fetch/i);
    // Header and feed selector stay.
    expect(section.querySelector('[aria-haspopup="menu"]')).not.toBeNull();
    for (const node of Array.from(section.querySelectorAll('*'))) expect(node.getAttribute('class') ?? '').not.toMatch(BORDER);
  });

  it('renders an unserved feed as the compact unavailable chip inside the panel frame', () => {
    const host = renderSettled(new HttpError(503, { code: 'DEPENDENCY_UNAVAILABLE', message: 'no backing service', requestId: 'r1', retryable: false, retryAfterMs: null, fieldErrors: [], details: null }));
    const section = host.querySelector('section[aria-label="Hot callouts"]') as HTMLElement;
    const chip = section.querySelector('[data-state="unavailable"]') as HTMLElement;
    expect(chip).not.toBeNull();
    expect(chip.textContent).toContain('Callouts are not available yet');
    expect(chip.textContent).toContain('Try again');
    expect(chip.parentElement?.className).toContain('bg-surface-1');
    expect(section.querySelector('h3')).toBeNull();
    expect(section.textContent).not.toMatch(/DEPENDENCY_UNAVAILABLE|HTTP 503/);
  });
});
