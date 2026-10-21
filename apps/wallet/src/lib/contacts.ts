import {
  contactsRepository,
  type ContactRecord,
} from '@/platform/storage/repositories';

export type Contact = ContactRecord;

function generateId(): string {
  return `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
}

// ── Read ────────────────────────────────────────────────────────────────────

export function loadContacts(): Contact[] {
  return contactsRepository.read();
}

function persist(contacts: Contact[]): void {
  contactsRepository.write(contacts);
}

// ── Create ──────────────────────────────────────────────────────────────────

export function addContact(name: string, address: string, note?: string): Contact {
  const contacts = loadContacts();

  const normalizedAddr = address.trim().toLowerCase();
  if (contacts.some((c) => c.address.toLowerCase() === normalizedAddr)) {
    throw new Error('A contact with this address already exists');
  }

  const now = Date.now();
  const contact: Contact = {
    id: generateId(),
    name: name.trim(),
    address: address.trim(),
    note: note?.trim() || undefined,
    createdAt: now,
    updatedAt: now,
  };
  contacts.push(contact);
  persist(contacts);
  return contact;
}

// ── Update ──────────────────────────────────────────────────────────────────

export function updateContact(
  id: string,
  updates: Partial<Pick<Contact, 'name' | 'address' | 'note'>>,
): Contact {
  const contacts = loadContacts();
  const idx = contacts.findIndex((c) => c.id === id);
  if (idx === -1) throw new Error('Contact not found');

  if (updates.address) {
    const normalizedAddr = updates.address.trim().toLowerCase();
    const duplicate = contacts.find(
      (c) => c.id !== id && c.address.toLowerCase() === normalizedAddr,
    );
    if (duplicate) throw new Error('Another contact already uses this address');
  }

  const contact = contacts[idx];
  if (updates.name !== undefined) contact.name = updates.name.trim();
  if (updates.address !== undefined) contact.address = updates.address.trim();
  if (updates.note !== undefined) contact.note = updates.note.trim() || undefined;
  contact.updatedAt = Date.now();

  contacts[idx] = contact;
  persist(contacts);
  return contact;
}

// ── Delete ──────────────────────────────────────────────────────────────────

export function deleteContact(id: string): void {
  const contacts = loadContacts();
  const filtered = contacts.filter((c) => c.id !== id);
  if (filtered.length === contacts.length) throw new Error('Contact not found');
  persist(filtered);
}

// ── Search ──────────────────────────────────────────────────────────────────

export function searchContacts(query: string): Contact[] {
  if (!query.trim()) return loadContacts();
  const q = query.trim().toLowerCase();
  return loadContacts().filter(
    (c) =>
      c.name.toLowerCase().includes(q) ||
      c.address.toLowerCase().includes(q) ||
      (c.note && c.note.toLowerCase().includes(q)),
  );
}

// ── Lookup ──────────────────────────────────────────────────────────────────

export function getContactByAddress(address: string): Contact | undefined {
  const normalizedAddr = address.trim().toLowerCase();
  return loadContacts().find((c) => c.address.toLowerCase() === normalizedAddr);
}

export function getContactById(id: string): Contact | undefined {
  return loadContacts().find((c) => c.id === id);
}
