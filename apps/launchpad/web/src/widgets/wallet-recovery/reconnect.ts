import { getAddress, type Address } from 'viem';
import { PaxeerWallet } from '../../core/wallet-sdk/client';
import { WalletSessionController } from '../../core/wallet-sdk/session';

export const walletRecoveryGuidance = "Your wallet app holds your keys and backup. We'll never ask for your seed phrase or private key. To reconnect, just sign in with your wallet again.";
export interface WalletReconnectAttempt { connectorId: string; expectedWallet: Address; chainId: 125; sessionEpoch: number; createdAt: number; terminalReason?: string }
/** One user-initiated attempt at a time, no retry signature prompts. */
export class WalletReconnect {
  state: 'disconnected' | 'awaiting_wallet_connection' | 'awaiting_fresh_proof' | 'distinct_identity_requires_own_proof' = 'disconnected';
  attempt?: WalletReconnectAttempt;
  private pending = false;
  constructor(private readonly sessions: WalletSessionController) {}
  async connect(wallet: PaxeerWallet, connectorId: string, expectedWallet: Address, registeredIds: readonly string[], providerKind: 'injected' | 'walletConnect' = 'injected') {
    if (this.pending) throw new Error('Reconnect already in progress');
    if (!registeredIds.includes(connectorId)) throw new Error('Unregistered connector');
    const expected = getAddress(expectedWallet);
    this.pending = true;
    this.state = 'awaiting_wallet_connection';
    this.sessions.disconnect();
    const epoch = this.sessions.sessionEpoch;
    const cancellationEpoch = this.sessions.cancellationEpoch;
    try {
      const accounts = await wallet.connect();
      const selected = accounts[0]!;
      this.sessions.assertConnecting(epoch, selected, 125, cancellationEpoch);
      const session = this.sessions.select(connectorId, providerKind, selected, 125, registeredIds);
      this.attempt = { connectorId, expectedWallet: expected, chainId: 125, sessionEpoch: session.sessionEpoch, createdAt: Date.now() };
      const matched = selected === expected;
      this.state = matched ? 'awaiting_fresh_proof' : 'distinct_identity_requires_own_proof';
      if (!matched) this.attempt.terminalReason = 'address_changed';
      return { session, matched, privateAccess: false as const };
    } catch (error) { if (epoch === this.sessions.sessionEpoch) this.sessions.disconnect(); this.state = 'disconnected'; throw error; }
    finally { this.pending = false; }
  }
}
