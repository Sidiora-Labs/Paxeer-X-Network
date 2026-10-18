/**
 * @paxeer/wallet integration — public surface for Hyperpax-UI.
 *
 * The new system is server-signing (Supabase auth + REST API at
 * `connect.paxportwallet.com`). Old iframe/postMessage Oko bridge is gone.
 *
 *   PaxeerProvider / usePaxeer / useEmbeddedWalletAvailable
 *     → React context + hooks driving the UI
 *
 *   paxeerEmbeddedConnector / PAXEER_CONNECTOR_ID
 *     → wagmi v2 connector
 *
 *   getPaxeerWallet / resolvePaxeerConfig / getAuthRedirectUrl
 *     → low-level singleton helpers for the auth/callback page or scripts
 *
 *   PaxeerWallet (vendored SDK class)
 *     → escape hatch for advanced flows that want to call the SDK directly
 */

export {
  PaxeerProvider,
  usePaxeer,
  usePaxeerOptional,
  useEmbeddedWalletAvailable,
} from "./PaxeerProvider";
export type { SignInProvider, EmbeddedSignInResult, PaxeerContextValue } from "./PaxeerProvider";

export { paxeerEmbeddedConnector, PAXEER_CONNECTOR_ID } from "./paxeerConnector";
export type { PaxeerConnectorParams } from "./paxeerConnector";

export {
  getPaxeerWallet,
  resolvePaxeerConfig,
  getAuthRedirectUrl,
} from "./client";
export type { ResolvedPaxeerConfig } from "./client";

export { PaxeerWallet, PaxeerWalletError } from "./sdk";
export type {
  ChainInfo,
  OAuthProvider,
  PaxeerWalletConfig,
  PublicWallet,
  SendTxResponse,
  SignMessageResponse,
  SignTxResponse,
  TxRequest,
} from "./sdk";

export { createPaxeerEIP1193Provider } from "./eip1193";
export type { PaxeerEIP1193Provider } from "./eip1193";
