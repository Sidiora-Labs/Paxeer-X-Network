import crypto from 'node:crypto';
import fs from 'node:fs/promises';
import { chromium } from 'playwright-core';
import { PROVIDER_SOURCE } from './provider-source.mjs';
import { normalizePublicHttpsUrl, requestUrlAllowed } from './security.mjs';

const MAX_EVENTS = 256;
const MAX_RPC_BYTES = 64 * 1_024;
const RPC_TIMEOUT_MS = 60_000;
const FRAME_JPEG_QUALITY = 92;
const FRAME_CAPTURE_INTERVAL_MS = 80;
const SESSION_IDLE_MS = Number(process.env.BROWSER_PLANE_IDLE_MS ?? 15 * 60_000);
const STREAM_ENABLED = process.env.BROWSER_STREAM_ENABLED === '1';
const STREAM_WIDTH = boundedViewport(
  Number(process.env.BROWSER_STREAM_WIDTH ?? 780),
  780,
);
const STREAM_HEIGHT = boundedViewport(
  Number(process.env.BROWSER_STREAM_HEIGHT ?? 1592),
  1592,
);
const METHOD_PATTERN = /^[a-z][a-zA-Z0-9_]{1,63}$/;
const PROVIDER_EVENTS = new Set([
  'accountsChanged',
  'chainChanged',
  'connect',
  'disconnect',
  'message',
]);

function waitForSignal(register, timeoutMs) {
  return new Promise(resolve => {
    let settled = false;
    let timer;
    const finish = value => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      resolve(value);
    };
    const unregister = register(finish);
    timer = setTimeout(() => {
      unregister();
      finish(null);
    }, timeoutMs);
  });
}

function boundedViewport(value, fallback) {
  return Number.isSafeInteger(value) ? Math.min(1_920, Math.max(320, value)) : fallback;
}

function normalizeRpcRequest(value) {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    throw Object.assign(new Error('Invalid provider request.'), { code: -32600 });
  }
  const { id, method, params } = value;
  if (typeof id !== 'string' || id.length < 1 || id.length > 128) {
    throw Object.assign(new Error('Invalid request id.'), { code: -32600 });
  }
  if (typeof method !== 'string' || !METHOD_PATTERN.test(method)) {
    throw Object.assign(new Error('Invalid request method.'), { code: -32600 });
  }
  if (
    params !== undefined &&
    !Array.isArray(params) &&
    (typeof params !== 'object' || params === null)
  ) {
    throw Object.assign(new Error('Invalid request params.'), { code: -32602 });
  }
  const serialized = JSON.stringify({ id, method, params });
  if (Buffer.byteLength(serialized) > MAX_RPC_BYTES) {
    throw Object.assign(new Error('Request exceeds the size limit.'), { code: -32602 });
  }
  return { id, method, params: params ?? [] };
}

function rpcFailure(source) {
  const error = new Error(
    typeof source?.message === 'string' ? source.message : 'Wallet request failed.',
  );
  error.code = typeof source?.code === 'number' ? source.code : -32603;
  if (source && typeof source === 'object' && 'data' in source) error.data = source.data;
  return error;
}

export class RemoteBrowserSession {
  static async create({ url, width, height, deviceScaleFactor }) {
    const target = await normalizePublicHttpsUrl(url);
    const session = new RemoteBrowserSession({
      width: boundedViewport(width, 390),
      height: boundedViewport(height, 720),
      deviceScaleFactor:
        typeof deviceScaleFactor === 'number'
          ? Math.min(3, Math.max(1, deviceScaleFactor))
          : 1,
    });
    await session.start(target.toString());
    return session;
  }

