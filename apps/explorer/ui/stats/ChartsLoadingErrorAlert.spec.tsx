// @vitest-environment jsdom

import React from 'react';

import { render } from 'ui/shared/layout/testWrapper';
import { describe, expect, it } from 'vitest';

import ChartsLoadingErrorAlert from './ChartsLoadingErrorAlert';

describe('ChartsLoadingErrorAlert', () => {
  it('warns that some charts did not load', () => {
    const { container } = render(<ChartsLoadingErrorAlert/>);

    const alert = container.querySelector('[data-charts-error]') as HTMLElement;

    expect(alert).not.toBeNull();
    expect(alert.textContent).toContain('Some of the charts did not load');
  });

  it('offers to load the page again', () => {
    const { container } = render(<ChartsLoadingErrorAlert/>);

    const link = container.querySelector('[data-charts-error] a') as HTMLAnchorElement;

    expect(link.textContent).toBe('click once again.');
    expect(link.getAttribute('href')).toBe(window.document.location.href);
  });
});
