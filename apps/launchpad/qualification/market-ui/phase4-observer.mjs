export function installMarketObserver(policy) {
  if (window.__kindleMarketCapture) throw new Error('Market observer already installed');
  const queue = [], prices = new Map(), freshness = new Map(), anchors = new Map();
  let stopped = false, draining = false, failed = null;
  let clsWindow = 0, clsMaximum = 0, clsWindowStart = null, clsLastShift = null;
  const now = () => ({ monotonic_ms: performance.now(), observed_utc_ms: performance.timeOrigin + performance.now(), pathname: location.pathname });
  const add = (kind, data) => {
    if (stopped || failed) return;
    if (queue.length >= policy.budgets.observer_queue_records) { failed = 'Actual observer queue exhausted'; return; }
    queue.push({ kind, ...now(), ...data });
  };
  const drain = async () => {
    if (draining || stopped) return;
    draining = true;
    try {
      if (failed) { await window.__kindleMarketWrite([{ kind: 'observer_failure', ...now(), reason: failed }]); stopped = true; return; }
      while (queue.length) await window.__kindleMarketWrite(queue.splice(0, 512));
    } catch (error) { failed = String(error); }
    finally { draining = false; }
  };
  const sample = () => {
    const lists = [...document.querySelectorAll('main table, [data-market-list] table, main ul, main ol')].map((element, index) => ({ index, label: element.closest('[aria-label]')?.getAttribute('aria-label') ?? element.tagName, rows: element.tagName === 'TABLE' ? element.querySelectorAll('tbody tr').length : element.children.length, mounted_data_rows: element.tagName === 'TABLE' ? element.querySelectorAll('tbody tr:not([aria-hidden])').length : element.children.length }));
    const sessions = [...document.querySelectorAll('[data-market-pool]')].map(element => ({ pool: element.dataset.marketPool, stream: element.dataset.marketStream, freshness: element.dataset.marketFreshness, generated_at: element.dataset.marketGeneratedAt, fill_sequence: element.dataset.marketFillSequence, fill_revision: element.dataset.marketFillRevision, fill_cache: Number(element.dataset.marketFillCacheSize), pending_fills: Number(element.dataset.marketPendingFillSize), inactive_pools: Number(element.dataset.marketInactivePoolCount), listeners: Number(element.dataset.marketListenerCount), topic_heads: Number(element.dataset.marketTopicCount) }));
    add('dom', { lists, sessions, cls: clsMaximum, viewport: { width: innerWidth, height: innerHeight, dpr: devicePixelRatio, scroll_x: scrollX, scroll_y: scrollY, document_width: document.documentElement.scrollWidth }, visibility: document.visibilityState, reduced_motion: matchMedia('(prefers-reduced-motion: reduce)').matches });
  };
  const changes = new MutationObserver(records => {
    const candidates = new Set();
    for (const record of records) {
      if (record.type === 'attributes' && record.target instanceof Element) candidates.add(record.target);
      if (record.type === 'childList') for (const added of record.addedNodes) if (added instanceof Element) { if (added.matches('[data-market-price-topic],[data-market-pool]')) candidates.add(added); for (const element of added.querySelectorAll('[data-market-price-topic],[data-market-pool]')) candidates.add(element); }
    }
    for (const element of candidates) {
      if (element.matches('[data-market-price-topic]')) {
        const topic = element.dataset.marketPriceTopic, sequence = element.dataset.marketPriceSequence, revision = element.dataset.marketPriceRevision;
        if (!sequence || prices.get(topic) === sequence) continue;
        const initial = !prices.has(topic); prices.set(topic, sequence);
        add('price_commit', { topic, sequence, revision, initial, stream: element.closest('[data-market-pool]')?.dataset.marketStream ?? null, text: element.textContent });
      }
      if (element.matches('[data-market-pool]')) {
        const pool = element.dataset.marketPool, value = `${element.dataset.marketStream}:${element.dataset.marketFreshness}`;
        if (freshness.get(pool) !== value) { freshness.set(pool, value); add('freshness', { pool, stream: element.dataset.marketStream, freshness: element.dataset.marketFreshness, generated_at: element.dataset.marketGeneratedAt, fill_sequence: element.dataset.marketFillSequence }); }
      }
    }
    if (records.some(record => record.type === 'childList')) sample();
  });
  changes.observe(document, { subtree: true, childList: true, attributes: true, attributeFilter: ['data-market-price-sequence', 'data-market-price-revision', 'data-market-stream', 'data-market-freshness'] });
  const observers = [];
  for (const type of ['longtask', 'layout-shift', 'event']) {
    if (!PerformanceObserver.supportedEntryTypes.includes(type)) throw new Error(`Actual browser lacks ${type} observation`);
    const observer = new PerformanceObserver(list => {
      for (const entry of list.getEntries()) {
        if (type === 'event') { if (entry.interactionId > 0) add('event_timing', { name: entry.name, start_ms: entry.startTime, processing_start_ms: entry.processingStart, processing_end_ms: entry.processingEnd, duration_ms: entry.duration, interaction_id: entry.interactionId }); }
        else if (type === 'layout-shift') {
          if (!entry.hadRecentInput) {
            if (clsLastShift !== null && entry.startTime - clsLastShift < 1000 && entry.startTime - clsWindowStart < 5000) clsWindow += entry.value;
            else { clsWindow = entry.value; clsWindowStart = entry.startTime; }
            clsLastShift = entry.startTime; clsMaximum = Math.max(clsMaximum, clsWindow);
          }
          add('layout_shift', { start_ms: entry.startTime, value: entry.value, had_recent_input: entry.hadRecentInput, session_window_maximum: clsMaximum });
        }
        else add('longtask', { start_ms: entry.startTime, duration_ms: entry.duration });
      }
    });
    observer.observe(type === 'event' ? { type, buffered: true, durationThreshold: 16 } : { type, buffered: true }); observers.push(observer);
  }
  const click = event => {
    const control = event.target instanceof Element ? event.target.closest('button,a,input,select,[role="button"],[role="tab"]') : null;
    if (!control) return;
    const started = performance.now(), trusted = event.isTrusted;
    requestAnimationFrame(() => requestAnimationFrame(() => add('local_interaction', { duration_ms: performance.now() - started, trusted, control: (control.getAttribute('aria-label') ?? control.textContent ?? '').trim().slice(0, 256), terminal: location.pathname === '/terminal' })));
  };
  document.addEventListener('click', click, true);
  const visibility = () => add('visibility', { state: document.visibilityState });
  document.addEventListener('visibilitychange', visibility);
  const sampleTimer = setInterval(sample, 1000), drainTimer = setInterval(() => { void drain(); }, 250);
  window.__kindleMarketCapture = {
    sample,
    cls: () => clsMaximum,
    anchor(id, selector) {
      const element = document.querySelector(selector);
      if (!(element instanceof HTMLElement)) throw new Error('Actual scroll element missing');
      const rows = [...element.querySelectorAll('[data-market-trade-key]')];
      const box = element.getBoundingClientRect(), row = rows.find(item => item.getBoundingClientRect().bottom > box.top);
      const value = { scroll_top: element.scrollTop, identity: row?.getAttribute('data-market-trade-key') ?? null, offset: row ? row.getBoundingClientRect().top - box.top : null };
      anchors.set(id, value); add('scroll_anchor', { id, ...value }); return value;
    },
    async flush() { sample(); await drain(); if (failed) throw new Error(failed); },
    async stop() { await this.flush(); stopped = true; changes.disconnect(); for (const observer of observers) observer.disconnect(); clearInterval(sampleTimer); clearInterval(drainTimer); document.removeEventListener('click', click, true); document.removeEventListener('visibilitychange', visibility); delete window.__kindleMarketCapture; }
  };
}

export function percentile(values, fraction) {
  if (!Array.isArray(values) || values.length === 0 || !(fraction > 0 && fraction <= 1) || values.some(value => !Number.isFinite(value) || value < 0)) throw new Error('Actual bounded measurement samples required');
  return [...values].sort((a, b) => a - b)[Math.ceil(values.length * fraction) - 1];
}
