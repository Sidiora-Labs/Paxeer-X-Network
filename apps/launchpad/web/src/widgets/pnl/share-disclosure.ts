import { sanitizedShare, publicationFields, type PublicShare, type PublicationField, type SharePreview } from './share-transport';
import { exactAmount, normalizeAddress, uint256Revision } from '../profile/account-disclosure';
export const shareFields = ['realized', 'unrealized', 'total'] as const;
export type ShareField = typeof shareFields[number];
export interface ShareSnapshot {
  snapshotId: string; accountingRevision: string; intervalStart: string; intervalEnd: string;
  wallet: string; realized: string | null; unrealized: string | null; total: string | null;
}
export interface ShareSelection { selectedFields: readonly ShareField[]; privacyMode?: 'anonymous' | 'wallet'; expiresAt: string }
/** Exact allowlist for preview; backend must apply same policy to public HTML/image. */
export function sharePreview(snapshot: ShareSnapshot, selection: ShareSelection) {
  if (!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(snapshot.snapshotId) || uint256Revision(snapshot.accountingRevision, true) < 1n) throw new Error('Immutable accounting snapshot required');
  for (const timestamp of [snapshot.intervalStart, snapshot.intervalEnd, selection.expiresAt]) {
    if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$/.test(timestamp) || !Number.isFinite(Date.parse(timestamp)) || new Date(timestamp).toISOString() !== timestamp) throw new Error('Canonical UTC timestamp required');
  }
  const start = Date.parse(snapshot.intervalStart), end = Date.parse(snapshot.intervalEnd), expiry = Date.parse(selection.expiresAt);
  if (!Number.isFinite(start) || !Number.isFinite(end) || end <= start || !Number.isFinite(expiry)) throw new Error('Invalid UTC interval/expiration');
  if (selection.selectedFields.length > 3 || selection.selectedFields.some(f => !shareFields.includes(f))) throw new Error('Invalid disclosure selection');
  const privacyMode = selection.privacyMode ?? 'anonymous';
  if (!['anonymous', 'wallet'].includes(privacyMode)) throw new Error('Invalid privacy mode');
  const figures = Object.fromEntries([...new Set(selection.selectedFields)].map(field => [field, exactAmount(snapshot[field])]));
  return { snapshotId: snapshot.snapshotId, accountingRevision: snapshot.accountingRevision, intervalStart: snapshot.intervalStart, intervalEnd: snapshot.intervalEnd, quoteCurrency: 'USDL', privacyMode, expiresAt: selection.expiresAt, publicationStatus: 'preview' as const, figures, ...(privacyMode === 'wallet' ? { wallet: normalizeAddress(snapshot.wallet) } : {}) };
}

export function utcTimestamp(value: string): number {
  const parsed = Date.parse(value);
  if (!Number.isFinite(parsed) || !/^\d{4}-\d{2}-\d{2}T/.test(value) || !value.endsWith('Z')) throw new Error('UTC timestamp required');
  return parsed;
}

export function checkedPublicShare(source: PublicShare): PublicShare {
  const projected = sanitizedShare(source);
  if (utcTimestamp(projected.expiresAt) <= Date.now()) throw new Error('Card withdrawn or expired');
  if (utcTimestamp(projected.rangeEnd) <= utcTimestamp(projected.rangeStart)) throw new Error('Invalid card accounting interval');
  if (projected.decimals !== 6 || projected.quoteIdentity.decimals !== 6 || projected.watermark.chainId !== '125') throw new Error('Invalid card denomination or chain');
  if (new Set(projected.fields).size !== projected.fields.length) throw new Error('Duplicate disclosure field');
  uint256Revision(projected.watermark.projectionRevision);
  for (const field of projected.fields) if (field !== 'historicalReturn' && projected.values[field] !== null) exactAmount(projected.values[field]!, 6);
  return projected;
}
export function checkedSharePreview(preview: SharePreview, selected: { fields: PublicationField[]; includeWallet: boolean; expiresAt: string }, wallet: string): SharePreview {
  if (preview.version !== 3 || preview.id !== preview.share.id || !preview.intent || utcTimestamp(preview.previewUntil) <= Date.now()) throw new Error('Invalid or expired publication preview');
  if (!selected.fields.length || selected.fields.some(field => !publicationFields.includes(field)) || new Set(selected.fields).size !== selected.fields.length) throw new Error('Select valid public figures');
  const share = checkedPublicShare(preview.share);
  if (share.fields.slice().sort().join(',') !== selected.fields.slice().sort().join(',') || utcTimestamp(share.expiresAt) !== utcTimestamp(selected.expiresAt)) throw new Error('Preview does not match selected disclosure');
  if (selected.includeWallet ? !share.wallet || normalizeAddress(share.wallet) !== normalizeAddress(wallet) : !!share.wallet) throw new Error('Preview wallet disclosure does not match selection');
  if (!selected.includeWallet && JSON.stringify(share).toLowerCase().includes(normalizeAddress(wallet))) throw new Error('Anonymous preview contains wallet attribution');
  return { version: 3, id: preview.id, intent: preview.intent, previewUntil: preview.previewUntil, share };
}
