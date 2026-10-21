import { OPTIONAL_CONTRACT_CAPABILITIES } from '@/core/network/deployment';
/** There is currently no admitted user-share/redemption interface. Keep reserve
 * balances out of the ownership view and never mount withdrawal hooks. */
export default function LiquidityPosition() {
  // This remains fail closed even if a core AMM pool has large reserves.
  const enabled = OPTIONAL_CONTRACT_CAPABILITIES.metaAg;
  return <section role="status" className="rounded-xl bg-surface-2 p-4 text-sm text-text-muted">
    <p>Liquidity positions are not available yet</p>
    <p>Coin reserves and vault funds aren't shares you own. Vault deposits can't be withdrawn</p>
    <p>{enabled ? 'Withdrawals are coming soon' : "Withdrawals aren't available yet"}</p>
  </section>;
}
