import { query } from './pool.js';

/**
 * Wallet-side mirror of the LayerX per-DID ledger head (layerx_accounts).
 * Written read-only from the LayerX sequencer Postgres by jobs/layerxSync.ts;
 * joinable to agent_principals to resolve DID → agent wallet → owner.
 */

export interface LayerxAccountMirror {
  did: string;
  evm_address: string | null;
  balance_usdx: string;
  escrow_usdx: string;
  layerx_updated: string | null;
  synced_at: string;
}

/** Upsert one account snapshot pulled from LayerX. */
export async function upsertLayerxAccount(args: {
  did: string;
  evmAddress: string | null;
  balanceUsdx: string;
  escrowUsdx: string;
  layerxUpdated: string;
}): Promise<void> {
  await query(
    `insert into layerx_accounts (did, evm_address, balance_usdx, escrow_usdx, layerx_updated, synced_at)
     values ($1, $2, $3, $4, $5, now())
     on conflict (did) do update
       set evm_address = excluded.evm_address,
           balance_usdx = excluded.balance_usdx,
           escrow_usdx = excluded.escrow_usdx,
           layerx_updated = excluded.layerx_updated,
           synced_at = now()`,
    [args.did, args.evmAddress, args.balanceUsdx, args.escrowUsdx, args.layerxUpdated],
  );
}

/** The newest LayerX account timestamp we've mirrored — the sync cursor. */
export async function latestMirroredAt(): Promise<string | null> {
  const { rows } = await query<{ max: string | null }>(
    `select max(layerx_updated)::text as max from layerx_accounts`,
  );
  return rows[0]?.max ?? null;
}

/** DID → agent-wallet → owner view for a single account (ecosystem linkage). */
export async function resolveDidLinkage(did: string): Promise<{
  did: string;
  agent_wallet: string | null;
  owner_user_id: string | null;
  balance_usdx: string | null;
  escrow_usdx: string | null;
} | null> {
  const { rows } = await query<{
    did: string;
    agent_wallet: string | null;
    owner_user_id: string | null;
    balance_usdx: string | null;
    escrow_usdx: string | null;
  }>(
    `select p.did,
            w.address       as agent_wallet,
            p.owner_user_id::text as owner_user_id,
            m.balance_usdx::text  as balance_usdx,
            m.escrow_usdx::text   as escrow_usdx
       from agent_principals p
       left join wallets w on w.id = p.wallet_id
       left join layerx_accounts m on m.did = p.did
      where p.did = $1
      limit 1`,
    [did],
  );
  return rows[0] ?? null;
}
