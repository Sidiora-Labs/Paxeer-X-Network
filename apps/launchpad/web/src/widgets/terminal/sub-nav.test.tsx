// @vitest-environment jsdom
import React, { act } from 'react';
import { beforeAll, describe, expect, it } from 'vitest';
import { createRoot } from 'react-dom/client';
import TerminalSubNav, { terminalViews, type TerminalView } from './sub-nav';

beforeAll(() => { (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true; });

function render(active: TerminalView) {
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);
  act(() => root.render(<TerminalSubNav active={active} />));
  const links = Array.from(container.querySelectorAll('a'));
  const result = links.map(link => ({ text: link.textContent, href: link.getAttribute('href'), current: link.getAttribute('aria-current'), indicator: link.querySelector('[data-indicator]') !== null }));
  act(() => root.unmount());
  container.remove();
  return result;
}

describe('TerminalSubNav', () => {
  it('links every view and marks only the active one', () => {
    const links = render('track');
    expect(links.map(link => link.text)).toEqual(terminalViews.map(view => view.label));
    expect(links.map(link => link.href)).toEqual(['/terminal', '/terminal/portfolio', '/terminal/track', '/terminal/rewards', '/terminal/trenches']);
    expect(links.filter(link => link.current === 'page').map(link => link.text)).toEqual(['Track']);
    expect(links.filter(link => link.indicator).map(link => link.text)).toEqual(['Track']);
  });
  it('marks no tab while a pair is open', () => {
    const links = render('coin');
    expect(links.some(link => link.current !== null || link.indicator)).toBe(false);
  });
});
