import { socialWsUrl } from '@/core/sdk-config';
import { plainText } from '@/core/user-facing-error';
import { ChatError, decodeMessage, issueChatTicket, type DmMessage, type PoolMessage } from './chat-api';

export type WsStatus = 'disconnected' | 'connecting' | 'authenticating' | 'connected' | 'authorization-required';
export interface PersistedAck { clientMessageId: string; id: string; conversationId: string; sequence: string; revision: string }
export interface ChatWsCallbacks {
  onPoolMessage?: (message: PoolMessage) => void;
  onDm?: (message: DmMessage) => void;
  onPersisted?: (ack: PersistedAck) => void;
  onUncertain?: (clientId: string) => void;
  onReplay?: () => void;
  onRejected?: (clientId: string, error: string) => void;
  onRead?: (conversationId: string, through: string) => void;
  onStatusChange?: (status: WsStatus) => void;
  onError?: (error: string) => void;
  onReset?: () => void;
}
export interface ChatAuth { wallet: string }
type ConsumerId = string;
const consumers = new Map<ConsumerId, ChatWsCallbacks>();
const joinedPools = new Map<string,number>();
let socket: WebSocket | undefined;
let auth: ChatAuth | undefined;
let status: WsStatus = 'disconnected';
let generation = 0;
let abort = new AbortController();
let retryTimer: ReturnType<typeof setTimeout> | undefined;
let acceptTimer: ReturnType<typeof setTimeout> | undefined;
let attempt = 0;
let rejected = false;
let dmSubscribed = 0;
let nextConsumer = 0;
let openingGeneration: number | undefined;
function emit<K extends keyof ChatWsCallbacks>(key: K, invoke: (callback: NonNullable<ChatWsCallbacks[K]>) => void) { for (const callbacks of consumers.values()) { const callback = callbacks[key]; if (callback) invoke(callback as NonNullable<ChatWsCallbacks[K]>); } }
function setStatus(value: WsStatus) { status = value; emit('onStatusChange', callback => callback(value)); }
const chatOffline = 'Chat is not connecting right now, try again';
/** Emits plain wording to the screen; the raw reason goes to the console. */
function error(value: string) { const plain = plainText(value, chatOffline); if (plain !== value) console.error('[chat]', value); emit('onError', callback => callback(plain)); }
function stop(clear: boolean) {
  generation++; openingGeneration = undefined; abort.abort(); abort = new AbortController();
  clearTimeout(retryTimer); clearTimeout(acceptTimer); retryTimer = undefined; acceptTimer = undefined;
  const previous = socket; socket = undefined;
  if (previous) { previous.onclose = null; previous.onmessage = null; previous.onerror = null; previous.onopen = null; previous.close(1000); }
  setStatus('disconnected');
  if (clear) { joinedPools.clear(); dmSubscribed = 0; emit('onReset', callback => callback()); }
}
function topics() { return [...joinedPools.keys()].map(pool => `chat.pool.${pool}`).concat(dmSubscribed && auth ? [`private.chat.${auth.wallet.toLowerCase()}`] : []); }
function send(frame: unknown) { if (status !== 'connected' || socket?.readyState !== WebSocket.OPEN) throw new Error('Chat is offline, message not sent'); socket.send(JSON.stringify(frame)); }
function subscriptions() { for (const poolAddress of joinedPools.keys()) send({ type: 'join_pool', pool_address: poolAddress }); if (dmSubscribed) send({ type: 'subscribe_dms' }); }
function retry() {
  if (rejected || !auth || !consumers.size || !topics().length) return;
  clearTimeout(retryTimer);
  retryTimer = setTimeout(() => { retryTimer = undefined; void open(); }, Math.min(30_000, 500 * 2 ** Math.min(attempt++, 6)) + Math.floor(Math.random() * 250));
}
async function open() {
  if (socket || openingGeneration === generation || rejected || !auth || !topics().length) return;
  const epoch = generation, wallet = auth.wallet, signal = abort.signal;
  openingGeneration = epoch;
  setStatus('authenticating');
  try {
    const ticket = await issueChatTicket(topics(), signal);
    if (signal.aborted || epoch !== generation || wallet !== auth?.wallet) return;
    const endpoint = new URL(socialWsUrl()); endpoint.searchParams.set('topics', topics().join(','));
    const ws = new WebSocket(endpoint, ['sidiora.v2', `ticket.${ticket.ticket}`]);
    socket = ws; setStatus('connecting');
    acceptTimer = setTimeout(() => { if (socket === ws && status !== 'connected') ws.close(4003, 'Session acceptance timeout'); }, 10_000);
    ws.onopen = () => { if (epoch !== generation || ws.protocol !== 'sidiora.v2') ws.close(4003, 'Protocol version mismatch'); };
    ws.onmessage = event => {
      if (epoch !== generation || signal.aborted) return;
      try {
        const frame: unknown = JSON.parse(String(event.data));
        if (!frame || typeof frame !== 'object' || Array.isArray(frame)) throw new Error('Invalid chat frame');
        const data = frame as Record<string, unknown>;
        if (data.type === 'session_accepted') {
          if (data.protocol_version !== 2) { ws.close(4003, 'Protocol version mismatch'); return; }
          clearTimeout(acceptTimer); attempt = 0; setStatus('connected'); subscriptions(); return;
        }
        if (status !== 'connected') throw new Error('Message before session acceptance');
        if (data.type === 'pool_message' || data.type === 'dm') {
          const message = decodeMessage(data);
          if (data.type === 'pool_message') emit('onPoolMessage', callback => callback(message));
          else emit('onDm', callback => callback(message));
        } else if (data.type === 'message_persisted') {
          if (['client_message_id', 'id', 'conversation_id', 'sequence', 'revision'].some(key => typeof data[key] !== 'string')) throw new Error('Invalid durable acknowledgement');
          emit('onPersisted', callback => callback({ clientMessageId: data.client_message_id as string, id: data.id as string, conversationId: data.conversation_id as string, sequence: data.sequence as string, revision: data.revision as string }));
        } else if (data.type === 'joined_pool' || data.type === 'subscribed_dms') { emit('onReplay', callback => callback());
        } else if (data.type === 'message_uncertain') { if (typeof data.client_message_id !== 'string') throw new Error('Invalid uncertain acknowledgement'); emit('onUncertain', callback => callback(data.client_message_id as string));
        } else if (data.type === 'read_marker') {
          if (typeof data.conversation_id !== 'string' || typeof data.through_sequence !== 'string') throw new Error('Invalid read marker');
          emit('onRead', callback => callback(data.conversation_id as string, data.through_sequence as string));
        } else if (data.type === 'error') {
          const message = plainText(typeof data.message === 'string' ? data.message : null, 'Message not sent, try again');
          if (typeof data.client_message_id === 'string') emit('onRejected', callback => callback(data.client_message_id as string, message));
          error(message);
        }
      } catch (cause) { error(cause instanceof Error ? cause.message : 'Invalid chat response'); ws.close(1007, 'Invalid chat response'); }
    };
    ws.onclose = event => {
      if (epoch !== generation) return;
      clearTimeout(acceptTimer); socket = undefined;
      if (event.code === 4001 || event.code === 4003) { rejected = true; setStatus('authorization-required'); }
      else { setStatus('disconnected'); retry(); }
    };
    ws.onerror = () => { if (epoch === generation) error(chatOffline); };
  } catch (cause) {
    if (signal.aborted || epoch !== generation) return;
    error(cause instanceof Error ? cause.message : 'Chat connection failed');
    if (cause instanceof ChatError && (cause.status === 401 || cause.status === 403)) { rejected = true; setStatus('authorization-required'); }
    else { setStatus('disconnected'); retry(); }
  } finally { if (openingGeneration === epoch) openingGeneration = undefined; }
}
export function addConsumer(identity: ChatAuth, callbacks: ChatWsCallbacks): ConsumerId {
  if (auth && auth.wallet.toLowerCase() !== identity.wallet.toLowerCase()) { stop(true); consumers.clear(); rejected = false; attempt = 0; }
  auth = { wallet: identity.wallet.toLowerCase() };
  const id = String(++nextConsumer); consumers.set(id, callbacks); callbacks.onStatusChange?.(status); void open(); return id;
}
export function removeConsumer(id: ConsumerId) { consumers.delete(id); if (!consumers.size) disconnect(); }
export function connect(identity: ChatAuth, callbacks: ChatWsCallbacks) { consumers.delete('legacy'); const id = addConsumer(identity, callbacks); const value = consumers.get(id)!; consumers.delete(id); consumers.set('legacy', value); }
export function disconnect() { stop(true); auth = undefined; rejected = false; attempt = 0; }
export function refreshAuthorization() { rejected = false; stop(false); void open(); }
export function joinPool(pool: string) { const normalized = pool.toLowerCase(); const count=joinedPools.get(normalized) ?? 0; if(count){joinedPools.set(normalized,count+1);return;} if (topics().length >= 20) throw new Error('Chat subscription limit'); joinedPools.set(normalized,1); stop(false); void open(); }
export function leavePool(pool: string) { const normalized=pool.toLowerCase(); const count=joinedPools.get(normalized) ?? 0;if(count>1){joinedPools.set(normalized,count-1);return;} if (!joinedPools.delete(normalized)) return; stop(false); void open(); }
export function subscribeDms() { if (dmSubscribed) { dmSubscribed++; return; } if (topics().length >= 20) throw new Error('Chat subscription limit'); dmSubscribed = 1; stop(false); void open(); }
export function sendPoolMessage(poolAddress: string, content: string, replyToId?: string | null, clientMessageId = crypto.randomUUID()) { send({ type: 'pool_message', pool_address: poolAddress, content, reply_to_id: replyToId ?? null, client_message_id: clientMessageId }); return clientMessageId; }
export function sendDm(to: string, content: string, clientMessageId = crypto.randomUUID()) { send({ type: 'dm', to, content, client_message_id: clientMessageId }); return clientMessageId; }
export function getStatus() { return status; }

export function unsubscribeDms(){if(dmSubscribed>1){dmSubscribed--;return;}if(!dmSubscribed)return;dmSubscribed=0;stop(false);void open();}
