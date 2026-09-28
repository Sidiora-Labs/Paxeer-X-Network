import type { FastifyInstance, FastifyRequest, FastifyReply } from 'fastify';
import { createHash } from 'node:crypto';
import { createWalletClient, http, type Hex, type TransactionSerializable } from 'viem';
import { requireAuth } from '../middleware/auth.js';
import {
  findWalletByUserId,
  getSigningAccountForRow,
  logSignature,
  provisionWalletForUser,
  type WalletRow,
} from '../db/wallets.js';
import {
  findFundedAccountByWalletId,
  findTier,
  insertFundedAccount,
  listActiveTiers,
  listWhitelist,
  recordFundingTxHashes,
  type FundedAccountRow,
} from '../db/fundedAccounts.js';
import { evaluateFunded, type FundedDecision } from '../policy/funded.js';
import { evaluate as evaluateStandardPolicy } from '../policy/index.js';
import { hyperPaxeer } from '../chain.js';
import { env } from '../env.js';
import { withWalletLock } from '../util/walletLock.js';
import {
  disburseTier,
  TreasuryInsufficientFundsError,
  TreasuryUnavailableError,
} from '../treasury/disburse.js';
import { ensureGas, GasRefillFailedError } from '../treasury/gasRefill.js';
import {
  readPaxBalance,
  readUsdlBalance,
} from '../treasury/index.js';
import {
  SignTxBody,
  SendTxBody,
  SignMessageBody,
  ProvisionFundedBody,
  type TxRequest,
} from '../schemas/tx.js';

/**
 * Funded-account routes.
 *
 * Endpoint surface mirrors the standard `/v1/wallet/*` routes but uses the
 * funded-kind wallet and threads every signing call through `evaluateFunded`
 * BEFORE the standard policy. Gas auto top-up runs on every sign + send.
 */

// Body schemas are defined in `src/schemas/tx.ts` so both /v1/wallet/* and
// /v1/funded/* share the same hardened validators (length caps, strict
// envelopes, charsets). Update there to update both routes simultaneously.

// -----------------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------------

function hashRequest(payload: unknown): string {
  return createHash('sha256').update(JSON.stringify(payload)).digest('hex');
}

function clientIp(
  headers: Record<string, string | string[] | undefined>,
  fallback: string | null,
): string | null {
  const xff = headers['x-forwarded-for'];
  if (typeof xff === 'string' && xff.length > 0) {
    return xff.split(',')[0]?.trim() ?? fallback;
  }
  return fallback;
}

function buildTxParams(tx: TxRequest): {
  to?: `0x${string}`;
  value?: bigint;
  data?: `0x${string}`;
  gas?: bigint;
  maxFeePerGas?: bigint;
  maxPriorityFeePerGas?: bigint;
  nonce?: number;
} {
  const out: ReturnType<typeof buildTxParams> = {};
  if (tx.to) out.to = tx.to as `0x${string}`;
  if (tx.value !== undefined) out.value = BigInt(tx.value);
  if (tx.data) out.data = tx.data as `0x${string}`;
  if (tx.gas !== undefined) out.gas = BigInt(tx.gas);
  if (tx.maxFeePerGas !== undefined) out.maxFeePerGas = BigInt(tx.maxFeePerGas);
  if (tx.maxPriorityFeePerGas !== undefined)
    out.maxPriorityFeePerGas = BigInt(tx.maxPriorityFeePerGas);
  if (tx.nonce !== undefined) out.nonce = tx.nonce;
  return out;
}

/**
 * Load the user's funded wallet + funded_account row in one place. Returns a
 * shaped 4xx if either is missing.
 */
async function loadFundedContext(
  req: FastifyRequest,
  reply: FastifyReply,
): Promise<{ wallet: WalletRow; account: FundedAccountRow } | null> {
  const userId = req.user!.id;
  const wallet = await findWalletByUserId(userId, 'funded');
  if (!wallet) {
    void reply
      .code(404)
      .send({ error: 'no_funded_account', message: 'funded wallet not provisioned' });
    return null;
  }
  const account = await findFundedAccountByWalletId(wallet.id);
  if (!account) {
    // Theoretically impossible if provisioning is atomic, but treat defensively.
    void reply.code(409).send({
      error: 'wallet_orphaned',
      message: 'funded wallet exists without an associated funded_account row',
    });
    return null;
  }
  return { wallet, account };
}

