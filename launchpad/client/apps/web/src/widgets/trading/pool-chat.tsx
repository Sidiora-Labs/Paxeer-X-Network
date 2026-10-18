'use client';

import { useEffect, useRef, useState } from 'react';
import Link from 'next/link';
import { usePoolChat } from '@/hooks/chat/use-chat';
import type { PoolMessage } from '@/core/clients/chat-api';
import { MessagesSquare } from 'lucide-react';
import { Avatar, Button, EmptyState } from '@/ui/primitives';
import { relativeAge, shortAddress } from '@/utils/format';
import { cn } from '@/lib/utils';

interface PoolChatProps {
  poolAddress: string;
}

function ChatMessage({ msg, onReply, replyTarget, onRetry, onRevise, wallet }: {
  msg: PoolMessage;
  onReply: (msg: PoolMessage) => void;
  replyTarget?: PoolMessage;
  onRetry: (id: string) => void;
  onRevise: (message: PoolMessage, content: string, remove?: boolean) => Promise<boolean>;
  wallet?: string;
}) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(msg.content);
  useEffect(() => { setEditing(false); setDraft(''); }, [wallet]);
  const failed = msg.sendState === 'rejected' || msg.sendState === 'uncertain';
  const own = wallet?.toLowerCase() === msg.sender.toLowerCase();
  return (
    <div role="article" data-message-id={msg.id} className="group flex gap-2.5 rounded-sm px-3 py-2 transition-colors hover:bg-surface-hover">
      <Avatar src={null} name={shortAddress(msg.sender)} size="sm" decorative className="mt-0.5" />
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <Link href={`/profile/${msg.sender}`} className="font-mono text-xs font-bold text-accent hover:underline">{shortAddress(msg.sender)}</Link>
          <time dateTime={new Date(msg.createdAt * 1000).toISOString()} className="text-2xs text-text-faint">{relativeAge(msg.createdAt)}</time>
          <button type="button" onClick={() => onReply(msg)} className="ml-auto text-2xs text-text-faint opacity-0 transition-opacity hover:text-text focus-visible:opacity-100 group-hover:opacity-100">Reply</button>
        </div>
        {msg.replyToId && replyTarget && (
          <div className="mt-0.5 truncate rounded-xs bg-surface-2 px-2 py-0.5 text-2xs text-text-faint">Replying to {shortAddress(replyTarget.sender, 3, 3)}: {replyTarget.content.slice(0, 60)}</div>
        )}
        <p className={cn('mt-0.5 break-words text-sm', msg.sendState === 'submitting' ? 'italic text-text-faint' : failed ? 'text-danger' : 'text-text')}>{msg.deleted ? 'Message removed' : msg.content}</p>
        {editing && <div className="mt-1 flex flex-col gap-1.5">
          <label className="sr-only" htmlFor={`pool-edit-${msg.id}`}>Edit message</label>
          <textarea id={`pool-edit-${msg.id}`} value={draft} maxLength={2000} onChange={event => setDraft(event.target.value)} className="min-h-16 w-full rounded-sm bg-surface-2 px-3 py-2 text-sm text-text outline-none focus-visible:outline-2 focus-visible:outline-focus" />
          <div className="flex gap-2"><Button size="sm" variant="primary" onClick={async () => { if (await onRevise(msg, draft)) setEditing(false); }}>Save edit</Button><Button size="sm" variant="ghost" onClick={() => setEditing(false)}>Cancel edit</Button></div>
        </div>}
        {!msg.deleted && msg.sequence !== '0' && own && <div className="mt-1 flex gap-2">
          {Date.now() / 1000 - msg.createdAt <= 900 && <button type="button" onClick={() => { setDraft(msg.content); setEditing(true); }} className="text-2xs text-text-faint hover:text-text">Edit</button>}
          <button type="button" onClick={() => onRevise(msg, '', true)} className="text-2xs text-text-faint hover:text-danger">Remove message</button>
        </div>}
        {failed && <button type="button" onClick={() => onRetry(msg.clientMessageId!)} className="mt-0.5 text-2xs text-danger hover:underline">Retry</button>}
        {msg.sendState === 'submitting' && <span className="text-2xs text-text-faint">Sending...</span>}
      </div>
    </div>
  );
}

