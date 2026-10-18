'use client';
import { FileText, Globe, Link as LinkIcon, MessageCircle, Send } from 'lucide-react';
import { Chip, EmptyState, Skeleton, UnavailableState } from '@/ui/primitives';
import { usePoolSnapshot } from '@/core/market/use-pool-snapshot';

export function safeSocialUrl(raw: string, approvedOrigins: readonly string[] = (process.env.NEXT_PUBLIC_MARKET_SOCIAL_ORIGINS ?? '').split(',').map(value => value.trim()).filter(Boolean)): string | null {
  try {
    const url = new URL(raw);
    const approved = approvedOrigins.some(origin => { try { const configured = new URL(origin); return configured.protocol === 'https:' && configured.origin === origin && configured.origin === url.origin; } catch { return false; } });
    return approved && url.protocol === 'https:' && !url.username && !url.password ? url.href : null;
  } catch { return null; }
}

function linkIcon(label: string, url: string) {
  const text = `${label} ${url}`.toLowerCase();
  if (/telegram|t\.me/.test(text)) return Send;
  if (/discord|chat/.test(text)) return MessageCircle;
  if (/web|site|home/.test(text)) return Globe;
  return LinkIcon;
}

export default function TokenDescription({ poolAddress }: { poolAddress: string }) {
  const snapshot = usePoolSnapshot(poolAddress);
  if (snapshot.isPending) return <Skeleton variant="block" height={96} label="description" />;
  if (snapshot.isError) {
    // Compact, so the rail Description card keeps its height when the snapshot fails.
    const status = (snapshot.error as { status?: unknown } | null)?.status;
    return <UnavailableState variant="chip" wrap title="The description" plural="auto" operation="IF_POOL_DETAIL" status={typeof status === 'number' ? status : null} onRetry={() => { void snapshot.refetch(); }} className="self-start" />;
  }
  const content = snapshot.data.metadata;
  if (!content) {
    const marker = snapshot.data.unavailable.metadata;
    return marker ? <UnavailableState variant="inline" title="The description" operation={marker.operation} status={marker.status} /> : <EmptyState variant="inline" icon={FileText} title="No description yet" body="The creator hasn't added one." />;
  }
  if (content.token.address.toLowerCase() !== snapshot.data.pool.tokenAddress.toLowerCase()) return <p role="alert" className="text-sm text-danger">This description can&apos;t be shown right now.</p>;
  return <section aria-label="Description" className="flex flex-col gap-3">
    <p className="whitespace-pre-wrap break-words text-sm leading-6 text-text">{content.description || <span className="text-text-faint">No description yet</span>}</p>
    {content.tags.length ? <ul className="flex flex-wrap gap-1.5" aria-label="Tags">{content.tags.map(tag => <li key={tag}><Chip variant="token">{tag}</Chip></li>)}</ul> : null}
    {content.socialLinks.length ? <ul className="flex flex-wrap gap-2" aria-label="Links">
      {content.socialLinks.map(link => {
        const href = safeSocialUrl(link.url);
        const Icon = linkIcon(link.label, link.url);
        return <li key={`${link.label}:${link.url}`}>
          {href
            ? <a href={href} target="_blank" rel="noopener noreferrer nofollow" className="inline-flex h-control-height-sm items-center gap-1.5 rounded-pill bg-surface-2 px-3 text-sm font-bold text-text transition-colors hover:bg-surface-hover"><Icon aria-hidden="true" className="size-icon-sm text-text-muted" />{link.label}<span className="sr-only">, opens in a new tab</span></a>
            : <span className="inline-flex h-control-height-sm items-center gap-1.5 rounded-pill bg-surface-2 px-3 text-sm text-text-disabled" title="This link can't be opened"><Icon aria-hidden="true" className="size-icon-sm" />{link.label}<span className="sr-only">, link can't be opened</span></span>}
        </li>;
      })}
    </ul> : null}
    <p className="text-xs text-text-faint">Metadata revision {content.revision}</p>
  </section>;
}
