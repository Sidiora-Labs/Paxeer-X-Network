'use client';

import { useState, useCallback } from 'react';
import {
  loadContacts,
  addContact,
  updateContact,
  deleteContact,
  searchContacts,
  getContactByAddress,
  type Contact,
} from '@/lib/contacts';

export function useContacts() {
  const [contacts, setContacts] = useState<Contact[]>(() => loadContacts());

  const refresh = useCallback(() => {
    setContacts(loadContacts());
  }, []);

  const add = useCallback((name: string, address: string, note?: string) => {
    const contact = addContact(name, address, note);
    refresh();
    return contact;
  }, [refresh]);

  const update = useCallback(
    (id: string, updates: Partial<Pick<Contact, 'name' | 'address' | 'note'>>) => {
      const contact = updateContact(id, updates);
      refresh();
      return contact;
    },
    [refresh],
  );

  const remove = useCallback((id: string) => {
    deleteContact(id);
    refresh();
  }, [refresh]);

  const search = useCallback((query: string) => {
    return searchContacts(query);
  }, []);

  const findByAddress = useCallback((address: string) => {
    return getContactByAddress(address);
  }, []);

  return { contacts, add, update, remove, search, findByAddress, refresh };
}
