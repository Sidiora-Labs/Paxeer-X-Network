// @vitest-environment jsdom

import React from 'react';

import { render } from 'ui/shared/layout/testWrapper';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { fireEvent, screen, within } from 'vitest/lib';

vi.hoisted(() => {
  window.__envs = {
    ...window.__envs,
    NEXT_PUBLIC_NAVIGATION_LAYOUT: 'horizontal',
    NEXT_PUBLIC_NETWORK_NAME: 'Paxeer X Network',
    NEXT_PUBLIC_OTHER_LINKS: '[{"text":"GitHub","url":"https://github.com/Sidiora-Labs/Paxeer-X-Network"}]',
    NEXT_PUBLIC_HIDE_INDEXING_ALERT_INT_TXS: 'true',
  };
});

import Footer from './Footer';

const columnOf = (container: HTMLElement, title: string) => {
  const column = Array.from(container.querySelectorAll('[data-label="footer-column"]'))
    .find((candidate) => candidate.textContent?.startsWith(title));

  if (!column) {
    throw new Error(`${ title } is not a footer column`);
  }

  return column as HTMLElement;
};

describe('Footer', () => {
  beforeEach(() => {
    fetchMock.resetMocks();
    fetchMock.mockResponse(JSON.stringify({}), { headers: { 'Content-Type': 'application/json' } });
  });

  it('opens with the social links on the left and the back to top control on the right', () => {
    const { container } = render(<Footer/>);

    const top = container.querySelector('[data-label="footer-top"]') as HTMLElement;
    const social = within(top).getByText('GitHub').closest('a');

    expect(social?.getAttribute('href')).toBe('https://github.com/Sidiora-Labs/Paxeer-X-Network');
    expect(social?.querySelector('svg')).toBeTruthy();

    const markers = Array.from(top.querySelectorAll('[data-label]')).map((item) => item.getAttribute('data-label'));

    expect(markers).toEqual([ 'footer-social', 'back-to-top' ]);
  });

  it('takes the reader back to the top of the page', () => {
    const scrollTo = vi.spyOn(window, 'scrollTo');

    const { container } = render(<Footer/>);

    fireEvent.click(container.querySelector('[data-label="back-to-top"]') as HTMLElement);

    expect(scrollTo).toHaveBeenCalledWith({ top: 0, behavior: 'smooth' });

    scrollTo.mockRestore();
  });

  it('names the network and describes it in the first column', () => {
    const { container } = render(<Footer/>);

    const brand = container.querySelector('[data-label="footer-brand"]') as HTMLElement;

    expect(within(brand).getByText('Paxeer X Network')).toBeTruthy();
    expect(within(brand).getByText(/^The block explorer for Paxeer X Network/)).toBeTruthy();
    expect(within(brand).getByText('Frontend v1.0.11').closest('a')).toBeNull();
  });

  it('carries three columns of links to the pages of the explorer', () => {
    const { container } = render(<Footer/>);

    const columns = container.querySelectorAll('[data-label="footer-column"]');

    expect(columns).toHaveLength(3);

    expect(within(columnOf(container, 'Explore')).getByText('Blocks').closest('a')?.getAttribute('href')).toBe('/blocks');
    expect(within(columnOf(container, 'Network')).getByText('Gas tracker').closest('a')?.getAttribute('href')).toBe('/gas-tracker');
    expect(within(columnOf(container, 'Developers')).getByText('Verify contract').closest('a')?.getAttribute('href')).toBe('/contract-verification');
  });

  it('closes with the product name and the year and nothing else', () => {
    const { container } = render(<Footer/>);

    const bottom = container.querySelector('[data-label="footer-bottom"]') as HTMLElement;

    expect(within(bottom).getByText(`Paxeer X Network Block Explorer © ${ new Date().getFullYear() }`)).toBeTruthy();
    expect(screen.queryByText(/donat/i)).toBeNull();
  });
});
