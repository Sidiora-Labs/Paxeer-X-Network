'use client';

import { useState, useEffect } from 'react';
import { useAccount } from 'wagmi';
import Link from 'next/link';
import { getDmConversations, type DmConversation } from '@/core/clients/chat-api';
import { MessageCircle } from 'lucide-react';
import { formatAddress } from '@/utils/format';
import { isSignedOutError } from '@/core/market/unsupported';
import { Button, EmptyState, ErrorState, Skeleton } from '@/ui/primitives';
import { SignedOutState } from '@/ui/shared/signed-out-state';
import { publicProfile } from '@/widgets/profile/account-transport';

function relTime(ts: number | null): string {
  if (!ts) return '';
  const diff = Math.floor(Date.now() / 1000) - ts;
  if (diff < 60) return 'now';
  if (diff < 3600) return `${Math.floor(diff / 60)}m ago`;
  if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`;
  return `${Math.floor(diff / 86400)}d ago`;
}

interface PeerMeta {
  displayName: string;
}

export default function ConversationList() {
  const { address, isConnected } = useAccount();
  const [conversations, setConversations] = useState<DmConversation[]>([]);
  const [peerMeta, setPeerMeta] = useState<Record<string, PeerMeta>>({});
  const [loading, setLoading] = useState(true);
  const [scope, setScope] = useState<string>();
  const [error, setError] = useState<{ message: string; signedOut: boolean } | null>(null);
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    setConversations([]); setPeerMeta({}); setError(null); setLoading(true);
    if (!isConnected || !address) { setLoading(false); return; }
    let cancelled = false;

    async function load() {
      try {
        const convos = await getDmConversations();
        if (!cancelled) {
          setScope(address); setConversations(convos);
          // Peer display names: public GET /v2/profiles/{wallet} on the gateway; a 404 (no profile row) keeps the address.
          const peers = convos.map(c => c.peer).filter(Boolean);
          peers.forEach(peer => {
            publicProfile(peer)
              .then(response => {
                if (!cancelled) setPeerMeta(prev => ({ ...prev, [peer.toLowerCase()]: { displayName: response.data.profile.displayName } }));
              })
              .catch(() => {});
          });
        }
      } catch (e: unknown) {
        // A 401/403 means the wallet is connected but the app session is missing: that is the sign-in prompt, not an error.
        if (!cancelled) { if (!isSignedOutError(e)) console.error('[chat] conversations failed to load', e); setError({ message: 'Check your connection and try again', signedOut: isSignedOutError(e) }); }
      } finally {
        if (!cancelled) setLoading(false);
      }
    }

    load();
    return () => { cancelled = true; };
  }, [isConnected, address, attempt]);

  if (!isConnected || error?.signedOut) {
    return <SignedOutState icon={MessageCircle} title="Sign in to see your messages" body="Your direct messages with other traders show up here." />;
  }

  if (loading) {
    return <Skeleton variant="rows" rows={4} label="conversations" className="rounded-md bg-surface-1" />;
  }

  if (error) {
    return <ErrorState title="Can't load messages right now" message={error.message} onRetry={() => setAttempt(value => value + 1)} />;
  }

  if (conversations.length === 0) {
    return (
      <EmptyState
        icon={MessageCircle}
        title="No conversations yet"
        body="Open a trader's profile and send them a message"
        action={<Button variant="secondary" size="sm" href="/leaderboard">Find traders</Button>}
        announce
      />
    );
  }

  return (
    <div className="space-y-1">
      {(scope === address ? conversations : []).map(convo => {
        const meta = peerMeta[convo.peer.toLowerCase()];
        return (
          <Link
            key={convo.id}
            href={`/chat/${encodeURIComponent(convo.id)}`}
            className="flex items-center gap-3 px-4 py-3 rounded-lg hover:bg-surface-2 transition"
          >
            <div className="w-10 h-10 rounded-full bg-surface-2 flex-shrink-0 flex items-center justify-center overflow-hidden">
              <span className="text-xs font-bold text-text-faint">
                {convo.peer.slice(2, 4).toUpperCase()}
              </span>
            </div>
            <div className="flex-1 min-w-0">
              <div className="text-sm font-bold text-text truncate">
                {meta?.displayName || formatAddress(convo.peer, 6)}
              </div>
              <div className="text-2xs text-text-faint">
                {relTime(convo.lastMessageAt)}{convo.unreadCount > 0 ? ` · ${convo.unreadCount} unread` : ''}
              </div>
            </div>
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" className="text-text-faint flex-shrink-0">
              <polyline points="9 18 15 12 9 6"/>
            </svg>
          </Link>
        );
      })}
    </div>
  );
}
