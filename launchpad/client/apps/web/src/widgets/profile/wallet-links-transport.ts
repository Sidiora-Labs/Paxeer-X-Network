import { gatewayUrl } from '@/core/sdk-config';
import { normalizeAddress } from './account-disclosure';
import { accountRequest } from './account-transport';

/** media/gateway identity_models.go WalletLink (GET /v2/me/wallet-links, owner session). */
export interface WalletLink {
  linkId: string;
  accountId: string;
  chainId: string;
  wallet: string;
  visibility: 'private' | 'public_optin';
  state: 'pending' | 'verified' | 'revoked';
  linkedAt: string | null;
}

interface WalletLinksEnvelope { data: { items: unknown } }

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null;
}

export function parseWalletLink(value: unknown): WalletLink {
  if (!isRecord(value)) throw new TypeError('Wallet link row is not an object');
  const { linkId, accountId, chainId, wallet, visibility, state, linkedAt } = value;
  if (typeof linkId !== 'string' || typeof accountId !== 'string' || typeof chainId !== 'string' || typeof wallet !== 'string') throw new TypeError('Wallet link identity fields are missing');
  if (visibility !== 'private' && visibility !== 'public_optin') throw new TypeError('Wallet link visibility is unknown');
  if (state !== 'pending' && state !== 'verified' && state !== 'revoked') throw new TypeError('Wallet link state is unknown');
  if (linkedAt !== null && typeof linkedAt !== 'string') throw new TypeError('Wallet link linkedAt is invalid');
  return { linkId, accountId, chainId, wallet: normalizeAddress(wallet), visibility, state, linkedAt };
}

/** Wallets attached to the signed-in account. Pending and revoked links are excluded. */
export async function readWalletLinks(): Promise<WalletLink[]> {
  const response = await accountRequest<WalletLinksEnvelope>(gatewayUrl('/v2/me/wallet-links'));
  const items = response?.data?.items;
  if (!Array.isArray(items)) throw new TypeError('Wallet links response has no items');
  return items.map(parseWalletLink).filter(link => link.state === 'verified');
}

/** The route wallet first, then verified links on chain 125 that are not the route wallet. */
export function mergeProfileWallets(routeWallet: string, links: readonly WalletLink[]): string[] {
  const primary = normalizeAddress(routeWallet);
  const rest = links.filter(link => link.chainId === '125' && link.wallet !== primary).map(link => link.wallet);
  return [primary, ...new Set(rest)];
}
