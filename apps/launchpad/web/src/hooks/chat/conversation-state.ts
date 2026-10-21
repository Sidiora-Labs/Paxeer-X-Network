import type { DmMessage, PoolMessage, SendState } from '../../core/clients/chat-api';
import { projectMessage, orderedMessages } from './revision-projection';
export type ChatMessage = DmMessage | PoolMessage;
export interface PendingSend { clientMessageId: string; target: string; kind: 'dm' | 'pool'; content: string; replyToId: string | null; sender: string; createdAt: number; state: SendState; id?: string; sequence?: string; revision?: string }
export class ConversationState {
  private rows: ReadonlyMap<string, ChatMessage> = new Map();
  readonly pending = new Map<string, PendingSend>();
  constructor(private readonly actor = '') {}
  readThrough = 0n;
  cursor = '';
  apply(message: ChatMessage) {
    const previous = this.rows;
    this.rows = projectMessage(this.rows, message);
    if (message.clientMessageId) {
      const pending = this.pending.get(message.clientMessageId);
      if (pending?.sender.toLowerCase() === message.sender.toLowerCase()) this.pending.delete(message.clientMessageId);
    }
    for (const [key, pending] of this.pending) if (pending.id === message.id) this.pending.delete(key);
    return previous !== this.rows;
  }
  get messages() { return orderedMessages(this.rows); }
  get unreadCount() { return this.messages.filter(row => !row.deleted && row.sender.toLowerCase() !== this.actor && BigInt(row.sequence) > this.readThrough).length; }
  read(through: string) { if (!/^(0|[1-9][0-9]{0,18})$/.test(through)) throw new Error('Invalid read marker'); const value = BigInt(through); if (value > this.readThrough) this.readThrough = value; }
  acknowledge(clientId: string, id: string, sequence: string, revision: string) {
    const pending = this.pending.get(clientId);
    if (!pending) return;
    if (!/^[1-9][0-9]{0,18}$/.test(sequence) || !/^[1-9][0-9]{0,18}$/.test(revision)) throw new Error('Invalid acknowledgement position');
    if (pending.id && pending.id !== id) throw new Error('Acknowledged message identity changed');
    if (this.rows.has(id)) { this.pending.delete(clientId); return; }
    this.pending.set(clientId, { ...pending, id, sequence, revision, state: 'persisted' });
  }
  uncertain() { for (const [key, pending] of this.pending) if (pending.state === 'submitting') this.pending.set(key, { ...pending, state: 'uncertain' }); }
  resetSnapshot() { this.rows = new Map(); this.cursor = ''; }
  clear() { this.resetSnapshot(); this.pending.clear(); this.readThrough = 0n; }
}
