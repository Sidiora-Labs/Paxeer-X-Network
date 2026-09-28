import { z } from 'zod';
import { compileOrigin } from './cors.js';

export { compileOrigin };

/**
 * Environment schema. Validated once at startup; every other module imports
 * the parsed `env` object instead of touching `process.env` directly.
 */
const Env = z.object({
  NODE_ENV: z.enum(['development', 'test', 'production']).default('development'),
  PORT: z.coerce.number().int().positive().default(8787),
  LOG_LEVEL: z.enum(['fatal', 'error', 'warn', 'info', 'debug', 'trace']).default('info'),
  CORS_ORIGINS: z
    .string()
    .min(1, 'CORS_ORIGINS is required')
    .transform((s) =>
      s
        .split(',')
        .map((o) => o.trim())
        .filter(Boolean)
        .map(compileOrigin),
    ),

  // Supabase is used ONLY for JWT issuance / user identity. The API verifies
  // tokens locally using Supabase's published JWKS at
  // ${SUPABASE_URL}/auth/v1/.well-known/jwks.json — fetched once at boot and
  // cached in memory. No JWT secret or service-role key is needed.
  // If Supabase is down, already-issued tokens keep working until they expire
  // (~1h). Cached JWKS keeps signature verification working through outages.
  SUPABASE_URL: z.string().url(),

  // Postgres we own. Wallet rows and signature audit live here.
  // Connection string with credentials, supplied only through the environment.
  DATABASE_URL: z.string().url(),
  DATABASE_POOL_MAX: z.coerce.number().int().positive().default(20),

  WALLET_MASTER_KEY: z
    .string()
    .min(1, 'WALLET_MASTER_KEY is required')
    .refine((v) => {
      try {
        const buf = Buffer.from(v, 'base64');
        return buf.byteLength === 32;
      } catch {
        return false;
      }
    }, 'WALLET_MASTER_KEY must decode to exactly 32 bytes (base64). Generate with: node -e "console.log(require(\'crypto\').randomBytes(32).toString(\'base64\'))"'),
  WALLET_MASTER_KEY_VERSION: z.coerce.number().int().positive().default(1),

  HYPERPAXEER_CHAIN_ID: z.coerce.number().int().positive().default(125),
  HYPERPAXEER_RPC_URL: z.string().url(),
  HYPERPAXEER_EXPLORER_URL: z.string().url().optional(),

  POLICY_MAX_TX_VALUE_WEI: z.coerce.bigint().nonnegative().default(1_000_000_000_000_000_000n),
  POLICY_MAX_DAILY_VALUE_WEI: z.coerce
    .bigint()
    .nonnegative()
    .default(10_000_000_000_000_000_000n),
  POLICY_RATE_LIMIT_PER_MINUTE: z.coerce.number().int().positive().default(60),

  // -- Funded accounts (prop-firm tier) -------------------------------------
  //
  // The treasury wallet pre-funds new funded accounts with USDL collateral
  // and PAX gas. It also auto-tops-up PAX when a funded wallet drops below
  // FUNDED_GAS_REFILL_THRESHOLD_WEI. Its private key never appears in DB —
  // it lives only here, in process memory, behind the same module boundary
  // as the per-user master key.
  //
  // For prod, this MUST come from a secrets manager. For local dev, dropping
  // it into .env is acceptable.
  FUNDED_TREASURY_PRIVATE_KEY: z
    .string()
    .regex(/^0x[0-9a-fA-F]{64}$/,
      'FUNDED_TREASURY_PRIVATE_KEY must be a 0x-prefixed 32-byte hex string')
    .optional(),

  // USDL token (ERC-20, 6 decimals) on chain 125.
  FUNDED_USDL_ADDRESS: z
    .string()
    .regex(/^0x[0-9a-fA-F]{40}$/, 'FUNDED_USDL_ADDRESS must be a 20-byte hex address')
    .default('0x7c69c84daAEe90B21eeCABDb8f0387897E9B7B37'),
  FUNDED_USDL_DECIMALS: z.coerce.number().int().nonnegative().default(6),

  // Auto top-up: if a funded wallet's PAX balance drops below this threshold
  // before a sign/send request, the treasury sends FUNDED_GAS_REFILL_AMOUNT_WEI
  // PAX to it (and we wait for the receipt before signing the user's tx).
  // Defaults: top up when below 2 PAX, send 5 PAX at a time.
  FUNDED_GAS_REFILL_THRESHOLD_WEI: z.coerce
    .bigint()
    .nonnegative()
    .default(2_000_000_000_000_000_000n),
  FUNDED_GAS_REFILL_AMOUNT_WEI: z.coerce
    .bigint()
    .nonnegative()
    .default(5_000_000_000_000_000_000n),

  // Drawdown evaluator tick interval (ms). Lower = faster breach detection
  // and tighter scale/payout reaction, higher = less RPC + DB load.
  FUNDED_EVALUATOR_INTERVAL_MS: z.coerce.number().int().positive().default(10_000),

  // -- Paxscan (Blockscout-based explorer) integration ----------------------
  //
  // The funded-account evaluator values wallets via Paxscan rather than direct
  // RPC reads. Paxscan already indexes every ERC-20 + native balance with live
  // USD exchange_rate per token, so a single HTTP call returns the full USD
  // equity of a wallet across every spot asset it holds — including SID,
  // USDC, vault shares, anything indexed.
  //
  // The previous USDL-only evaluator wrongly breached any user who swapped
  // their starting USDL for any other token (USDL → 0 read as $0 equity).
  // Paxscan-based valuation handles all spot trading transparently. Open
  // perp positions on the Diamond are NOT reflected here yet — that is a
  // Stage-2 adapter in jobs/fundedEvaluator.ts.
  PAXSCAN_API_URL: z
    .string()
    .url()
    .default('https://api.paxscan.io/api/v2'),
  // HTTP timeout per Paxscan request. Conservative — Paxscan typically
  // responds in <100ms; 5s is to absorb transient indexer-side stalls.
  PAXSCAN_TIMEOUT_MS: z.coerce.number().int().positive().default(5_000),
  // In-process TTL cache for per-wallet Paxscan results. Evaluator ticks every
  // ~10s and may evaluate the same wallet from multiple workers; a 4s cache
  // collapses redundant requests without making valuations meaningfully stale.
  PAXSCAN_CACHE_TTL_MS: z.coerce.number().int().nonnegative().default(4_000),

  // RPC quorum for on-chain balance reads. HYPERPAXEER_RPC_URL is a global
  // load balancer fronting ~100 validator-aligned nodes; cross-region nodes
  // can lag by hundreds of ms. A single balance call can land on a behind
  // node and return the pre-funding state (0 USDL), causing the evaluator
  // to compute equity=$0 and falsely breach a freshly-provisioned account.
  //
  // We fix this at the read layer by sampling N nodes in parallel and
  // taking the MAX value. Behind nodes can only be BEHIND, never ahead —
  // so the highest balance any sampled node reports is the truest, freshest
  // state visible somewhere in the cluster. With N=3 the probability of all
  // three samples hitting behind nodes is vanishingly small for any normal
  // cluster lag distribution.
  //
  // Cost: 3x RPC calls per balance read. Acceptable because the operator owns the
  // nodes; bump to 5–7 in extreme conditions, 1 in dev against a single
  // local node.
  FUNDED_BALANCE_READ_QUORUM: z.coerce.number().int().min(1).max(15).default(3),

  // UTC hour at which the daily drawdown window resets (00 = midnight UTC).
  FUNDED_DAILY_RESET_UTC_HOUR: z.coerce.number().int().min(0).max(23).default(0),

  // Number of worker processes to fork via node:cluster. Default 1 = single
  // process (matches dev/test). In production set to ~(vCPU - 2) to leave
  // headroom for Postgres, Caddy, mail. The 18-vCPU prod box runs 12.
  // Each worker is independent: shared-nothing memory, its own DB pool,
  // its own JWKS cache, its own in-process wallet lock. The OS round-robins
  // accepted TCP connections across workers automatically.
  API_WORKERS: z.coerce.number().int().positive().max(64).default(1),

  // -- Agent-native lane ----------------------------------------------------
  //
  // The agent lane lets a Matrix agent authenticate with its own ed25519 DID
  // key (challenge/verify) instead of borrowing a human's Supabase JWT, and
  // operate a dedicated kind='agent' wallet under an owner-controlled policy.
  //
  // AGENT_JWT_SECRET signs the short-lived agent_token (HS256) the API mints
  // after a successful DID verify. The API both mints AND verifies it, so a
  // symmetric secret is sufficient and self-contained. If unset, the agent
  // auth routes return 503 (lane disabled) — they never crash boot. Mirrors
  // the FUNDED_TREASURY_PRIVATE_KEY optional-feature posture.
  AGENT_JWT_SECRET: z
    .string()
    .min(32, 'AGENT_JWT_SECRET must be >=32 chars. Generate with: node -e "console.log(require(\'crypto\').randomBytes(48).toString(\'base64url\'))"')
    .optional(),
  // agent_token lifetime (seconds). Short by design — the agent re-auths via
  // challenge/verify on expiry. Default 15 min.
  AGENT_TOKEN_TTL_SECONDS: z.coerce.number().int().positive().max(86_400).default(900),
  // Auth challenge nonce lifetime (seconds). The window between /challenge and
  // /verify. Short to bound replay surface. Default 2 min.
  AGENT_CHALLENGE_TTL_SECONDS: z.coerce.number().int().positive().max(3_600).default(120),

  // Safe-by-default: a newly-seen agent principal is created FROZEN and in
  // read_only mode until its owner raises the leash via the control plane.
  // Set AGENT_DEFAULT_FROZEN=false only in trusted single-tenant dev.
  AGENT_DEFAULT_FROZEN: z
    .enum(['true', 'false'])
    .default('true')
    .transform((s) => s === 'true'),
  AGENT_DEFAULT_MODE: z.enum(['read_only', 'trade_only', 'full']).default('read_only'),

  // When true, an agent DID whose label segment (did:matrix:<label>:<fp>) is a
  // UUID is auto-bound to the owner whose Supabase user_id == that label. This
  // is the ownership binding the control plane pivots on. Disable to require an
  // explicit owner claim for every agent.
  AGENT_BIND_OWNER_FROM_DID: z
    .enum(['true', 'false'])
    .default('true')
    .transform((s) => s === 'true'),

  // Default per-agent caps used when a policy row leaves a field NULL. These
  // are the SAME knobs as the standard POLICY_* caps so behaviour is familiar;
  // an owner can override any of them per-agent through the control plane.
  AGENT_DEFAULT_MAX_TX_VALUE_WEI: z.coerce.bigint().nonnegative().default(0n),
  AGENT_DEFAULT_MAX_DAILY_VALUE_WEI: z.coerce.bigint().nonnegative().default(0n),
  AGENT_DEFAULT_RATE_LIMIT_PER_MINUTE: z.coerce.number().int().positive().default(30),
  // Cap on ERC-20 approve() amounts (wei). 0 = approvals disabled by default
  // (the safest posture: an owner must opt an agent into approvals). Set high
  // to allow, or leave 0 to force exact-amount approvals only via policy.
  AGENT_DEFAULT_MAX_APPROVE_WEI: z.coerce.bigint().nonnegative().default(0n),

  // -- Agent durable actions (high-level intent orchestrator) ---------------
  //
  // The agent-actions lane turns a one-shot signing request into a durable,
  // server-driven, idempotent operation: one POST (e.g. layerx/deposit) whose
  // approve → confirm → deposit → confirm → verify sequence is owned by the
  // wallet, survives a client disconnect, and is polled via GET
  // /v1/agent/actions/:id. Every knob below is safe to leave at its default.

  // LayerX custody vault (depositUSDL target + approve spender). Required only
  // for the /v1/agent/actions/layerx/deposit route; unset → that route 503s.
  LAYERX_VAULT_ADDRESS: z
    .string()
    .regex(/^0x[0-9a-fA-F]{40}$/, 'LAYERX_VAULT_ADDRESS must be a 20-byte hex address')
    .optional(),

  // Read-only connection to the LayerX sequencer Postgres (whitelisted). Used
  // to (a) authoritatively verify a deposit credited (deposits.deposit_tx =
  // our on-chain call hash) and (b) mirror per-DID escrow/balance into the
  // wallet DB. Unset → credit verification degrades to on-chain-only and the
  // mirror sync worker stays disabled. NEVER logged.
  LAYER_X_DB_URI: z.string().min(1).optional(),

  // Block confirmations before an approval or call tx is treated as final.
  // Paxeer has instant CometBFT finality (no reorgs) so 1 is correct; kept
  // configurable for safety on other chains.
  ACTION_CONFIRMATIONS: z.coerce.number().int().nonnegative().default(1),
  // Max time (ms) the orchestrator waits for a single tx receipt before
  // parking the action in `reconciling` for the reconciler to pick up.
  ACTION_RECEIPT_TIMEOUT_MS: z.coerce.number().int().positive().default(90_000),
  // How often the background worker advances in-flight actions (ms).
  ACTION_WORKER_INTERVAL_MS: z.coerce.number().int().positive().default(3_000),
  // How often the boot reconciler re-sweeps for stalled non-terminal actions
  // (ms). Covers actions orphaned by a crash mid-flight.
  ACTION_RECONCILE_INTERVAL_MS: z.coerce.number().int().positive().default(30_000),
  // Deterministic fee-bump (percent) applied when REPLACING a provably-pending
  // stuck tx at a wedged nonce. 12% clears most min-bump rules in one step.
  ACTION_FEE_BUMP_PERCENT: z.coerce.number().int().min(10).max(100).default(12),
  // How often the LayerX mirror-sync worker pulls accounts/claims (ms). Only
  // runs when LAYER_X_DB_URI is set.
  LAYERX_SYNC_INTERVAL_MS: z.coerce.number().int().positive().default(120_000),

  ATTESTOR_ENDPOINTS: z
    .string()
    .optional()
    .transform((s) =>
      (s ?? '')
        .split(',')
        .map((u) => u.trim())
        .filter(Boolean),
    )
    .refine(
      (urls) => urls.every((u) => /^https:\/\/[^\s/]+/.test(u)),
      'ATTESTOR_ENDPOINTS must be a comma-separated list of https URLs',
    ),
  ATTESTOR_CLIENT_CERT_FILE: z.string().min(1).optional(),
  ATTESTOR_CLIENT_KEY_FILE: z.string().min(1).optional(),
  ATTESTOR_CA_FILE: z.string().min(1).optional(),
  ATTESTOR_QUORUM: z.coerce.number().int().min(1).max(64).default(3),
  ATTESTOR_HEALTH_INTERVAL_MS: z.coerce.number().int().positive().default(5_000),
  ATTESTOR_TIMEOUT_MS: z.coerce.number().int().positive().default(15_000),

  RPC_URLS: z
    .string()
    .default(
      Array.from({ length: 16 }, (_, i) => `https://api${i + 1}.mainnet-beta.paxeer.network`).join(','),
    )
    .transform((s) =>
      s
        .split(',')
        .map((u) => u.trim())
        .filter(Boolean),
    )
    .refine(
      (urls) => urls.length > 0 && urls.every((u) => /^https?:\/\/[^\s/]+/.test(u)),
      'RPC_URLS must be a non-empty comma-separated list of http(s) URLs',
    ),
  RPC_LAG_THRESHOLD_BLOCKS: z.coerce.number().int().nonnegative().default(20),
  RPC_HEALTH_INTERVAL_MS: z.coerce.number().int().positive().default(5_000),
  RPC_TIMEOUT_MS: z.coerce.number().int().positive().default(8_000),

  RATE_LIMIT_CLIENT_PER_MINUTE: z.coerce.number().int().positive().default(120),
  RATE_LIMIT_ACCOUNT_PER_MINUTE: z.coerce.number().int().positive().default(60),
}).superRefine((v, ctx) => {
  if (v.ATTESTOR_ENDPOINTS.length === 0) return;
  for (const key of ['ATTESTOR_CLIENT_CERT_FILE', 'ATTESTOR_CLIENT_KEY_FILE', 'ATTESTOR_CA_FILE'] as const) {
    if (!v[key]) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        path: [key],
        message: `${key} is required when ATTESTOR_ENDPOINTS is set`,
      });
    }
  }
  if (v.ATTESTOR_ENDPOINTS.length < v.ATTESTOR_QUORUM) {
    ctx.addIssue({
      code: z.ZodIssueCode.custom,
      path: ['ATTESTOR_ENDPOINTS'],
      message: 'ATTESTOR_ENDPOINTS must list at least ATTESTOR_QUORUM endpoints',
    });
  }
});

export type Env = z.infer<typeof Env>;

let parsed: Env | null = null;

export function loadEnv(): Env {
  if (parsed) return parsed;
  const result = Env.safeParse(process.env);
  if (!result.success) {
    const issues = result.error.issues
      .map((i) => `  - ${i.path.join('.')}: ${i.message}`)
      .join('\n');
    // eslint-disable-next-line no-console
    console.error(`\n[env] Invalid environment configuration:\n${issues}\n`);
    process.exit(1);
  }
  parsed = result.data;
  return parsed;
}

export const env = loadEnv();
