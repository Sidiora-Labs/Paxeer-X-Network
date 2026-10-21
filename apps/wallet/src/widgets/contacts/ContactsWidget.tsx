'use client';

import { useState, useEffect } from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import { CheckCircle } from 'lucide-react';
import { SvgIcon } from '@/components/ui/SvgIcon';
import { useContacts } from '@/hooks/useContacts';
import type { Contact } from '@/lib/contacts';
import { ContactFormView } from './ContactFormView';
import { useLocale } from '@/providers/LocaleProvider';
import Image from "next/image";

interface ContactsWidgetProps {
    onBack: () => void;
}

type ContactsView = 'list' | 'add' | 'edit';

export function ContactsWidget({ onBack }: ContactsWidgetProps) {
    const { contacts, add, update, remove, search } = useContacts();
    const { t } = useLocale();

    const [view, setView] = useState<ContactsView>('list');
    const [searchQuery, setSearchQuery] = useState('');
    const [editingContact, setEditingContact] = useState<Contact | null>(null);

    const [formName, setFormName] = useState('');
    const [formAddress, setFormAddress] = useState('');
    const [formNote, setFormNote] = useState('');
    const [formError, setFormError] = useState('');
    const [deleteConfirmId, setDeleteConfirmId] = useState<string | null>(null);
    const [savedToast, setSavedToast] = useState<string | null>(null);

    useEffect(() => {
        if (!savedToast) return;
        const timer = setTimeout(() => setSavedToast(null), 2500);
        return () => clearTimeout(timer);
    }, [savedToast]);

    const filteredContacts = searchQuery ? search(searchQuery) : contacts;

    const resetForm = () => {
        setFormName(''); setFormAddress(''); setFormNote(''); setFormError(''); setEditingContact(null);
    };

    const openAdd = () => { resetForm(); setView('add'); };

    const openEdit = (contact: Contact) => {
        setEditingContact(contact);
        setFormName(contact.name);
        setFormAddress(contact.address);
        setFormNote(contact.note || '');
        setFormError('');
        setView('edit');
    };

    const handleSave = () => {
        setFormError('');
        if (!formName.trim()) { setFormError(t.contacts.nameRequired); return; }
        if (!formAddress.trim()) { setFormError(t.contacts.addressRequired); return; }
        if (!/^0x[a-fA-F0-9]{40}$/.test(formAddress.trim())) { setFormError(t.contacts.invalidAddress); return; }
        try {
            if (view === 'add') {
                add(formName, formAddress, formNote || undefined);
                setSavedToast(`${formName} ${t.contacts.contactAdded}`);
            } else if (view === 'edit' && editingContact) {
                update(editingContact.id, { name: formName, address: formAddress, note: formNote || undefined });
                setSavedToast(`${formName} ${t.contacts.contactUpdated}`);
            }
            resetForm(); setView('list');
        } catch (e: unknown) {
            setFormError((e as Error).message || t.common.error);
        }
    };

    const handleDelete = (id: string) => {
        try {
            remove(id);
            setDeleteConfirmId(null);
            if (view === 'edit') { resetForm(); setView('list'); }
        } catch (e: unknown) {
            setFormError((e as Error).message || t.common.error);
        }
    };

    if (view === 'add' || view === 'edit') {
        return (
            <ContactFormView
                view={view}
                editingContact={editingContact}
                formName={formName} setFormName={setFormName}
                formAddress={formAddress} setFormAddress={setFormAddress}
                formNote={formNote} setFormNote={setFormNote}
                formError={formError}
                deleteConfirmId={deleteConfirmId} setDeleteConfirmId={setDeleteConfirmId}
                onSave={handleSave}
                onBack={() => { resetForm(); setView('list'); }}
                onDelete={handleDelete}
            />
        );
    }

    return (
        <div className="min-h-screen flex flex-col px-4 pt-4 safe-area-pt">
            {/* Saved toast */}
            <AnimatePresence>
                {savedToast && (
                    <motion.div
                        initial={{ opacity: 0, y: -12, scale: 0.95 }}
                        animate={{ opacity: 1, y: 0, scale: 1 }}
                        exit={{ opacity: 0, y: -8, scale: 0.95 }}
                        transition={{ duration: 0.25 }}
                        className="fixed top-[calc(env(safe-area-inset-top,0px)+70px)] left-1/2 -translate-x-1/2 z-50"
                    >
                        <div className="flex items-center gap-2 px-4 py-2.5 rounded-full bg-pax-card   shadow-2xl">
                            <CheckCircle className="w-4 h-4 text-green-400 shrink-0" />
                            <span className="text-xs font-medium">{savedToast}</span>
                        </div>
                    </motion.div>
                )}
            </AnimatePresence>
            <div className="flex items-center gap-3 mb-4">
                <button type="button" onClick={onBack} aria-label="Back" className="p-2 -ml-2 press-scale">
                    <SvgIcon name="arrow-left" className="w-5 h-5" style={{ filter: 'brightness(0) invert(0.6)' }} />
                </button>
                <h2 className="text-lg font-bold flex-1">{t.contacts.title}</h2>
                <button onClick={openAdd} className="p-2 rounded-full bg-pax-accent/10 press-scale" aria-label={t.contacts.addContact}>
                    <SvgIcon name="plus" className="w-4.5 h-4.5" style={{ filter: 'brightness(0) saturate(100%) invert(22%) sepia(93%) saturate(7388%) hue-rotate(222deg) brightness(98%) contrast(101%)' }} />
                </button>
            </div>

            {contacts.length > 0 && (
                <div className="relative mb-4">
                    <Image src="/wallet/ui_icons/search.svg" alt="" className="absolute left-3.5 top-1/2 -translate-y-1/2 w-4 h-4" style={{ filter: 'brightness(0) invert(0.6)' }} width={16} height={16} />
                    <input
                        type="text" value={searchQuery} onChange={(e) => setSearchQuery(e.target.value)}
                        placeholder={t.contacts.searchPlaceholder}
                        className="w-full pl-10 pr-4 py-3 rounded-xl bg-white/5   text-sm outline-none  transition-colors placeholder:text-white/20"
                    />
                    {searchQuery && (
                        <button onClick={() => setSearchQuery('')} className="absolute right-3 top-1/2 -translate-y-1/2 p-0.5">
                            <SvgIcon name="x" className="w-3.5 h-3.5" style={{ filter: 'brightness(0) invert(0.6)' }} />
                        </button>
                    )}
                </div>
            )}

            {contacts.length === 0 && (
                <div className="flex-1 flex flex-col items-center justify-center gap-4 pb-24">
                    <div className="w-16 h-16 rounded-full bg-white/5 flex items-center justify-center">
                        <SvgIcon name="user" className="w-7 h-7" style={{ filter: 'brightness(0) invert(0.6)' }} />
                    </div>
                    <div className="text-center">
                        <p className="text-sm font-medium mb-1">{t.contacts.noContacts}</p>
                        <p className="text-xs text-pax-muted">{t.contacts.noContactsHint}</p>
                    </div>
                    <button onClick={openAdd} className="flex items-center gap-2 px-5 py-2.5 rounded-full bg-pax-accent text-black text-sm font-semibold press-scale">
                        <SvgIcon name="plus" className="w-4 h-4" style={{ filter: 'brightness(0)' }} />
                        {t.contacts.addContact}
                    </button>
                </div>
            )}

            {contacts.length > 0 && (
                <div className="flex-1 pb-24">
                    {filteredContacts.length === 0 ? (
                        <div className="flex flex-col items-center py-12 gap-2">
                            <p className="text-sm text-white/50">{t.contacts.noResults} “{searchQuery}”</p>
                        </div>
                    ) : (
                        <div className="space-y-1.5">
                            {filteredContacts.map((c) => (
                                <div key={c.id} className="flex items-center gap-3 px-3.5 py-3.5 rounded-xl bg-white/5 hover:bg-white/8 transition-all group">
                                    <div className="w-10 h-10 rounded-full bg-pax-accent/10 flex items-center justify-center shrink-0">
                                        <span className="text-sm font-bold text-pax-accent">{c.name[0]?.toUpperCase() || '?'}</span>
                                    </div>
                                    <div className="flex-1 min-w-0">
                                        <p className="text-sm font-medium truncate">{c.name}</p>
                                        <p className="text-[11px] text-pax-muted font-mono truncate">{c.address.slice(0, 6)}...{c.address.slice(-4)}</p>
                                        {c.note && <p className="text-[10px] text-pax-muted/60 truncate mt-0.5">{c.note}</p>}
                                    </div>
                                    <div className="flex items-center gap-1 shrink-0 opacity-60 group-hover:opacity-100 transition-opacity">
                                        <button onClick={() => navigator.clipboard.writeText(c.address).catch(() => { })} className="p-2 rounded-lg hover:bg-white/10 press-scale" aria-label={t.a11y.copyAddress}>
                                            <SvgIcon name="copy" className="w-3.5 h-3.5" style={{ filter: 'brightness(0) invert(0.6)' }} />
                                        </button>
                                        <button onClick={() => openEdit(c)} className="p-2 rounded-lg hover:bg-white/10 press-scale" aria-label={t.contacts.editContact}>
                                            <SvgIcon name="pencil" className="w-3.5 h-3.5" style={{ filter: 'brightness(0) invert(0.6)' }} />
                                        </button>
                                    </div>
                                </div>
                            ))}
                        </div>
                    )}
                </div>
            )}
        </div>
    );
}
