import { isWalletProvider } from './client';

/** Name shown for an injected wallet whose brand cannot be determined. */
export const BROWSER_WALLET_NAME = 'Browser wallet';

/** Result of probing one injected connector for a live EIP-1193 provider. */
export interface InjectedPresence {
  present: boolean;
  /** Display name: the EIP-6963 `info.name` / wagmi connector name, a known provider flag, or {@link BROWSER_WALLET_NAME}. */
  name: string;
}

export interface InjectedConnectorLike {
  id: string;
  name: string;
  type: string;
  getProvider(): Promise<unknown>;
}

// Order matters: several wallets also set `isMetaMask` for compatibility, so
// MetaMask is checked last.
const PROVIDER_FLAGS: ReadonlyArray<readonly [string, string]> = [
  ['isRabby', 'Rabby'],
  ['isBraveWallet', 'Brave Wallet'],
  ['isCoinbaseWallet', 'Coinbase Wallet'],
  ['isOkxWallet', 'OKX Wallet'],
  ['isTrust', 'Trust Wallet'],
  ['isTrustWallet', 'Trust Wallet'],
  ['isPhantom', 'Phantom'],
  ['isZerion', 'Zerion'],
  ['isFrame', 'Frame'],
  ['isTokenPocket', 'TokenPocket'],
  ['isMetaMask', 'MetaMask'],
];

/** Best-effort brand name of a legacy `window.ethereum` provider from its `isX` flags. */
export function providerFlagName(provider: unknown): string | null {
  if (typeof provider !== 'object' || provider === null) return null;
  const flags = provider as Record<string, unknown>;
  for (const [flag, name] of PROVIDER_FLAGS) if (flags[flag] === true) return name;
  return null;
}

/**
 * Probes an injected connector for a usable provider without connecting.
 * The generic `injected` connector is named from the provider's flags (its
 * wagmi name is the literal "Injected"); EIP-6963 connectors already carry the
 * wallet's announced `info.name`. Browser only: call it from an effect.
 */
export async function detectInjectedWallet(connector: InjectedConnectorLike): Promise<InjectedPresence> {
  let provider: unknown;
  try { provider = await connector.getProvider(); } catch { provider = undefined; }
  const present = isWalletProvider(provider);
  const generic = connector.id === 'injected' || connector.name === 'Injected';
  const name = generic ? (present ? providerFlagName(provider) : null) ?? BROWSER_WALLET_NAME : connector.name;
  return { present, name };
}
