// @vitest-environment jsdom

import React from 'react';

import { render } from 'ui/shared/layout/testWrapper';
import { describe, it, expect, vi } from 'vitest';
import { within } from 'vitest/lib';

vi.hoisted(() => {
  window.__envs = {
    ...window.__envs,
    NEXT_PUBLIC_NAVIGATION_LAYOUT: 'horizontal',
  };
});

import Content from './Content';

describe('Content', () => {
  it('holds the page in the main region of the document', () => {
    const { container } = render(<Content><div>page body</div></Content>);

    const content = container.querySelector('[data-label="content"]') as HTMLElement;

    expect(content.tagName).toBe('MAIN');
    expect(within(content).getByText('page body')).toBeTruthy();
  });

  it('keeps the styling a page hands it', () => {
    const { container } = render(<Content className="page-content"><div>page body</div></Content>);

    const content = container.querySelector('[data-label="content"]') as HTMLElement;

    expect(content.classList.contains('page-content')).toBe(true);
  });
});
