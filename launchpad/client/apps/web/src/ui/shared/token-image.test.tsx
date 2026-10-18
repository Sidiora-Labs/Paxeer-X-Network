import React from 'react';
import { describe, it, expect, vi } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import TokenImage from './token-image';

// vitest.config.ts sets `deps.interopDefault: false` (required by @kindle/sdk),
// so the default import of CJS `next/image` arrives as its whole `exports`
// object. Re-expose the real component as the default, as Next's bundler does.
vi.mock('next/image', async (importOriginal) => {
  const mod = await importOriginal<{ default: unknown }>();
  const cjs = mod.default as { default?: unknown } | undefined;
  return { ...mod, default: cjs && typeof cjs === 'object' && 'default' in cjs ? cjs.default : mod.default };
});

describe('token image raster policy', () => {
  it('uses local fallback for remote SVG even with query or uppercase extension', () => {
    for (const src of ['https://ipfs.io/ipfs/token.svg', 'https://metadata.kindlelaunch.com/logo/token.SVG?v=1']) {
      const html = renderToStaticMarkup(<TokenImage src={src} alt="Token" width={32} height={32} />);
      expect(html).toContain('shadcn.png');
      expect(html).not.toContain('ipfs.io');
      expect(html).not.toContain('metadata.kindlelaunch.com');
    }
  });
  it('keeps authored local SVG icons and optimizes raster originals', () => {
    const local = renderToStaticMarkup(<TokenImage src="/icons/token-fallback.svg" alt="Token" width={32} height={32} />);
    expect(local).toContain('src="/icons/token-fallback.svg"');
    const raster = renderToStaticMarkup(<TokenImage src="https://metadata.kindlelaunch.com/logo/token.png" alt="Token" width={32} height={32} />);
    expect(raster).toContain('/_next/image?');
    expect(raster).toContain('token.png');
  });
});
