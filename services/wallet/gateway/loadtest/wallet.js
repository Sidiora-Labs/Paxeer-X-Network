// =============================================================================
// Paxeer Wallet API — k6 load test
//
// Quick local check (no auth, just hits /healthz):
//   k6 run --vus 50 --duration 30s gateway/loadtest/wallet.js
//
// Full ramp (recommended for capacity planning):
//   k6 run gateway/loadtest/wallet.js
//
// Authenticated test (hits /v1/wallet/me to exercise JWT + db path):
//   PAXEER_JWT="$(...your supabase access token...)" \
//     k6 run --vus 100 --duration 60s gateway/loadtest/wallet.js
//
// Target a remote host:
//   API_BASE=https://wallet.example k6 run gateway/loadtest/wallet.js
//
// Install k6:  https://k6.io/docs/get-started/installation/
//   linux:     curl https://dl.k6.io/key.gpg | sudo apt-key add - && \
//              echo "deb https://dl.k6.io/deb stable main" | \
//                sudo tee /etc/apt/sources.list.d/k6.list && \
//              sudo apt update && sudo apt install k6
// =============================================================================
import http from 'k6/http';
import { check, sleep, group } from 'k6';
import { Trend, Rate } from 'k6/metrics';

const BASE = __ENV.API_BASE || 'http://localhost:8787';
const JWT = __ENV.PAXEER_JWT || ''; // optional — empty disables auth'd routes

// ---- Custom metrics so the summary is human-readable -----------------------
const healthLatency = new Trend('paxeer_healthz_latency', true);
const meLatency = new Trend('paxeer_me_latency', true);
const meErrorRate = new Rate('paxeer_me_errors');

// ---- Stage profile ---------------------------------------------------------
//
// Ramp pattern hits four interesting regimes:
//   1. warm-up           ( 30s @ 10 VU )   sanity, no contention
//   2. typical peak load ( 60s @ 100 VU )  matches ~10k DAU peak
//   3. stress            ( 60s @ 500 VU )  matches ~50k DAU peak
//   4. cooldown          ( 30s @ 10 VU )   detect leaks / GC tails
//
// Override with `--vus N --duration Ts` from the CLI to skip stages.
// ---------------------------------------------------------------------------
export const options = {
  scenarios: {
    ramp: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '30s', target: 10 },
        { duration: '60s', target: 100 },
        { duration: '60s', target: 500 },
        { duration: '30s', target: 10 },
      ],
      gracefulRampDown: '10s',
    },
  },
  thresholds: {
    // Hard pass/fail criteria — k6 exits non-zero if violated.
    http_req_failed: ['rate<0.01'],                   // <1% errors overall
    paxeer_healthz_latency: ['p(95)<50', 'p(99)<200'], // healthz is trivial
    paxeer_me_latency: ['p(95)<200', 'p(99)<500'],    // db + jwks roundtrip
    paxeer_me_errors: ['rate<0.01'],
  },
};

export default function () {
  group('healthz', () => {
    const r = http.get(`${BASE}/healthz`, { tags: { route: 'healthz' } });
    healthLatency.add(r.timings.duration);
    check(r, {
      'healthz 200': (res) => res.status === 200,
      'healthz reports ok': (res) => {
        try { return JSON.parse(res.body).ok === true; }
        catch { return false; }
      },
    });
  });

  if (JWT) {
    group('wallet/me', () => {
      const r = http.get(`${BASE}/v1/wallet/me`, {
        headers: { Authorization: `Bearer ${JWT}` },
        tags: { route: 'wallet_me' },
      });
      meLatency.add(r.timings.duration);
      meErrorRate.add(r.status !== 200);
      check(r, {
        'me 200': (res) => res.status === 200,
        'me has address': (res) => {
          try {
            const body = JSON.parse(res.body);
            return typeof body.address === 'string' && body.address.startsWith('0x');
          } catch { return false; }
        },
      });
    });
  }

  // Small think time so we don't pin a single VU to maximum RPS — gives a
  // more realistic concurrency profile than a tight loop.
  sleep(Math.random() * 0.5 + 0.1);
}

// Custom end-of-run summary written to stdout in addition to k6's default.
export function handleSummary(data) {
  const m = data.metrics;
  const line = (label, key) => {
    const t = m[key];
    if (!t) return `${label.padEnd(28)} —`;
    const v = t.values || {};
    return `${label.padEnd(28)} avg=${(v.avg ?? 0).toFixed(1)}ms  p95=${(v['p(95)'] ?? 0).toFixed(1)}ms  p99=${(v['p(99)'] ?? 0).toFixed(1)}ms`;
  };
  const rate = (label, key) => {
    const t = m[key];
    if (!t) return `${label.padEnd(28)} —`;
    return `${label.padEnd(28)} ${((t.values?.rate ?? 0) * 100).toFixed(3)}%`;
  };
  const report = [
    '',
    '─── paxeer wallet api ───────────────────────────────────────────────',
    line('GET /healthz', 'paxeer_healthz_latency'),
    line('GET /v1/wallet/me', 'paxeer_me_latency'),
    rate('  errors', 'paxeer_me_errors'),
    rate('http failures (all)', 'http_req_failed'),
    `requests           ${m.http_reqs?.values?.count ?? 0}`,
    `rps (avg)          ${(m.http_reqs?.values?.rate ?? 0).toFixed(1)}`,
    `vu peak            ${m.vus_max?.values?.value ?? 0}`,
    '─────────────────────────────────────────────────────────────────────',
    '',
  ].join('\n');
  return {
    stdout: report,
  };
}
