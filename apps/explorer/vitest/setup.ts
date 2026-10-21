import dotenv from 'dotenv';

import { vi } from 'vitest';
import createFetchMock from 'vitest-fetch-mock';

const envs = dotenv.config({ path: './configs/envs/.env.vitest' });

const fetchMocker = createFetchMock(vi);
fetchMocker.enableMocks();

Object.defineProperty(globalThis, '__envs', {
  writable: true,
  value: envs.parsed || {},
});

if (typeof document !== 'undefined' && !('fonts' in document)) {
  const fontFaces = new Set();
  const loadedFontFace = { family: '', style: 'normal', weight: 'normal', stretch: 'normal', status: 'loaded' };

  const fontFaceSet: Record<PropertyKey, unknown> = {
    status: 'loaded',
    size: 0,
    onloading: null,
    onloadingdone: null,
    onloadingerror: null,
    load: () => Promise.resolve([ loadedFontFace ]),
    check: () => true,
    'delete': () => false,
    has: () => false,
    clear: () => undefined,
    forEach: () => undefined,
    keys: () => fontFaces.keys(),
    values: () => fontFaces.values(),
    entries: () => fontFaces.entries(),
    [Symbol.iterator]: () => fontFaces.values(),
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
    dispatchEvent: () => true,
  };

  fontFaceSet.add = () => fontFaceSet;
  fontFaceSet.ready = Promise.resolve(fontFaceSet);

  Object.defineProperty(document, 'fonts', {
    configurable: true,
    value: fontFaceSet,
  });
}
