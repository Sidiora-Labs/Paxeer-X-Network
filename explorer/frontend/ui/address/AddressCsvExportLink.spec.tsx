// @vitest-environment jsdom

import React from 'react';

import { render } from 'ui/shared/layout/testWrapper';
import { describe, expect, it, vi } from 'vitest';

vi.hoisted(() => {
  window.__envs = {
    ...window.__envs,
    NEXT_PUBLIC_RE_CAPTCHA_APP_SITE_KEY: 'test-site-key',
  };
});

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import AddressCsvExportLink from './AddressCsvExportLink';

const ADDRESS = '0xd789a607CEac2f0E14867de4EB15b15C9FFB5859';

describe('AddressCsvExportLink', () => {
  it('reads "Download" when the caller names no label', () => {
    const { container } = render(
      <AddressCsvExportLink address={ ADDRESS } params={{ type: 'transactions', filterType: 'address', filterValue: undefined }}/>,
    );

    expect(container.querySelector('[data-csv-export-label]')?.textContent).toBe('Download');
  });

  it('carries the label the card header asks for', () => {
    const { container } = render(
      <AddressCsvExportLink
        address={ ADDRESS }
        label="Download Page Data"
        params={{ type: 'transactions', filterType: 'address', filterValue: undefined }}
      />,
    );

    expect(container.querySelector('[data-csv-export-label]')?.textContent).toBe('Download Page Data');
  });

  it('points at the CSV export route with the address and the filter it was given', () => {
    const { container } = render(
      <AddressCsvExportLink
        address={ ADDRESS }
        label="CSV Export"
        params={{ type: 'token-transfers', filterType: 'address', filterValue: 'from' }}
      />,
    );

    const href = container.querySelector('a')?.getAttribute('href') ?? '';

    expect(href.startsWith('/csv-export?')).toBe(true);
    expect(href).toContain('type=token-transfers');
    expect(href).toContain('filterValue=from');
    expect(href).toContain(`address=${ ADDRESS }`);
  });
});
