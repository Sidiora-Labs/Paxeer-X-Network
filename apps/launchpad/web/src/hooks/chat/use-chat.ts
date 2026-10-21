'use client';
import { useState, useEffect, useCallback, useRef } from 'react';
import { useAccount } from 'wagmi';
import { ChatError, getPoolMessages, getDmMessages, markConversationRead, reviseChatMessage, type PoolMessage } from '@/core/clients/chat-api';
import * as chatWs from '@/core/clients/chat-ws';
import { reportUserFacingError, UserFacingError } from '@/core/user-facing-error';
import { ConversationState, type PendingSend } from './conversation-state';

export function useChatAuth() {
  const { address, isConnected } = useAccount();
  const [ready, setReady] = useState(false);
  useEffect(() => {
    setReady(false);
    if(!address || !isConnected)return;
    const id=chatWs.addConsumer({wallet:address},{onStatusChange:status=>setReady(status==='connected'),onReset:()=>setReady(false)});
    return()=>chatWs.removeConsumer(id);
  },[address,isConnected]);
  const authenticate = useCallback(async () => {
    if (!address || !isConnected) return null;
    chatWs.refreshAuthorization();
    return chatWs.getStatus()==='connected'?{wallet:address}:null;
  }, [address, isConnected]);
  return { ready, authenticate, auth: ready && address ? { wallet: address } : null };
}
function useConversation(kind: 'pool' | 'dm', target: string, conversationId: string) {
  const { address, isConnected } = useAccount();
  const [messages, setMessages] = useState<PoolMessage[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string>();
  const [wsStatus, setWsStatus] = useState<chatWs.WsStatus>('disconnected');
  const [replyTo, setReplyTo] = useState<PoolMessage | null>(null);
  const state = useRef(new ConversationState());
  const epoch = useRef(0);
  const renderedWallet = useRef(address?.toLowerCase());
  const timeout = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const readTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const publish = useCallback(() => {
    const current = state.current;
    const rows: PoolMessage[] = current.messages.map(row => ({ ...row, poolAddress: row.poolAddress ?? '' }));
    for (const pending of current.pending.values()) rows.push({ id: pending.id ?? pending.clientMessageId, clientMessageId: pending.clientMessageId, conversationId, poolAddress: kind === 'pool' ? target : '', sender: pending.sender, content: pending.content, replyToId: pending.replyToId, createdAt: pending.createdAt, sequence: pending.sequence ?? '0', revision: pending.revision ?? '1', deleted: false, sendState: pending.state });
    setMessages(rows);
  }, [conversationId, kind, target]);
  const transmit = useCallback((pending: PendingSend) => {
    if (pending.kind === 'pool') chatWs.sendPoolMessage(pending.target, pending.content, pending.replyToId, pending.clientMessageId);
    else chatWs.sendDm(pending.target, pending.content, pending.clientMessageId);
  }, []);
  useEffect(() => {
    renderedWallet.current = address?.toLowerCase();
    const generation = ++epoch.current;
    const controller = new AbortController();
    const local = new ConversationState(address?.toLowerCase()); state.current.clear(); state.current = local;
    setMessages([]); setReplyTo(null); setError(undefined); setLoading(true);
    const current = () => !controller.signal.aborted && generation === epoch.current;
    let replaying = false;
    let replayRequested = false;
    const replay = async () => {
      if (replaying) { replayRequested = true; return; }
      replaying = true;
      if (!conversationId || !address || !isConnected) { replaying = false; if (current()) setLoading(false); return; }
      try {
        let resets = 0;
        for (;;) {
          const previous = local.cursor;
          let page;
          try { page = await (kind === 'pool' ? getPoolMessages(target, { cursor: local.cursor, signal: controller.signal }) : getDmMessages(conversationId, { cursor: local.cursor, signal: controller.signal })); }
          catch (cause) {
            if (cause instanceof ChatError && (cause.code === 'history_snapshot_required' || cause.code === 'replay_window_expired') && cause.nextCursor && resets++ < 2) { local.resetSnapshot(); local.cursor = cause.nextCursor; if (current()) publish(); else return; continue; }
            throw cause;
          }
          if (!current()) return;
          if (page.resetRequired) local.resetSnapshot();
          for (const row of page.messages) local.apply(row);
          local.read(page.readThroughSequence);
          local.cursor = page.nextCursor; publish();
          if (!page.hasMore || page.nextCursor === previous) break;
        }
        if (current()) {
          setError(undefined);
          for (const pending of local.pending.values()) if (pending.state === 'uncertain') transmit(pending);
        }
      } catch (cause) { if (current()) { if (cause instanceof ChatError && (cause.status === 401 || cause.status === 403 || cause.status === 404)) { local.clear(); setMessages([]); setReplyTo(null); } setError(reportUserFacingError(cause, 'Messages are not loading right now, try again', 'chat-history')); } }
      finally { replaying = false; if (current()) { setLoading(false); if (replayRequested) { replayRequested = false; void replay(); } } }
    };
    void replay();
    if (!address || !isConnected || !conversationId) return () => { controller.abort(); local.clear(); };
    const id = chatWs.addConsumer({ wallet: address }, {
      onPoolMessage: row => { if (current() && row.conversationId === conversationId) { local.apply(row); publish(); } },
      onDm: row => { if (current() && row.conversationId === conversationId) { local.apply(row); publish(); } },
      onPersisted: ack => { if (current() && ack.conversationId === conversationId) { local.acknowledge(ack.clientMessageId, ack.id, ack.sequence, ack.revision); publish(); } },
      onReplay: () => { if (current()) void replay(); },
      onUncertain: clientId => { const pending=local.pending.get(clientId); if(current()&&pending){local.pending.set(clientId,{...pending,state:'uncertain'});publish();} },
      onRejected: (clientId, reason) => { if (!current()) return; const pending = local.pending.get(clientId); if (pending) { local.pending.set(clientId, { ...pending, state: 'rejected' }); setError(reason); publish(); } },
      onRead: (id, through) => { if (current() && id === conversationId) { local.read(through); publish(); } },
      onStatusChange: value => { if (!current()) return; setWsStatus(value); if (value === 'connected') void replay(); else { local.uncertain(); publish(); } },
      onError: reason => { if (current()) setError(reason); },
      onReset: () => { local.clear(); if (current()) { setMessages([]); setReplyTo(null); } },
    });
    if (kind === 'pool') chatWs.joinPool(target); else chatWs.subscribeDms();
    return () => { controller.abort(); epoch.current++; clearTimeout(timeout.current); clearTimeout(readTimer.current); local.clear(); if (kind === 'pool') chatWs.leavePool(target); else chatWs.unsubscribeDms(); chatWs.removeConsumer(id); };
  }, [address, isConnected, kind, target, conversationId, publish, transmit]);
  const sendMessage = useCallback(async (content: string) => {
    if (!content.trim() || !address || renderedWallet.current !== address.toLowerCase() || wsStatus !== 'connected') return false;
    const clientMessageId = crypto.randomUUID();
    const pending: PendingSend = { clientMessageId, target, kind, content: content.trim(), replyToId: replyTo?.id ?? null, sender: address, createdAt: Math.floor(Date.now() / 1000), state: 'submitting' };
    state.current.pending.set(clientMessageId, pending); publish(); setReplyTo(null);
    try { transmit(pending); }
    catch (cause) { state.current.pending.set(clientMessageId, { ...pending, state: 'rejected' }); setError(reportUserFacingError(cause, 'Message not sent, try again', 'chat-send')); publish(); return false; }
    clearTimeout(timeout.current);
    timeout.current = setTimeout(() => { state.current.uncertain(); publish(); }, 10_000);
    return true;
  }, [address, wsStatus, target, kind, replyTo, publish, transmit]);
  const retryMessage = useCallback((clientId: string) => {
    const pending = state.current.pending.get(clientId);
    if (!pending || wsStatus !== 'connected') return;
    state.current.pending.set(clientId, { ...pending, state: 'submitting' }); publish();
    try { transmit(pending); } catch (cause) { state.current.pending.set(clientId, { ...pending, state: 'rejected' }); setError(reportUserFacingError(cause, 'Message not sent, try again', 'chat-send')); publish(); }
    clearTimeout(timeout.current); timeout.current = setTimeout(() => { state.current.uncertain(); publish(); }, 10_000);
  }, [wsStatus, transmit, publish]);
  const markRead = useCallback(() => {
    const rows = state.current.messages;
    const through = rows.at(-1)?.sequence;
    if (!through || !address || readTimer.current || BigInt(through) <= state.current.readThrough) return;
    const generation = epoch.current;
    readTimer.current = setTimeout(() => { readTimer.current = undefined; if (generation !== epoch.current) return; void markConversationRead(conversationId, through).then(() => { if (generation === epoch.current) { state.current.read(through); publish(); } }).catch(cause => { if (generation === epoch.current) setError(reportUserFacingError(cause, 'Something went wrong, try again', 'chat-read')); }); }, 2000);
  }, [address, conversationId, publish]);
  const loadMore = useCallback(async () => {
    if(!address || !isConnected) throw new UserFacingError('Sign in to load more messages');
    const generation=epoch.current;
    const page=await (kind==='pool'?getPoolMessages(target,{cursor:state.current.cursor}):getDmMessages(conversationId,{cursor:state.current.cursor}));
    if(generation!==epoch.current)throw new UserFacingError('This chat changed, refresh to see new messages');
    if(page.resetRequired)state.current.resetSnapshot();
    for(const row of page.messages)state.current.apply(row);
    state.current.cursor=page.nextCursor;state.current.read(page.readThroughSequence);publish();return page.hasMore;
  },[address,isConnected,kind,target,conversationId,publish]);
  const reviseMessage = useCallback(async(message:PoolMessage,content:string,remove=false)=>{
    if(!address || renderedWallet.current!==address.toLowerCase() || message.sender.toLowerCase()!==address.toLowerCase())return false;
    const generation=epoch.current;
    try {const updated=await reviseChatMessage(message.id,message.revision,content,remove);if(generation!==epoch.current)return false;state.current.apply(updated);publish();setError(undefined);return true;}
    catch(cause){if(generation===epoch.current)setError(reportUserFacingError(cause,'Could not update your message, try again','chat-edit'));return false;}
  },[address,publish]);
  const connectChat = useCallback(async () => { chatWs.refreshAuthorization(); return isConnected; }, [isConnected]);
  return { messages: renderedWallet.current === address?.toLowerCase() ? messages : [], loading, error, wsStatus, sendMessage, retryMessage, reviseMessage, connectChat, loadMore, replyTo: renderedWallet.current === address?.toLowerCase() ? replyTo : null, setReplyTo, isAuthenticated: renderedWallet.current === address?.toLowerCase() && wsStatus === 'connected', isConnected, wallet: address, markRead, unreadCount: state.current.unreadCount };
}
export function usePoolChat(poolAddress: string) { return useConversation('pool', poolAddress.toLowerCase(), `pool:${poolAddress.toLowerCase()}`); }
export function useDmChat(conversationId: string, peerAddress: string) {
  const value = useConversation('dm', peerAddress, conversationId);
  return { ...value, sendDm: value.sendMessage, connectAndSubscribe: value.connectChat };
}