  constructor({ width, height, deviceScaleFactor }) {
    this.id = crypto.randomUUID();
    this.token = crypto.randomBytes(32).toString('base64url');
    this.width = width;
    this.height = height;
    this.deviceScaleFactor = deviceScaleFactor;
    this.browser = null;
    this.context = null;
    this.profileDir = null;
    this.tabs = new Map();
    this.pageToTab = new Map();
    this.activeTabId = null;
    this.events = [];
    this.eventSequence = 0;
    this.eventWaiters = new Set();
    this.latestFrame = null;
    this.frameVersion = 0;
    this.frameWaiters = new Set();
    this.pendingRpc = new Map();
    this.closeHandlers = new Set();
    this.streamEnabled = STREAM_ENABLED;
    this.closed = false;
    this.lastActivity = Date.now();
    this.idleTimer = setInterval(() => {
      if (Date.now() - this.lastActivity > SESSION_IDLE_MS) {
        void this.close('idle-timeout').catch(() => undefined);
      }
    }, Math.min(60_000, Math.max(5_000, Math.floor(SESSION_IDLE_MS / 3))));
    this.idleTimer.unref();
  }

  authorize(token) {
    if (typeof token !== 'string') return false;
    const expected = Buffer.from(this.token);
    const actual = Buffer.from(token);
    return expected.length === actual.length && crypto.timingSafeEqual(expected, actual);
  }

  touch() {
    this.lastActivity = Date.now();
  }

  onClose(handler) {
    this.closeHandlers.add(handler);
    return () => this.closeHandlers.delete(handler);
  }

  async start(url) {
    const executablePath =
      process.env.CHROMIUM_EXECUTABLE_PATH || chromium.executablePath();
    const args = [
      '--disable-background-networking',
      '--disable-component-update',
      '--disable-default-apps',
      '--disable-dev-shm-usage',
      '--disable-features=Translate,OptimizationHints,MediaRouter',
      '--disable-sync',
      '--metrics-recording-only',
      '--no-first-run',
      '--password-store=basic',
      '--use-mock-keychain',
    ];
    if (process.env.BROWSER_CHROMIUM_NO_SANDBOX === '1') {
      args.push('--no-sandbox', '--disable-setuid-sandbox');
    }
    if (this.streamEnabled) {
      args.push(
        `--app=${url}`,
        '--kiosk',
        '--start-fullscreen',
        '--start-maximized',
        '--window-position=0,0',
        `--window-size=${STREAM_WIDTH},${STREAM_HEIGHT}`,
        `--force-device-scale-factor=${this.deviceScaleFactor}`,
      );
    }
    const contextOptions = {
      ...(this.streamEnabled
        ? { viewport: null }
        : {
            viewport: { width: this.width, height: this.height },
            deviceScaleFactor: this.deviceScaleFactor,
            hasTouch: true,
            isMobile: true,
          }),
      locale: 'en-US',
      colorScheme: 'dark',
      acceptDownloads: false,
      serviceWorkers: 'allow',
    };
    if (this.streamEnabled) {
      this.profileDir = await fs.mkdtemp('/tmp/paxport-browser-');
      this.context = await chromium.launchPersistentContext(this.profileDir, {
        executablePath,
        headless: false,
        ignoreDefaultArgs: ['about:blank'],
        args,
        ...contextOptions,
      });
      this.browser = this.context.browser();
    } else {
      this.browser = await chromium.launch({
        executablePath,
        headless: true,
        args,
      });
      this.context = await this.browser.newContext(contextOptions);
    }

    await this.context.exposeBinding(
      '__paxportRemoteRpc',
      (source, request) => this.handleProviderRequest(source, request),
    );
    await this.context.addInitScript({ content: PROVIDER_SOURCE });
    await this.context.route('**/*', async route => {
      if (await requestUrlAllowed(route.request().url())) {
        await route.continue();
      } else {
        await route.abort('blockedbyclient');
      }
    });
    this.context.on('page', page => {
      void this.attachPage(page, true);
    });

    const page = this.context.pages()[0] ?? (await this.context.newPage());
    await this.attachPage(page, true);
    await page.goto(url, { waitUntil: 'domcontentloaded', timeout: 30_000 });
    await this.publishState();
  }

