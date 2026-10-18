import type { WalletRowBadge } from './sign-in-wallet-row';

const RECENT_CONNECTOR_KEY = 'sidiora:recent-connector:v1';

export function readRecentConnectorId(): string | null {
  try {
    const value = localStorage.getItem(RECENT_CONNECTOR_KEY);
    return value && value.length <= 128 ? value : null;
  } catch { return null; }
}

export function rememberConnector(id: string) {
  try { localStorage.setItem(RECENT_CONNECTOR_KEY, id); } catch { /* Connector memory is a convenience only. */ }
}

export interface WalletChoice { id: string; name: string; type: string; icon?: string }
export interface OrderedChoice<T extends WalletChoice> { connector: T; badge: WalletRowBadge }

/**
 * Orders wallet rows: the last-used connector first (RECENT), then injected
 * providers announced through EIP-6963 or a present window.ethereum (DETECTED),
 * then everything else. The generic `injected` connector is hidden when a
 * named injected provider was discovered, so a wallet never appears twice.
 */
export function orderWalletChoices<T extends WalletChoice>(connectors: readonly T[], recentId: string | null, genericInjectedDetected: boolean): OrderedChoice<T>[] {
  const namedInjected = connectors.some(connector => connector.type === 'injected' && connector.id !== 'injected');
  const visible = connectors.filter(connector => connector.id !== 'injected' || !namedInjected);
  const rows = visible.map<OrderedChoice<T>>(connector => {
    const detected = connector.type === 'injected' && (connector.id !== 'injected' || genericInjectedDetected);
    const badge: WalletRowBadge = connector.id === recentId ? 'recent' : detected ? 'detected' : null;
    return { connector, badge };
  });
  const rank = (row: OrderedChoice<T>) => row.badge === 'recent' ? 0 : row.badge === 'detected' ? 1 : 2;
  return rows.sort((a, b) => rank(a) - rank(b));
}
