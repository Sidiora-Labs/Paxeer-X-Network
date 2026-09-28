'use client';

import { SvgIcon } from '@/components/ui/SvgIcon';
import type { Contact } from '@/lib/contacts';
import { useLocale } from '@/providers/LocaleProvider';

interface ContactFormViewProps {
  view: 'add' | 'edit';
  editingContact: Contact | null;
  formName: string;
  setFormName: (v: string) => void;
  formAddress: string;
  setFormAddress: (v: string) => void;
  formNote: string;
  setFormNote: (v: string) => void;
  formError: string;
  deleteConfirmId: string | null;
  setDeleteConfirmId: (v: string | null) => void;
  onSave: () => void;
  onBack: () => void;
  onDelete: (id: string) => void;
}

export function ContactFormView({
  view, editingContact,
  formName, setFormName, formAddress, setFormAddress, formNote, setFormNote,
  formError, deleteConfirmId, setDeleteConfirmId, onSave, onBack, onDelete,
}: ContactFormViewProps) {
  const { t } = useLocale();

  return (
    <div className="min-h-screen flex flex-col px-4 pt-4 safe-area-pt">
      <div className="flex items-center gap-3 mb-6">
        <button type="button" onClick={onBack} aria-label="Back" className="p-2 -ml-2 press-scale">
          <SvgIcon name="arrow-left" className="w-5 h-5" style={{ filter: 'brightness(0) invert(0.6)' }} />
        </button>
        <h2 className="text-lg font-bold">{view === 'add' ? t.contacts.newContact : t.contacts.editContact}</h2>
        {view === 'edit' && editingContact && (
          <button onClick={() => setDeleteConfirmId(editingContact.id)} className="ml-auto p-2 press-scale" aria-label={t.contacts.deleteContact}>
            <SvgIcon name="trash" className="w-4.5 h-4.5" style={{ filter: 'invert(48%) sepia(79%) saturate(2476%) hue-rotate(338deg) brightness(118%) contrast(119%)' }} />
          </button>
        )}
      </div>

      <div className="flex-1 flex flex-col gap-4">
        <div>
          <label className="text-xs text-pax-muted mb-1.5 block">{t.contacts.name}</label>
          <input
            type="text"
            value={formName}
            onChange={(e) => setFormName(e.target.value)}
            placeholder={t.contacts.namePlaceholder}
            maxLength={50}
            className="w-full px-4 py-3.5 rounded-xl bg-white/5   text-sm outline-none  transition-colors placeholder:text-white/20"
          />
        </div>

        <div>
          <label className="text-xs text-pax-muted mb-1.5 block">{t.contacts.walletAddress}</label>
          <input
            type="text"
            value={formAddress}
            onChange={(e) => setFormAddress(e.target.value)}
            placeholder="0x..."
            className="w-full px-4 py-3.5 rounded-xl bg-white/5   text-sm font-mono outline-none  transition-colors placeholder:text-white/20"
          />
        </div>

        <div>
          <label className="text-xs text-pax-muted mb-1.5 block">{t.contacts.note} <span className="text-white/20">({t.contacts.optional})</span></label>
          <input
            type="text"
            value={formNote}
            onChange={(e) => setFormNote(e.target.value)}
            placeholder={t.contacts.notePlaceholder}
            maxLength={100}
            className="w-full px-4 py-3.5 rounded-xl bg-white/5   text-sm outline-none  transition-colors placeholder:text-white/20"
          />
        </div>

        {formError && (
          <div className="flex items-center gap-2">
            <SvgIcon name="warning" className="w-3.5 h-3.5" style={{ filter: 'invert(48%) sepia(79%) saturate(2476%) hue-rotate(338deg) brightness(118%) contrast(119%)' }} />
            <p className="text-xs text-red-400">{formError}</p>
          </div>
        )}
      </div>

      <div className="py-4 pb-24">
        <button
          onClick={onSave}
          className="w-full flex items-center justify-center gap-2 py-3.5 rounded-2xl bg-pax-accent text-black font-semibold text-sm press-scale transition-all"
        >
          <SvgIcon name="check" className="w-4 h-4" style={{ filter: 'brightness(0)' }} />
          {view === 'add' ? t.contacts.saveContact : t.contacts.updateContact}
        </button>
      </div>

      {deleteConfirmId && (
        <div className="fixed inset-0 z-50 flex items-center justify-center">
          <div className="absolute inset-0 bg-black/60 backdrop-blur-sm" onClick={() => setDeleteConfirmId(null)} />
          <div className="relative w-[90%] max-w-xs bg-pax-card rounded-2xl p-5 space-y-4 animate-scale-in">
            <h3 className="text-base font-bold text-center">{t.contacts.deleteContact}?</h3>
            <p className="text-xs text-pax-muted text-center">
              {t.contacts.deleteConfirm}
            </p>
            <div className="flex gap-3">
              <button onClick={() => setDeleteConfirmId(null)} className="flex-1 py-2.5 rounded-xl bg-white/5 text-sm font-medium press-scale">
                {t.common.cancel}
              </button>
              <button onClick={() => onDelete(deleteConfirmId)} className="flex-1 py-2.5 rounded-xl bg-red-500/15 text-red-400 text-sm font-medium press-scale">
                {t.common.confirm}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
