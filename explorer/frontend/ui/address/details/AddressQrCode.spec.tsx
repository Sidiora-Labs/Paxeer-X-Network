// @vitest-environment jsdom

import React from 'react';

import { render, routerState } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, waitFor } from 'vitest/lib';

const { generatorLoads, toQrString } = vi.hoisted(() => ({
  generatorLoads: { count: 0 },
  toQrString: vi.fn(),
}));

vi.mock('qrcode', () => {
  generatorLoads.count += 1;

  return { toString: toQrString };
});

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

// The dialog fetches the generator as its own module, and the first fetch of it in this environment is a
// transform of the package rather than a cached chunk, which takes longer than the default wait.
vi.setConfig({ testTimeout: 60_000 });

const MODULE_TIMEOUT = 30_000;

import AddressQrCode from './AddressQrCode';

const HASH = '0x1e7a5b0b0d3f4d5e6f708192a3b4c5d6e7f80912';

const control = (container: HTMLElement) => container.querySelector('button[aria-label="Show QR code"]') as HTMLElement | null;

describe('AddressQrCode', () => {
  beforeEach(() => {
    routerState.pathname = '/address/[hash]';
    routerState.query = { hash: HASH };
    generatorLoads.count = 0;
    toQrString.mockReset();
  });

  it('carries the control without fetching the generator', () => {
    const { container } = render(<AddressQrCode hash={ HASH }/>);

    expect(control(container)).toBeTruthy();
    expect(generatorLoads.count).toBe(0);
    expect(toQrString).not.toHaveBeenCalled();
  });

  it('fetches the generator when the dialog is opened and draws what it returns', async() => {
    toQrString.mockImplementation((...args: Array<unknown>) => {
      const callback = args[2] as (error: Error | null, svg: string) => void;
      callback(null, '<svg data-qr-code="true"></svg>');
    });

    const { container } = render(<AddressQrCode hash={ HASH }/>);

    fireEvent.click(control(container) as HTMLElement);

    await waitFor(() => {
      expect(generatorLoads.count).toBe(1);
    }, { timeout: MODULE_TIMEOUT });
    await waitFor(() => {
      expect(toQrString).toHaveBeenCalled();
    }, { timeout: MODULE_TIMEOUT });

    expect(toQrString.mock.calls[0][0]).toBe(HASH);
    await waitFor(() => {
      expect(document.body.querySelector('[data-qr-code]')).toBeTruthy();
    }, { timeout: MODULE_TIMEOUT });
  });

  it('shows the skeleton instead of the control while the address is loading', () => {
    const { container } = render(<AddressQrCode hash={ HASH } isLoading/>);

    expect(control(container)).toBeNull();
    expect(generatorLoads.count).toBe(0);
  });
});