/** Turn a FundedDecision deny into a 403 with the structured shape we promised. */
function sendFundedDeny(reply: FastifyReply, d: Extract<FundedDecision, { allow: false }>): void {
  void reply.code(403).send({
    error: d.code,
    message: d.message,
    ...(d.contract ? { contract: d.contract } : {}),
    ...(d.selector ? { selector: d.selector } : {}),
    ...(d.spender ? { spender: d.spender } : {}),
    ...(d.status ? { status: d.status } : {}),
  });
}

// -----------------------------------------------------------------------------
// Routes
// -----------------------------------------------------------------------------

export async function fundedRoutes(app: FastifyInstance): Promise<void> {
  /**
   * GET /v1/funded/tiers
   * Public — list active tiers and their full whitelists. The UI consumes
   * this before the user clicks "Create Funded Account" so we can show
   * tier params + allowed protocols.
   */
  app.get('/v1/funded/tiers', async (_req, reply) => {
    const tiers = await listActiveTiers();
    const enriched = await Promise.all(
      tiers.map(async (t) => ({
        tier_id: t.tier_id,
        label: t.label,
        initial_usdl_units: t.initial_usdl_units.toString(),
        initial_usdl_decimals: env.FUNDED_USDL_DECIMALS,
        initial_pax_wei: t.initial_pax_wei.toString(),
        max_daily_dd_bps: t.max_daily_dd_bps,
        max_total_dd_bps: t.max_total_dd_bps,
        scale_threshold_usd: t.scale_threshold_usd,
        payout_threshold_usd: t.payout_threshold_usd,
        capital_fee_bps: t.capital_fee_bps,
        whitelist: await listWhitelist(t.tier_id),
      })),
    );
    return reply.send({ tiers: enriched });
  });

  /**
   * POST /v1/funded/provision
   * Create a fresh funded wallet for the authenticated user, then disburse
   * initial USDL + PAX from the treasury. Idempotent: if the user already has
   * a funded account, returns the existing one without re-funding.
   */
  app.post('/v1/funded/provision', { preHandler: requireAuth }, async (req, reply) => {
    const parsed = ProvisionFundedBody.safeParse(req.body ?? {});
    if (!parsed.success) {
      return reply.code(400).send({ error: 'invalid_body', issues: parsed.error.issues });
    }
    const userId = req.user!.id;
    const tierId = parsed.data.tier_id;

    const tier = await findTier(tierId);
    if (!tier || !tier.is_active) {
      return reply.code(400).send({
        error: 'unknown_tier',
        message: `tier ${tierId} is not registered or not active`,
      });
    }

    // 1) Provision the funded-kind wallet. Idempotent — returns existing on retry.
    let walletRow: WalletRow;
    try {
      const provisioned = await provisionWalletForUser(userId, 'funded');
      walletRow = provisioned.row;
    } catch (err) {
      req.log.error({ err, userId }, 'funded wallet provision failed');
      return reply.code(500).send({
        error: 'wallet_provision_failed',
        detail: (err as Error).message,
      });
    }

    // 2) If a funded_account already exists for this wallet, this is a retry.
    //    Return the existing record; don't re-fund.
    const existingAccount = await findFundedAccountByWalletId(walletRow.id);
    if (existingAccount) {
      return reply.send({
        wallet: publicWallet(walletRow),
        funded_account: shapeAccount(existingAccount),
        funding: {
          status: 'already_funded',
          tx_hashes: existingAccount.funding_tx_hashes,
        },
      });
    }

    // 3) Create the funded_account row with starting equity = initial USDL value.
    //    25K USDL = $25,000 (1:1). Stored at 6dp matching USDL's precision.
    const startingUsd = unitsToDecimalString(tier.initial_usdl_units, env.FUNDED_USDL_DECIMALS);
    let account: FundedAccountRow;
    try {
      account = await insertFundedAccount({
        wallet_id: walletRow.id,
        tier_id: tier.tier_id,
        starting_value_usd: startingUsd,
      });
    } catch (err) {
      req.log.error({ err, userId, walletId: walletRow.id }, 'funded_account insert failed');
      return reply.code(500).send({
        error: 'funded_account_insert_failed',
        detail: (err as Error).message,
      });
    }

    // 4) Disburse initial USDL + PAX from the treasury. We hold the route open
    //    while the two on-chain transfers settle so the UI can show the funded
    //    balance immediately after provision returns.
    try {
      const disbursement = await disburseTier({
        tierId: tier.tier_id,
        recipient: walletRow.address,
      });
      await recordFundingTxHashes(account.id, {
        usdl: disbursement.usdl_tx_hash,
        pax: disbursement.pax_tx_hash,
      });
      return reply.send({
        wallet: publicWallet(walletRow),
        funded_account: shapeAccount({
          ...account,
          funding_tx_hashes: {
            usdl: disbursement.usdl_tx_hash,
            pax: disbursement.pax_tx_hash,
          },
        }),
        funding: { status: 'funded', tx_hashes: disbursement },
      });
    } catch (err) {
      if (err instanceof TreasuryUnavailableError) {
        req.log.error({ err }, 'treasury unavailable for funded provision');
        return reply.code(503).send({
          error: 'treasury_unavailable',
          message: err.message,
          partial: { wallet_address: walletRow.address, funded_account_id: account.id },
        });
      }
      if (err instanceof TreasuryInsufficientFundsError) {
        req.log.error(
          { err, asset: err.asset, required: err.required.toString(), available: err.available.toString() },
          'treasury insufficient funds',
        );
        return reply.code(503).send({
          error: 'treasury_insufficient_funds',
          asset: err.asset,
          message: err.message,
          partial: { wallet_address: walletRow.address, funded_account_id: account.id },
        });
      }
      req.log.error({ err, userId }, 'funded disbursement failed');
      return reply.code(500).send({
        error: 'funded_disbursement_failed',
        detail: (err as Error).message,
        partial: { wallet_address: walletRow.address, funded_account_id: account.id },
      });
    }
  });

  /**
   * GET /v1/funded/me
   * Returns the user's funded account state — balances (live from chain),
   * tier params, status, drawdown headroom, and the full whitelist so the UI
   * can render the "Allowed Protocols" panel.
   */
  app.get('/v1/funded/me', { preHandler: requireAuth }, async (req, reply) => {
    const loaded = await loadFundedContext(req, reply);
    if (!loaded) return;
    const { wallet, account } = loaded;

    const [tier, whitelist, paxWei, usdlUnits] = await Promise.all([
      findTier(account.tier_id),
      listWhitelist(account.tier_id),
      // Live balances — funded users see the same numbers their UI shows.
      readPaxBalance(wallet.address).catch(() => null),
      readUsdlBalance(wallet.address).catch(() => null),
    ]);

    return reply.send({
      wallet: publicWallet(wallet),
      funded_account: shapeAccount(account),
      tier: tier
        ? {
            tier_id: tier.tier_id,
            label: tier.label,
            max_daily_dd_bps: tier.max_daily_dd_bps,
            max_total_dd_bps: tier.max_total_dd_bps,
            scale_threshold_usd: tier.scale_threshold_usd,
            payout_threshold_usd: tier.payout_threshold_usd,
            capital_fee_bps: tier.capital_fee_bps,
          }
        : null,
      balances: {
        pax_wei: paxWei?.toString() ?? null,
        usdl_units: usdlUnits?.toString() ?? null,
        usdl_decimals: env.FUNDED_USDL_DECIMALS,
      },
      whitelist,
    });
  });

  /**
   * POST /v1/funded/sign
   * Sign-only (no broadcast). Runs funded policy → standard policy → gas
   * top-up → sign. Returns the signed tx hex for the SDK to broadcast.
   */
  app.post('/v1/funded/sign', { preHandler: requireAuth }, async (req, reply) => {
    const parsed = SignTxBody.safeParse(req.body);
    if (!parsed.success) {
      return reply.code(400).send({ error: 'invalid_body', issues: parsed.error.issues });
    }
    const userId = req.user!.id;
    const { tx } = parsed.data;
    const valueWei = tx.value ? BigInt(tx.value) : 0n;

    const loaded = await loadFundedContext(req, reply);
    if (!loaded) return;
    const { wallet, account } = loaded;

    // Funded policy — strictest gate, evaluated first.
    const funded = await evaluateFunded({
      account,
      tx: { to: tx.to, value: valueWei, data: tx.data },
    });
    if (!funded.allow) return sendFundedDeny(reply, funded);

    // Standard policy (rate limit, tx-value cap, daily cap).
    const standard = await evaluateStandardPolicy({ user_id: userId, value_wei: valueWei });
    if (!standard.allow) {
      return reply.code(403).send({ error: standard.code, message: standard.message });
    }

    // Gas pre-check so the user's signed tx will actually be broadcastable.
    try {
      await ensureGas(wallet.address);
    } catch (err) {
      if (err instanceof GasRefillFailedError) {
        req.log.error({ err }, 'gas refill failed before sign');
        return reply.code(503).send({ error: 'gas_refill_failed', message: err.message });
      }
      throw err;
    }

    let signed: Hex;
    try {
      const signer = await getSigningAccountForRow(wallet);
      const txParams = buildTxParams(tx);
      signed = await signer.signTransaction({
        ...txParams,
        chainId: tx.chainId ?? env.HYPERPAXEER_CHAIN_ID,
      } as TransactionSerializable);
    } catch (err) {
      req.log.error({ err, userId }, 'funded sign_transaction failed');
      return reply.code(500).send({ error: 'sign_failed', detail: (err as Error).message });
    }

    await logSignature({
      user_id: userId,
      wallet_id: wallet.id,
      address: wallet.address,
      kind: 'transaction',
      to_address: tx.to ?? null,
      value_wei: valueWei,
      chain_id: tx.chainId ?? env.HYPERPAXEER_CHAIN_ID,
      request_hash: hashRequest({ funded: true, tx }),
      ip: clientIp(req.headers as Record<string, string | string[] | undefined>, req.ip),
      user_agent: req.headers['user-agent'] ?? null,
    });

    return reply.send({
      signed_tx: signed,
      address: wallet.address,
      chain_id: tx.chainId ?? env.HYPERPAXEER_CHAIN_ID,
      kind: 'funded',
    });
  });

  /**
   * POST /v1/funded/send
   * Sign + broadcast. Same gating as /sign, but submits via the configured
   * RPC and returns the tx hash. Serialised per-wallet via withWalletLock.
   */
  app.post('/v1/funded/send', { preHandler: requireAuth }, async (req, reply) => {
    const parsed = SendTxBody.safeParse(req.body);
    if (!parsed.success) {
      return reply.code(400).send({ error: 'invalid_body', issues: parsed.error.issues });
    }
    const userId = req.user!.id;
    const { tx } = parsed.data;
    const valueWei = tx.value ? BigInt(tx.value) : 0n;

    const loaded = await loadFundedContext(req, reply);
    if (!loaded) return;
    const { wallet, account } = loaded;

    const funded = await evaluateFunded({
      account,
      tx: { to: tx.to, value: valueWei, data: tx.data },
    });
    if (!funded.allow) return sendFundedDeny(reply, funded);

    const standard = await evaluateStandardPolicy({ user_id: userId, value_wei: valueWei });
    if (!standard.allow) {
      return reply.code(403).send({ error: standard.code, message: standard.message });
    }

    // Gas pre-check.
    try {
      await ensureGas(wallet.address);
    } catch (err) {
      if (err instanceof GasRefillFailedError) {
        req.log.error({ err }, 'gas refill failed before send');
        return reply.code(503).send({ error: 'gas_refill_failed', message: err.message });
      }
      throw err;
    }

    let txHash: Hex;
    try {
      txHash = await withWalletLock(wallet.address, async () => {
        const signer = await getSigningAccountForRow(wallet);
        const client = createWalletClient({
          chain: hyperPaxeer,
          transport: http(env.HYPERPAXEER_RPC_URL),
          account: {
            address: signer.address,
            type: 'local',
            source: 'paxeer-funded',
            publicKey: '0x' as Hex,
            signTransaction: signer.signTransaction,
            signMessage: signer.signMessage,
            signTypedData: signer.signTypedData,
          },
        });
        const params = buildTxParams(tx);
        return client.sendTransaction({ ...params, chain: hyperPaxeer });
      });
    } catch (err) {
      req.log.error({ err, userId }, 'funded send_transaction failed');
      return reply.code(500).send({ error: 'send_failed', detail: (err as Error).message });
    }

    await logSignature({
      user_id: userId,
      wallet_id: wallet.id,
      address: wallet.address,
      kind: 'transaction',
      to_address: tx.to ?? null,
      value_wei: valueWei,
      chain_id: tx.chainId ?? env.HYPERPAXEER_CHAIN_ID,
      request_hash: hashRequest({ funded: true, tx }),
      tx_hash: txHash,
      ip: clientIp(req.headers as Record<string, string | string[] | undefined>, req.ip),
      user_agent: req.headers['user-agent'] ?? null,
    });

    return reply.send({
      tx_hash: txHash,
      address: wallet.address,
      chain_id: tx.chainId ?? env.HYPERPAXEER_CHAIN_ID,
      kind: 'funded',
    });
  });

  /**
   * POST /v1/funded/sign-message
   * EIP-191 personal_sign. No funded gating beyond status check — messages
   * have no contract destination or transferable value. Mirrors
   * /v1/wallet/sign-message in shape.
   */
  app.post('/v1/funded/sign-message', { preHandler: requireAuth }, async (req, reply) => {
    const parsed = SignMessageBody.safeParse(req.body);
    if (!parsed.success) {
      return reply.code(400).send({ error: 'invalid_body', issues: parsed.error.issues });
    }
    const userId = req.user!.id;

    const loaded = await loadFundedContext(req, reply);
    if (!loaded) return;
    const { wallet, account } = loaded;

    // Status gate only — messages don't move funds.
    const funded = await evaluateFunded({ account, tx: { to: wallet.address } });
    if (!funded.allow && (funded.code === 'ACCOUNT_BREACHED' || funded.code === 'ACCOUNT_CLOSED')) {
      return sendFundedDeny(reply, funded);
    }

    const standard = await evaluateStandardPolicy({ user_id: userId, value_wei: 0n });
    if (!standard.allow) {
      return reply.code(403).send({ error: standard.code, message: standard.message });
    }

    let signature: Hex;
    try {
      const signer = await getSigningAccountForRow(wallet);
      signature = await signer.signMessage({ message: parsed.data.message });
    } catch (err) {
      req.log.error({ err, userId }, 'funded sign_message failed');
      return reply.code(500).send({ error: 'sign_failed', detail: (err as Error).message });
    }

    await logSignature({
      user_id: userId,
      wallet_id: wallet.id,
      address: wallet.address,
      kind: 'message',
      request_hash: hashRequest({ funded: true, message: parsed.data.message }),
      ip: clientIp(req.headers as Record<string, string | string[] | undefined>, req.ip),
      user_agent: req.headers['user-agent'] ?? null,
    });

    return reply.send({ signature, address: wallet.address, kind: 'funded' });
  });
}

