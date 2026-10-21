import {
  recentRecipientsRepository,
  type RecentRecipientRecord,
} from '@/platform/storage/repositories';

const MAX = 5;

export type RecentRecipient = RecentRecipientRecord;

export function loadRecentRecipients(): RecentRecipient[] {
  return recentRecipientsRepository.read();
}

export function saveRecentRecipient(address: string, label?: string): void {
  const existing = loadRecentRecipients().filter(
    (r) => r.address.toLowerCase() !== address.toLowerCase(),
  );
  const updated: RecentRecipient[] = [
    { address, label, timestamp: Date.now() },
    ...existing,
  ].slice(0, MAX);
  recentRecipientsRepository.write(updated);
}
