// @vitest-environment jsdom
import React, { act } from 'react';
import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { createRoot, type Root } from 'react-dom/client';
import { StreamTransportError } from '@kindle/sdk';
import { SourceNotice } from './source-notice';

beforeAll(() => { (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true; });
const mounted: Array<{ root: Root; container: HTMLElement }> = [];
function mount(node: React.ReactElement) {
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);
  act(() => root.render(node));
  mounted.push({ root, container });
  return container;
}
afterEach(() => { for (const { root, container } of mounted.splice(0)) { act(() => root.unmount()); container.remove(); } });

describe('SourceNotice', () => {
  it('words an unserved source plainly and never renders the operation', () => {
    const container = mount(<SourceNotice label="Coin stats" state={{ state: 'unsupported', operation: 'IF_STATS_BATCH', status: null, message: 'capability_unavailable' }} />);
    expect(container.textContent).toBe('Coin stats not available yet');
    expect(container.querySelector('[data-operation]')).toBeNull();
  });

  it('rewords a transport failure and keeps the retry', () => {
    let retried = 0;
    const container = mount(<SourceNotice label="Coin details" state={{ state: 'error', error: new StreamTransportError('NETWORK_INTERRUPTED'), retry: () => { retried += 1; } }} />);
    expect(container.textContent).toContain('Coin details: Connection problem');
    expect(container.textContent).not.toContain('NETWORK_INTERRUPTED');
    act(() => container.querySelector('button')?.click());
    expect(retried).toBe(1);
  });

  it('renders nothing for loading and ready sources', () => {
    expect(mount(<SourceNotice label="Coin stats" state={{ state: 'loading' }} />).innerHTML).toBe('');
    expect(mount(<SourceNotice label="Coin stats" state={{ state: 'ready', data: null }} />).innerHTML).toBe('');
  });
});