  async attachPage(page, activate) {
    const existing = this.pageToTab.get(page);
    if (existing) {
      if (activate) await this.activateTab(existing);
      return existing;
    }

    const tabId = crypto.randomUUID();
    const tab = {
      id: tabId,
      page,
      cdp: null,
      title: '',
      url: page.url(),
      navigationGeneration: 0,
      captureTimer: null,
      captureRunning: false,
      captureRequested: false,
      lastCaptureAt: 0,
    };
    this.tabs.set(tabId, tab);
    this.pageToTab.set(page, tabId);

    page.on('framenavigated', frame => {
      if (frame !== page.mainFrame()) return;
      tab.url = frame.url();
      tab.navigationGeneration += 1;
      this.rejectPendingForTab(tabId, {
        code: 4001,
        message: 'The page navigated before the wallet request completed.',
      });
      void this.publishState();
    });
    page.on('domcontentloaded', () => {
      void page.title().then(title => {
        tab.title = title.slice(0, 256);
        void this.publishState();
      });
    });
    page.on('close', () => {
      clearTimeout(tab.captureTimer);
      tab.captureTimer = null;
      this.tabs.delete(tabId);
      this.pageToTab.delete(page);
      this.rejectPendingForTab(tabId, {
        code: 4900,
        message: 'The browser tab was closed.',
      });
      if (this.activeTabId === tabId) {
        this.activeTabId = this.tabs.keys().next().value ?? null;
      }
      void this.publishState();
    });
    page.on('download', download => {
      void download.cancel();
      this.pushEvent({
        type: 'notice',
        level: 'warning',
        message: 'Downloads are disabled in the wallet browser.',
      });
    });

    if (!this.streamEnabled) {
      const cdp = await this.context.newCDPSession(page);
      tab.cdp = cdp;
      cdp.on('Page.screencastFrame', event => {
        void cdp
          .send('Page.screencastFrameAck', { sessionId: event.sessionId })
          .catch(() => undefined);
        this.queueFrameCapture(tab);
      });
      await cdp.send('Page.startScreencast', {
        format: 'jpeg',
        quality: 30,
        maxWidth: 320,
        maxHeight: 320,
        everyNthFrame: 1,
      });
    }

    if (activate || !this.activeTabId) {
      await this.activateTab(tabId);
    }
    await this.publishState();
    return tabId;
  }

  async activateTab(tabId) {
    const tab = this.tabs.get(tabId);
    if (!tab) throw new Error('Browser tab not found.');
    this.activeTabId = tabId;
    this.latestFrame = null;
    await tab.page.bringToFront();
    clearTimeout(tab.captureTimer);
    tab.captureTimer = null;
    tab.captureRequested = false;
    if (!this.streamEnabled) await this.captureFrame(tab);
    await this.publishState();
  }

  queueFrameCapture(tab) {
    if (
      this.closed ||
      tab.page.isClosed() ||
      this.activeTabId !== tab.id
    ) {
      return;
    }
    tab.captureRequested = true;
    if (tab.captureRunning || tab.captureTimer) return;

    const delay = Math.max(
      0,
      FRAME_CAPTURE_INTERVAL_MS - (Date.now() - tab.lastCaptureAt),
    );
    tab.captureTimer = setTimeout(() => {
      tab.captureTimer = null;
      if (
        this.closed ||
        tab.page.isClosed() ||
        this.activeTabId !== tab.id ||
        !tab.captureRequested
      ) {
        tab.captureRequested = false;
        return;
      }
      tab.captureRequested = false;
      void this.captureFrame(tab);
    }, delay);
    tab.captureTimer.unref();
  }

  async captureFrame(tab = this.activeTab()) {
    if (!tab || tab.page.isClosed() || tab.captureRunning) return;
    tab.captureRunning = true;
    try {
      const frame = await tab.page.screenshot({
        type: 'jpeg',
        quality: FRAME_JPEG_QUALITY,
        animations: 'allow',
        scale: 'device',
        timeout: 5_000,
      });
      if (this.closed || this.activeTabId !== tab.id) return;
      this.latestFrame = frame;
      this.frameVersion += 1;
      for (const waiter of this.frameWaiters) waiter(this.latestFrame);
      this.frameWaiters.clear();
    } catch {
    } finally {
      tab.captureRunning = false;
      tab.lastCaptureAt = Date.now();
      if (tab.captureRequested) this.queueFrameCapture(tab);
    }
  }