export default function PoolChat({ poolAddress }: PoolChatProps) {
  const { messages, loading, error, retryMessage, reviseMessage, markRead, sendMessage, replyTo, setReplyTo, isAuthenticated, isConnected, wallet } = usePoolChat(poolAddress);
  const [input, setInput] = useState('');
  const [historyOffset, setHistoryOffset] = useState(0);
  const draftWallet = useRef(wallet);
  useEffect(() => { setInput(''); setHistoryOffset(0); draftWallet.current = wallet; }, [poolAddress, isConnected, wallet]);
  useEffect(() => { if (document.visibilityState === 'visible') markRead(); }, [messages, markRead]);
  const bottomRef = useRef<HTMLDivElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const [autoScroll, setAutoScroll] = useState(true);

  useEffect(() => {
    if (autoScroll && bottomRef.current) bottomRef.current.scrollIntoView({ behavior: 'smooth' });
  }, [messages.length, autoScroll]);

  const handleScroll = () => {
    const el = containerRef.current;
    if (!el) return;
    setAutoScroll(el.scrollHeight - el.scrollTop - el.clientHeight < 60);
  };

  const handleSend = async () => {
    if (!input.trim()) return;
    const msg = input;
    if (await sendMessage(msg)) setInput(value => value === msg ? '' : value);
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); void handleSend(); }
  };

  const msgMap = new Map(messages.map(m => [m.id, m]));
  const status = error || (isConnected && !isAuthenticated ? "Chat is reconnecting. You can send once it's back" : '');

  return (
    <div className="flex flex-col overflow-hidden rounded-md bg-surface-2">
      <div role="status" aria-live="polite" className={cn('px-3 text-xs text-warning', status ? 'py-2' : 'sr-only')}>{status}</div>
      <div ref={containerRef} role="log" aria-label="Coin chat" tabIndex={0} onScroll={handleScroll} className="overflow-y-auto p-1 outline-none focus-visible:outline-2 focus-visible:outline-focus" style={{ maxHeight: 420 }}>
        {loading ? (
          <div role="status" aria-busy="true" className="p-6 text-center text-xs text-text-faint">Loading messages...</div>
        ) : error ? (
          <div role="alert" className="p-6 text-center text-sm text-text-muted">Chat can&apos;t load right now. Try again in a moment.</div>
        ) : messages.length === 0 ? (
          <EmptyState icon={MessagesSquare} title="No messages yet" body="Start the conversation about this coin." className="p-6" />
        ) : (
          <>
            {messages.length > 200 + historyOffset && <Button size="sm" variant="ghost" fullWidth onClick={() => setHistoryOffset(value => value + 200)}>Earlier messages</Button>}
            {messages.slice(Math.max(0, messages.length - 200 - historyOffset), Math.max(0, messages.length - historyOffset)).map(msg => (
              <ChatMessage key={msg.id} msg={msg} onReply={setReplyTo} onRetry={retryMessage} onRevise={reviseMessage} wallet={wallet} replyTarget={msg.replyToId ? msgMap.get(msg.replyToId) : undefined} />
            ))}
            {historyOffset > 0 && <Button size="sm" variant="ghost" fullWidth onClick={() => setHistoryOffset(value => Math.max(0, value - 200))}>Later messages</Button>}
            <div ref={bottomRef} />
          </>
        )}
      </div>

      {replyTo && (
        <div className="flex items-center gap-2 bg-surface-3 px-3 py-1.5">
          <span className="min-w-0 flex-1 truncate text-xs text-text-muted">Replying to {shortAddress(replyTo.sender, 3, 3)}: {replyTo.content.slice(0, 50)}</span>
          <button type="button" onClick={() => setReplyTo(null)} className="text-xs text-text-faint transition-colors hover:text-danger">Cancel</button>
        </div>
      )}

      <div className="flex items-center gap-2 bg-surface-3 p-3">
        {isConnected ? (
          <>
            <label htmlFor={`pool-chat-input-${poolAddress.toLowerCase()}`} className="sr-only">Message</label>
            <input
              id={`pool-chat-input-${poolAddress.toLowerCase()}`}
              type="text"
              value={draftWallet.current === wallet ? input : ''}
              onChange={e => setInput(e.target.value)}
              onKeyDown={handleKeyDown}
              placeholder={isAuthenticated ? 'Add a comment' : 'Sign in to start chatting'}
              disabled={!isAuthenticated}
              maxLength={500}
              className="h-control-height-md min-w-0 flex-1 rounded-sm bg-surface-1 px-3 text-sm text-text outline-none transition-colors placeholder:text-text-faint focus-visible:outline-2 focus-visible:outline-focus disabled:text-text-disabled"
            />
            <Button variant="primary" size="md" onClick={() => { void handleSend(); }} disabled={!isAuthenticated || !input.trim()}>Send</Button>
          </>
        ) : (
          <p className="flex-1 py-2 text-center text-xs text-text-faint">Connect a wallet to chat</p>
        )}
      </div>
    </div>
  );
}
