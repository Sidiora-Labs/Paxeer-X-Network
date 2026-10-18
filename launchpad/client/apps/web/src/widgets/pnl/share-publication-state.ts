import { sharePreview, type ShareSelection, type ShareSnapshot } from './share-disclosure';
export type PublicationState =
  | { kind: 'private' }
  | { kind: 'preview'; preview: ReturnType<typeof sharePreview>; mutationId: string }
  | { kind: 'unavailable'; reason: string };
export function prepareDisclosure(snapshot: ShareSnapshot, selection: ShareSelection, mutationId: string): PublicationState {
  if (!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(mutationId)) throw new Error('Publication mutation identity required');
  if (Date.parse(selection.expiresAt) <= Date.now()) throw new Error('Publication expiration must be in the future');
  return { kind: 'preview', preview: sharePreview(snapshot, selection), mutationId };
}
