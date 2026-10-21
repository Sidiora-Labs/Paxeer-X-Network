// @vitest-environment jsdom
import React, { act } from 'react';
import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { createRoot, type Root } from 'react-dom/client';
import { existsSync } from 'node:fs';
import { join } from 'node:path';
import { routeForPath } from '@/core/app-foundation/navigation';
import DocsIndexPage from './page';
import DocsLayout from './layout';
import { DOC_LINKS } from './doc-links';

beforeAll(() => { (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true; });
const mounted: Array<{ root: Root; container: HTMLElement }> = [];
afterEach(() => { for (const { root, container } of mounted.splice(0)) { act(() => root.unmount()); container.remove(); } });

describe('/docs index', () => {
  it('links every documentation page that exists, inside the docs layout', () => {
    const container = document.createElement('div');
    document.body.appendChild(container);
    const root = createRoot(container);
    act(() => root.render(<DocsLayout><DocsIndexPage /></DocsLayout>));
    mounted.push({ root, container });
    expect(container.querySelector('h1')?.textContent).toBe('Documentation');
    const article = container.querySelector('article')!;
    const hrefs = Array.from(article.querySelectorAll('a')).map(link => link.getAttribute('href'));
    expect(hrefs).toEqual(DOC_LINKS.map(link => link.href));
    for (const { href } of DOC_LINKS) {
      expect(existsSync(join(process.cwd(), 'src/app', href, 'page.tsx'))).toBe(true);
      expect(routeForPath(href)?.id).toBe('docs');
    }
    expect(routeForPath('/docs')?.id).toBe('docs');
    expect(container.innerHTML).not.toMatch(/\bborder(?!-0\b|-none\b)/);
  });

  it('sizes to its content: no fixed viewport or arbitrary min-height inside the app shell', () => {
    const container = document.createElement('div');
    document.body.appendChild(container);
    const root = createRoot(container);
    act(() => root.render(<DocsLayout><DocsIndexPage /></DocsLayout>));
    mounted.push({ root, container });
    for (const element of Array.from(container.querySelectorAll<HTMLElement>('*'))) {
      expect(element.getAttribute('class') ?? '').not.toMatch(/\bmin-h-(?!0\b)\S+/);
      expect(element.style.minHeight).toBe('');
      expect(element.style.height).toBe('');
    }
  });
});
