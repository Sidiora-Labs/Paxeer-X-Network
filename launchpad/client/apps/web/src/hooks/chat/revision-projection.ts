/** Durable message projection: broker retries and delayed creates cannot revive tombstones. */
export interface RevisionMessage {
  id: string;
  sequence: string;
  revision: string;
  content: string;
  deleted: boolean;
}
export function projectMessage<T extends RevisionMessage>(
  current: ReadonlyMap<string, T>, incoming: T,
): ReadonlyMap<string, T> {
  if (!/^[1-9][0-9]{0,18}$/.test(incoming.sequence) || !/^[1-9][0-9]{0,18}$/.test(incoming.revision)) {
    throw new Error('Invalid durable message sequence or revision');
  }
  const previous = current.get(incoming.id);
  if (previous && BigInt(previous.revision) >= BigInt(incoming.revision)) return current;
  if (previous && previous.sequence !== incoming.sequence) throw new Error('Message sequence changed');
  const next = new Map(current);
  next.set(incoming.id, incoming.deleted ? { ...incoming, content: '' } : incoming);
  return next;
}
export function orderedMessages<T extends RevisionMessage>(rows: ReadonlyMap<string, T>): T[] {
  return [...rows.values()].sort((a,b) => BigInt(a.sequence) < BigInt(b.sequence) ? -1 : BigInt(a.sequence) > BigInt(b.sequence) ? 1 : a.id.localeCompare(b.id));
}
