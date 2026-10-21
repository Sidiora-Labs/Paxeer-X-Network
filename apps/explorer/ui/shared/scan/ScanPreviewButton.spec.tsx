// @vitest-environment jsdom

import React from 'react';

import { Provider } from 'toolkit/chakra/provider';
import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { cleanup, render } from 'vitest/lib';

import ScanPreviewButton from './ScanPreviewButton';

// jsdom implements no CSS media queries, and the Chakra provider reads window.matchMedia on mount
const createMediaQueryList = (query: string): MediaQueryList => ({
  matches: false,
  media: query,
  onchange: null,
  addListener: () => undefined,
  removeListener: () => undefined,
  addEventListener: () => undefined,
  removeEventListener: () => undefined,
  dispatchEvent: () => false,
});

beforeAll(() => {
  Object.defineProperty(window, 'matchMedia', { writable: true, value: createMediaQueryList });
});

// the suite runs without vitest globals, so the testing library cannot register its own teardown
afterEach(cleanup);

describe('ScanPreviewButton', () => {
  it('offers a single icon control labelled for a reader', () => {
    const { container } = render(
      <Provider>
        <ScanPreviewButton>
          <span>the input data</span>
        </ScanPreviewButton>
      </Provider>,
    );

    const trigger = container.querySelector('[data-scan-preview]');

    expect(trigger).not.toBeNull();
    expect(trigger?.getAttribute('aria-label')).toBe('Preview');
    expect(trigger?.querySelector('svg')).not.toBeNull();
  });

  it('takes the label a caller gives it', () => {
    const { container } = render(
      <Provider>
        <ScanPreviewButton label="Preview input data">
          <span>the input data</span>
        </ScanPreviewButton>
      </Provider>,
    );

    expect(container.querySelector('[data-scan-preview]')?.getAttribute('aria-label')).toBe('Preview input data');
  });

  it('keeps the preview out of the row until the control is used', () => {
    const { container } = render(
      <Provider>
        <ScanPreviewButton>
          <span>the input data</span>
        </ScanPreviewButton>
      </Provider>,
    );

    expect(container.querySelector('[data-preview-body]')).toBeNull();
    expect(container.textContent).not.toContain('the input data');
  });
});
