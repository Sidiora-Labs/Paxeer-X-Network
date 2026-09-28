import assert from 'node:assert/strict';
import test from 'node:test';
import { RemoteBrowserSession } from './session.mjs';

const executablePath =
  process.env.CHROMIUM_EXECUTABLE_PATH ||
  '/root/.cache/ms-playwright/chromium-1234/chrome-linux64/chrome';

function jpegDimensions(buffer) {
  let offset = 2;
  while (offset + 9 < buffer.length) {
    if (buffer[offset] !== 0xff) {
      offset += 1;
      continue;
    }
    const marker = buffer[offset + 1];
    if (marker === 0xd8 || marker === 0xd9) {
      offset += 2;
      continue;
    }
    const length = buffer.readUInt16BE(offset + 2);
    if (
      [
        0xc0, 0xc1, 0xc2, 0xc3, 0xc5, 0xc6, 0xc7,
        0xc9, 0xca, 0xcb, 0xcd, 0xce, 0xcf,
      ].includes(marker)
    ) {
      return {
        height: buffer.readUInt16BE(offset + 5),
        width: buffer.readUInt16BE(offset + 7),
      };
    }
    if (length < 2) break;
    offset += 2 + length;
  }
  throw new Error('JPEG dimensions were not found.');
}

test(
  'injected provider binds wallet requests to the active top-level origin and navigation',
  { timeout: 45_000 },
  async () => {
    process.env.CHROMIUM_EXECUTABLE_PATH = executablePath;
    const session = await RemoteBrowserSession.create({
      url: 'https://example.com',
      width: 390,
      height: 640,
      deviceScaleFactor: 2,
    });
    try {
      const initialFrame = await session.frameAfter(0);
      assert.deepEqual(jpegDimensions(initialFrame.buffer), {
        width: 780,
        height: 1280,
      });

      const page = session.activeTab().page;
      const providerResult = page.evaluate(() =>
        globalThis.ethereum.request({ method: 'eth_requestAccounts' }),
      );

      let cursor = 0;
      let request;
      while (!request) {
        const batch = await session.eventsAfter(cursor);
        cursor = batch.cursor;
        request = batch.events.find(event => event.type === 'rpc')?.request;
      }

      assert.equal(request.origin, 'https://example.com');
      assert.equal(request.tabId, session.activeTabId);
      assert.equal(
        request.navigationGeneration,
        session.activeTab().navigationGeneration,
      );
      assert.equal(request.method, 'eth_requestAccounts');
      session.resolveRpc(request.id, {
        result: ['0x0000000000000000000000000000000000000001'],
      });
      assert.deepEqual(await providerResult, [
        '0x0000000000000000000000000000000000000001',
      ]);

      const staleResult = page
        .evaluate(() =>
          globalThis.ethereum.request({
            method: 'personal_sign',
            params: ['hello', '0x0000000000000000000000000000000000000001'],
          }),
        )
        .then(
          value => ({ ok: true, value }),
          error => ({ ok: false, error }),
        );
      let staleRequest;
      while (!staleRequest) {
        const batch = await session.eventsAfter(cursor);
        cursor = batch.cursor;
        staleRequest = batch.events.find(event => event.type === 'rpc')?.request;
      }
      await session.navigate('goto', 'https://example.org');
      const navigatedFrame = await session.frameAfter(initialFrame.version);
      assert.deepEqual(jpegDimensions(navigatedFrame.buffer), {
        width: 780,
        height: 1280,
      });
      const staleOutcome = await staleResult;
      assert.equal(staleOutcome.ok, false);
      assert.match(staleOutcome.error.message, /Execution context was destroyed|navigated/i);
      assert.throws(
        () => session.resolveRpc(staleRequest.id, { result: '0xlate' }),
        /not found/,
      );
    } finally {
      await session.close('test');
    }
  },
);