  activeTab() {
    return this.activeTabId ? this.tabs.get(this.activeTabId) ?? null : null;
  }

  state() {
    const active = this.activeTab();
    return {
      sessionId: this.id,
      activeTabId: this.activeTabId,
      url: active?.url ?? '',
      title: active?.title ?? '',
      navigationGeneration: active?.navigationGeneration ?? 0,
      tabs: [...this.tabs.values()].map(tab => ({
        id: tab.id,
        url: tab.url,
        title: tab.title,
        navigationGeneration: tab.navigationGeneration,
        active: tab.id === this.activeTabId,
      })),
    };
  }

  async publishState() {
    if (this.closed) return;
    const active = this.activeTab();
    if (active && !active.page.isClosed()) {
      active.url = active.page.url();
      try {
        active.title = (await active.page.title()).slice(0, 256);
      } catch {
      }
    }
    this.pushEvent({ type: 'state', state: this.state() });
  }

  pushEvent(event) {
    if (this.closed) return;
    const entry = { ...event, sequence: ++this.eventSequence };
    this.events.push(entry);
    if (this.events.length > MAX_EVENTS) this.events.splice(0, this.events.length - MAX_EVENTS);
    for (const waiter of this.eventWaiters) waiter(entry);
    this.eventWaiters.clear();
  }

  async eventsAfter(cursor) {
    this.touch();
    const immediate = this.events.filter(event => event.sequence > cursor);
    if (immediate.length > 0) {
      return { events: immediate, cursor: immediate.at(-1).sequence };
    }
    await waitForSignal(
      resolve => {
        this.eventWaiters.add(resolve);
        return () => this.eventWaiters.delete(resolve);
      },
      20_000,
    );
    const events = this.events.filter(event => event.sequence > cursor);
    return { events, cursor: events.at(-1)?.sequence ?? cursor };
  }

  async frameAfter(version) {
    this.touch();
    if (this.latestFrame && this.frameVersion > version) {
      return { buffer: this.latestFrame, version: this.frameVersion };
    }
    await waitForSignal(
      resolve => {
        this.frameWaiters.add(resolve);
        return () => this.frameWaiters.delete(resolve);
      },
      15_000,
    );
    return this.latestFrame && this.frameVersion > version
      ? { buffer: this.latestFrame, version: this.frameVersion }
      : null;
  }