// -----------------------------------------------------------------------------
// Shape helpers
// -----------------------------------------------------------------------------

function publicWallet(w: WalletRow): {
  id: string;
  address: `0x${string}`;
  chain_id: number;
  kind: 'standard' | 'funded' | 'agent';
  created_at: string;
  last_used_at: string | null;
} {
  return {
    id: w.id,
    address: w.address,
    chain_id: w.chain_id,
    kind: w.kind as 'standard' | 'funded' | 'agent',
    created_at: w.created_at,
    last_used_at: w.last_used_at,
  };
}

function shapeAccount(a: FundedAccountRow): {
  id: string;
  tier_id: string;
  status: string;
  starting_value_usd: string;
  peak_value_usd: string;
  current_value_usd: string | null;
  daily_start_value_usd: string | null;
  daily_start_at: string | null;
  last_eval_at: string | null;
  funding_tx_hashes: { usdl?: string; pax?: string };
  capital_fee_owed_usd: string;
  breached_reason: string | null;
  created_at: string;
} {
  return {
    id: a.id,
    tier_id: a.tier_id,
    status: a.status,
    starting_value_usd: a.starting_value_usd,
    peak_value_usd: a.peak_value_usd,
    current_value_usd: a.current_value_usd,
    daily_start_value_usd: a.daily_start_value_usd,
    daily_start_at: a.daily_start_at,
    last_eval_at: a.last_eval_at,
    funding_tx_hashes: a.funding_tx_hashes,
    capital_fee_owed_usd: a.capital_fee_owed_usd,
    breached_reason: a.breached_reason,
    created_at: a.created_at,
  };
}

/**
 * Convert raw token units (e.g. 25_000_000_000 for 25,000 USDL at 6dp) into a
 * decimal string at the same precision Postgres stores ("25000.000000").
 */
function unitsToDecimalString(units: bigint, decimals: number): string {
  if (decimals === 0) return units.toString();
  const s = units.toString().padStart(decimals + 1, '0');
  return `${s.slice(0, -decimals)}.${s.slice(-decimals)}`;
}