  async handleProviderRequest(source, input) {
    this.touch();
    const request = normalizeRpcRequest(input);
    const tabId = this.pageToTab.get(source.page);
    const tab = tabId ? this.tabs.get(tabId) : null;
    if (
      !tab ||
      tab.id !== this.activeTabId ||
      source.frame !== source.page.mainFrame() ||
      tab.page.isClosed()
    ) {
      throw rpcFailure({
        code: 4100,
        message: 'Wallet requests are accepted only from the active top-level browser tab.',
      });
    }
    if (this.pendingRpc.size >= 16) {
      throw rpcFailure({
        code: -32005,
        message: 'Too many wallet requests are already pending.',
      });
    }

    let origin;
    try {
      origin = new URL(source.frame.url()).origin;
    } catch {
      throw rpcFailure({ code: 4100, message: 'The requesting page has no valid origin.' });
    }
    if (!origin.startsWith('https://')) {
      throw rpcFailure({ code: 4100, message: 'Wallet requests require an HTTPS origin.' });
    }

    const rpcId = crypto.randomUUID();
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pendingRpc.delete(rpcId);
        reject(rpcFailure({ code: -32000, message: 'Wallet request timed out.' }));
      }, RPC_TIMEOUT_MS);
      this.pendingRpc.set(rpcId, {
        resolve,
        reject,
        timer,
        tabId,
        navigationGeneration: tab.navigationGeneration,
        origin,
      });
      this.pushEvent({
        type: 'rpc',
        request: {
          id: rpcId,
          pageRequestId: request.id,
          tabId,
          navigationGeneration: tab.navigationGeneration,
          origin,
          method: request.method,
          params: request.params,
        },
      });
    });
  }

  resolveRpc(rpcId, payload) {
    this.touch();
    const pending = this.pendingRpc.get(rpcId);
    if (!pending) throw new Error('Wallet request not found or already completed.');
    const tab = this.tabs.get(pending.tabId);
    if (
      !tab ||
      tab.id !== this.activeTabId ||
      tab.navigationGeneration !== pending.navigationGeneration ||
      new URL(tab.url).origin !== pending.origin
    ) {
      clearTimeout(pending.timer);
      this.pendingRpc.delete(rpcId);
      pending.reject(
        rpcFailure({
          code: 4100,
          message: 'The browser context changed before the wallet request completed.',
        }),
      );
      return;
    }

    clearTimeout(pending.timer);
    this.pendingRpc.delete(rpcId);
    if (payload && typeof payload === 'object' && payload.error) {
      pending.reject(rpcFailure(payload.error));
    } else {
      pending.resolve(payload?.result);
    }
  }

  rejectPendingForTab(tabId, error) {
    for (const [rpcId, pending] of this.pendingRpc) {
      if (pending.tabId !== tabId) continue;
      clearTimeout(pending.timer);
      this.pendingRpc.delete(rpcId);
      pending.reject(rpcFailure(error));
    }
  }

  async navigate(action, inputUrl) {
    this.touch();
    const tab = this.activeTab();
    if (!tab) throw new Error('No active browser tab.');
    switch (action) {
      case 'goto': {
        const target = await normalizePublicHttpsUrl(inputUrl);
        await tab.page.goto(target.toString(), {
          waitUntil: 'domcontentloaded',
          timeout: 30_000,
        });
        break;
      }
      case 'back':
        await tab.page.goBack({ waitUntil: 'domcontentloaded', timeout: 30_000 });
        break;
      case 'forward':
        await tab.page.goForward({ waitUntil: 'domcontentloaded', timeout: 30_000 });
        break;
      case 'reload':
        await tab.page.reload({ waitUntil: 'domcontentloaded', timeout: 30_000 });
        break;
      default:
        throw new Error('Unsupported navigation action.');
    }
    await this.publishState();
    return this.state();
  }

  async createTab(inputUrl) {
    this.touch();
    const target = await normalizePublicHttpsUrl(inputUrl);
    const page = await this.context.newPage();
    const tabId = await this.attachPage(page, true);
    await page.goto(target.toString(), { waitUntil: 'domcontentloaded', timeout: 30_000 });
    await this.publishState();
    return { tabId, state: this.state() };
  }

  async closeTab(tabId) {
    this.touch();
    if (this.tabs.size <= 1) throw new Error('The last browser tab cannot be closed.');
    const tab = this.tabs.get(tabId);
    if (!tab) throw new Error('Browser tab not found.');
    await tab.page.close();
    if (this.activeTabId) await this.activateTab(this.activeTabId);
    return this.state();
  }

  async dispatchInput(input) {
    this.touch();
    const tab = this.activeTab();
    if (!tab?.cdp) throw new Error('No active browser tab.');
    const { cdp, page } = tab;

    if (input?.type === 'pointer') {
      const x = Number(input.x);
      const y = Number(input.y);
      if (!Number.isFinite(x) || !Number.isFinite(y)) throw new Error('Invalid pointer position.');
      const phase = input.phase;
      const eventType =
        phase === 'down' ? 'mousePressed' : phase === 'up' ? 'mouseReleased' : 'mouseMoved';
      await cdp.send('Input.dispatchMouseEvent', {
        type: eventType,
        x: Math.max(0, Math.min(this.width, x)),
        y: Math.max(0, Math.min(this.height, y)),
        button: phase === 'move' ? 'none' : 'left',
        buttons: phase === 'down' || (phase === 'move' && input.pressed) ? 1 : 0,
        clickCount: phase === 'down' || phase === 'up' ? 1 : 0,
      });
      if (phase === 'up') {
        const editable = await page.evaluate(
          ({ x: px, y: py }) => {
            const element = document.elementFromPoint(px, py);
            return Boolean(
              element?.closest(
                'input:not([disabled]), textarea:not([disabled]), [contenteditable="true"]',
              ),
            );
          },
          { x, y },
        ).catch(() => false);
        return { editable };
      }
      return { editable: false };
    }

    if (input?.type === 'wheel') {
      const deltaX = Number(input.deltaX ?? 0);
      const deltaY = Number(input.deltaY ?? 0);
      if (!Number.isFinite(deltaX) || !Number.isFinite(deltaY)) {
        throw new Error('Invalid wheel input.');
      }
      await cdp.send('Input.dispatchMouseEvent', {
        type: 'mouseWheel',
        x: Number(input.x ?? this.width / 2),
        y: Number(input.y ?? this.height / 2),
        deltaX: Math.max(-1_000, Math.min(1_000, deltaX)),
        deltaY: Math.max(-1_000, Math.min(1_000, deltaY)),
      });
      return { editable: false };
    }

    if (input?.type === 'text') {
      if (typeof input.text !== 'string' || input.text.length > 1_024) {
        throw new Error('Invalid text input.');
      }
      await cdp.send('Input.insertText', { text: input.text });
      return { editable: true };
    }

    if (input?.type === 'key') {
      if (typeof input.key !== 'string' || input.key.length > 32) {
        throw new Error('Invalid key input.');
      }
      const modifiers =
        (input.altKey ? 1 : 0) |
        (input.ctrlKey ? 2 : 0) |
        (input.metaKey ? 4 : 0) |
        (input.shiftKey ? 8 : 0);
      await cdp.send('Input.dispatchKeyEvent', {
        type: input.phase === 'up' ? 'keyUp' : 'keyDown',
        key: input.key,
        code: typeof input.code === 'string' ? input.code : input.key,
        modifiers,
      });
      return { editable: true };
    }

    throw new Error('Unsupported browser input.');
  }

  async emitProviderEvent(event, payload) {
    this.touch();
    if (!PROVIDER_EVENTS.has(event)) throw new Error('Unsupported provider event.');
    await Promise.all(
      [...this.tabs.values()].map(tab =>
        tab.page
          .evaluate(
            ({ event: eventName, payload: eventPayload }) => {
              globalThis.__PAXPORT_PROVIDER_EMIT__?.(eventName, eventPayload);
            },
            { event, payload },
          )
          .catch(() => undefined),
      ),
    );
  }

  async close(reason = 'closed') {
    if (this.closed) return;
    this.closed = true;
    clearInterval(this.idleTimer);
    for (const tab of this.tabs.values()) {
      clearTimeout(tab.captureTimer);
      tab.captureTimer = null;
      tab.captureRequested = false;
    }
    for (const pending of this.pendingRpc.values()) {
      clearTimeout(pending.timer);
      pending.reject(rpcFailure({ code: 4900, message: 'Browser session closed.' }));
    }
    this.pendingRpc.clear();
    for (const waiter of this.eventWaiters) waiter(null);
    for (const waiter of this.frameWaiters) waiter(null);
    this.eventWaiters.clear();
    this.frameWaiters.clear();
    await this.context?.close().catch(() => undefined);
    await this.browser?.close().catch(() => undefined);
    if (this.profileDir) {
      await fs.rm(this.profileDir, { recursive: true, force: true }).catch(() => undefined);
      this.profileDir = null;
    }
    this.closeReason = reason;
    const handlers = [...this.closeHandlers];
    this.closeHandlers.clear();
    const outcomes = await Promise.allSettled(handlers.map(handler => handler(reason)));
    const failed = outcomes.find(outcome => outcome.status === 'rejected');
    if (failed?.status === 'rejected') throw failed.reason;
  }
}
