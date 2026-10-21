'use client';

import { useCallback, useEffect, useRef, useState } from 'react';
import { ImagePlus, RefreshCw, Trash2 } from 'lucide-react';
import { cn } from '@/lib/utils';
import { Button, FormField, NoticeBar, ModalFrame, humanError } from '@/ui/primitives';
import { useAccountSession } from './use-account-session';
import { AccountApiError, UNSAVED_PROFILE_REVISION, editableExtendedProfile, saveExtendedProfile, uploadAccountAsset, readAccountAsset, type ExtendedProfile, type ExtendedProfileBody, type AccountAsset } from './account-transport';
import { actionFailureMessage } from '@/core/market/unsupported';
import { forgetProfileAbsence } from '@/core/market/profile-absence';
import { validatedExtendedProfile } from './profile-edit-state';
import type { ProfileEditField } from './profile-header';

interface Props {
  open: boolean;
  onClose: () => void;
  onSaved: () => void;
  /** Field that receives focus when the dialog opens (the banner camera opens straight on the banner picker). */
  initialField?: ProfileEditField | null;
}

type Draft = { displayName: string; bio: string; website: string; twitter: string; telegram: string; discord: string; avatarAssetId: string | null; bannerAssetId: string | null };
type ImageKind = 'avatarAssetId' | 'bannerAssetId';
type Upload = { kind: ImageKind; id: string; file: File; asset?: AccountAsset; pending: boolean };
type TextName = 'displayName' | 'bio' | 'website' | 'twitter' | 'telegram' | 'discord';

const emptyDraft: Draft = { displayName: '', bio: '', website: '', twitter: '', telegram: '', discord: '', avatarAssetId: null, bannerAssetId: null };
const textFields: Array<{ name: TextName; label: string; max: number; multiline?: boolean; helper?: string; optional?: boolean }> = [
  { name: 'displayName', label: 'Display name', max: 50 },
  { name: 'bio', label: 'Bio', max: 500, multiline: true, optional: true },
  { name: 'website', label: 'Website', max: 2048, optional: true, helper: 'Starts with https://' },
  { name: 'twitter', label: 'X', max: 2048, optional: true, helper: 'Link to your X profile' },
  { name: 'telegram', label: 'Telegram', max: 2048, optional: true, helper: 'Starts with https://' },
  { name: 'discord', label: 'Discord', max: 2048, optional: true, helper: 'Invite link' },
];
const autofocus = { 'data-autofocus': 'true' } as Record<string, string>;

function fromProfile(p: ExtendedProfile): Draft {
  return { displayName: p.displayName, bio: p.bio, website: p.website ?? '', twitter: p.socials?.twitter ?? '', telegram: p.socials?.telegram ?? '', discord: p.socials?.discord ?? '', avatarAssetId: p.avatarAssetId, bannerAssetId: p.bannerAssetId };
}

function focusTarget(field: ProfileEditField | null | undefined): TextName | ImageKind | null {
  if (field === 'avatar') return 'avatarAssetId';
  if (field === 'banner') return 'bannerAssetId';
  if (field === 'socials') return 'twitter';
  return field ?? null;
}

export default function ProfileEditModal({ open, onClose, onSaved, initialField = null }: Props) {
  const { session } = useAccountSession();
  const [draft, setDraft] = useState<Draft>(emptyDraft);
  const [baseline, setBaseline] = useState<Draft>(emptyDraft);
  const [revision, setRevision] = useState<string | null>(null);
  const [state, setState] = useState<'editing' | 'loading' | 'saving' | 'committed' | 'conflict'>('editing');
  const [error, setError] = useState<string | null>(null);
  const [fields, setFields] = useState<readonly { pointer: string; rule: string }[]>([]);
  const [discard, setDiscard] = useState(false);
  const [upload, setUpload] = useState<Upload | null>(null);
  const generation = useRef(0);
  const target = focusTarget(initialField);
  const invalidate = useCallback(() => { generation.current++; }, []);

  useEffect(() => {
    if (!open || !session) return;
    const observed = ++generation.current;
    const abort = new AbortController();
    setState('loading'); setError(null); setUpload(null); setRevision(null); setDiscard(false); setFields([]);
    // A wallet with no saved profile reads as an empty one at revision 0; the first save creates it.
    editableExtendedProfile(session.session.wallet, abort.signal).then(profile => {
      if (observed !== generation.current) return;
      const next = fromProfile(profile);
      setDraft(next); setBaseline(next); setRevision(profile.revision); setState('editing');
    }).catch(e => {
      if (!abort.signal.aborted) { setError(actionFailureMessage('Profile editing', e)); setState('editing'); }
    });
    return () => { abort.abort(); invalidate(); };
  }, [open, session, invalidate]);

  if (!open || !session) return null;
  const dirty = JSON.stringify(draft) !== JSON.stringify(baseline) || !!upload;
  const requestClose = () => { if (dirty) setDiscard(true); else onClose(); };

  async function transfer(selected: Upload) {
    if (!session) return;
    const observed = generation.current;
    setUpload({ ...selected, pending: true }); setError(null);
    try {
      const asset = await uploadAccountAsset(session.session.wallet, selected.id, selected.file);
      if (observed !== generation.current) return;
      setUpload({ ...selected, asset, pending: false });
      if (asset.state === 'approved') setDraft(previous => ({ ...previous, [selected.kind]: asset.assetId }));
      else if (asset.state === 'rejected') setError("That image wasn't approved. Pick another one");
    } catch (e) {
      if (observed !== generation.current) return;
      setUpload({ ...selected, pending: false });
      setError(actionFailureMessage('Image upload', e));
    }
  }

  function selectImage(kind: ImageKind, file: File | undefined) {
    if (!file) return;
    if (!['image/png', 'image/jpeg', 'image/webp'].includes(file.type) || file.size > 6291456 || file.size === 0) {
      setError('Pick a PNG, JPEG or WebP under 6 MB');
      return;
    }
    void transfer({ kind, id: crypto.randomUUID(), file, pending: false });
  }

  async function refreshImage() {
    if (!session || !upload) return;
    const selected = upload, observed = generation.current;
    try {
      const asset = await readAccountAsset(session.session.wallet, selected.id);
      if (observed !== generation.current) return;
      setUpload({ ...selected, asset });
      if (asset.state === 'approved') setDraft(previous => ({ ...previous, [selected.kind]: asset.assetId }));
    } catch (e) {
      if (observed === generation.current) setError(humanError(e));
    }
  }

  async function save() {
    if (!session || !revision || state === 'saving') return;
    const observed = generation.current;
    setState('saving'); setError(null); setFields([]);
    try {
      const body: ExtendedProfileBody = { expectedRevision: revision, displayName: draft.displayName, bio: draft.bio, website: draft.website || null, avatarAssetId: draft.avatarAssetId, bannerAssetId: draft.bannerAssetId, socials: { twitter: draft.twitter || null, telegram: draft.telegram || null, discord: draft.discord || null } };
      const result = await saveExtendedProfile(session.session.wallet, validatedExtendedProfile(body));
      if (observed !== generation.current) return;
      if (BigInt(result.data.profile.revision) <= BigInt(revision)) throw new Error('Acknowledgment did not advance revision');
      forgetProfileAbsence(session.session.wallet);
      setBaseline(draft); setRevision(result.data.profile.revision); setUpload(null); setState('committed'); onSaved();
    } catch (e) {
      if (observed !== generation.current) return;
      setState(e instanceof AccountApiError && e.status === 412 ? 'conflict' : 'editing');
      setError(e instanceof AccountApiError && e.fields.length > 0 ? 'Check the highlighted fields' : actionFailureMessage('Saving your profile', e));
      if (e instanceof AccountApiError) setFields(e.fields);
    }
  }

  async function reviewLatest() {
    if (!session) return;
    const observed = generation.current;
    try {
      const profile = await editableExtendedProfile(session.session.wallet);
      if (observed !== generation.current) return;
      setRevision(profile.revision); setBaseline(fromProfile(profile)); setState('editing');
      setError('Loaded your saved profile. Your edits are still here');
    } catch (e) {
      setError(humanError(e));
    }
  }

  const fieldError = (name: TextName) => fields.find(f => f.pointer === `/${name}` || f.pointer === `/socials/${name}`)?.rule ?? null;
  const saveDisabled = !revision || !dirty || state === 'saving' || state === 'conflict' || state === 'loading' || (!!upload && upload.asset?.state !== 'approved');

  const footer = discard ? (
    <div className="flex w-full flex-wrap items-center justify-between gap-2">
      <p className="text-sm text-text-muted">Discard unsaved changes?</p>
      <div className="flex gap-2">
        <Button variant="secondary" onClick={() => setDiscard(false)}>Keep editing</Button>
        <Button variant="danger" onClick={onClose}>Discard</Button>
      </div>
    </div>
  ) : (
    <>
      <Button variant="ghost" onClick={requestClose}>Close</Button>
      <Button variant="primary" disabled={saveDisabled} loading={state === 'saving'} onClick={() => { void save(); }}>Save changes</Button>
    </>
  );

  return (
    <ModalFrame open={open} onClose={requestClose} title="Edit profile" subtitle={revision === UNSAVED_PROFILE_REVISION ? 'Set up your profile' : revision ? 'Your public profile' : state === 'loading' ? 'Loading...' : "Can't load your profile right now"} size="md" footer={footer}>
      <div className="flex flex-col gap-4">
        <div className="grid gap-3 sm:grid-cols-2">
          {(['avatarAssetId', 'bannerAssetId'] as const).map(kind => (
            <ImagePicker
              key={kind}
              kind={kind}
              currentAssetId={draft[kind]}
              upload={upload?.kind === kind ? upload : null}
              disabled={upload?.pending === true || state === 'loading'}
              autoFocus={target === kind}
              onSelect={file => selectImage(kind, file)}
              onRefresh={() => { void refreshImage(); }}
              onRetry={() => upload && void transfer({ ...upload, id: upload.asset?.state === 'rejected' ? crypto.randomUUID() : upload.id })}
              onRemove={() => setUpload(null)}
            />
          ))}
        </div>
        {textFields.map(field => (
          <FormField
            key={field.name}
            id={`profile-edit-${field.name}`}
            label={field.label}
            optional={field.optional}
            helper={field.helper}
            error={fieldError(field.name)}
            multiline={field.multiline}
            surface="surface-2"
            counter={field.name === 'displayName' || field.name === 'bio' ? { count: [...draft[field.name]].length, max: field.max } : undefined}
            inputProps={field.multiline ? undefined : { value: draft[field.name], maxLength: field.max, disabled: state === 'loading', onChange: e => setDraft({ ...draft, [field.name]: e.target.value }), ...(target === field.name ? autofocus : {}) }}
            textareaProps={field.multiline ? { value: draft[field.name], maxLength: field.max, disabled: state === 'loading', onChange: e => setDraft({ ...draft, [field.name]: e.target.value }), ...(target === field.name ? autofocus : {}) } : undefined}
          />
        ))}
        {error ? <NoticeBar tone={state === 'committed' ? 'info' : 'danger'} surface={2} announce>{error}</NoticeBar> : null}
        {state === 'conflict' ? (
          <NoticeBar tone="warning" variant="card" surface={2} title="Changed on another device" announce action={<Button variant="secondary" size="sm" onClick={() => { void reviewLatest(); }}>Load latest</Button>}>
            Your draft is retained. Load the saved values to compare before saving again.
          </NoticeBar>
        ) : null}
        {state === 'committed' ? <p role="status" className="text-xs text-text-faint">Profile saved</p> : null}
        {dirty && state !== 'loading' ? (
          <details className="rounded-md bg-surface-3 p-3 text-xs text-text-muted">
            <summary className="cursor-pointer text-sm font-bold text-text">Currently saved</summary>
            <dl className="mt-2 grid gap-1">
              <div className="flex gap-2"><dt className="text-text-faint">Name</dt><dd className="truncate">{baseline.displayName || '—'}</dd></div>
              <div className="flex gap-2"><dt className="text-text-faint">Bio</dt><dd className="truncate">{baseline.bio || '—'}</dd></div>
              <div className="flex gap-2"><dt className="text-text-faint">Website</dt><dd className="truncate">{baseline.website || '—'}</dd></div>
            </dl>
          </details>
        ) : null}
      </div>
    </ModalFrame>
  );
}

interface ImagePickerProps {
  kind: ImageKind;
  currentAssetId: string | null;
  upload: Upload | null;
  disabled: boolean;
  autoFocus: boolean;
  onSelect: (file: File | undefined) => void;
  onRefresh: () => void;
  onRetry: () => void;
  onRemove: () => void;
}

function ImagePicker({ kind, currentAssetId, upload, disabled, autoFocus, onSelect, onRefresh, onRetry, onRemove }: ImagePickerProps) {
  const inputRef = useRef<HTMLInputElement | null>(null);
  const label = kind === 'avatarAssetId' ? 'Avatar' : 'Banner';
  const inputId = `profile-edit-${kind}`;
  const status = upload ? (upload.pending ? 'Uploading' : upload.asset?.state === 'approved' ? 'Approved' : upload.asset?.state === 'rejected' ? 'Rejected' : upload.asset ? 'Under review' : 'Upload failed') : currentAssetId ? 'Current image' : 'No image';
  const preview = upload?.asset?.state === 'approved' ? upload.asset.publicUrl : null;
  return (
    <div className="flex flex-col gap-2 rounded-md bg-surface-3 p-3">
      <div className="flex items-center justify-between gap-2">
        <span className="text-sm font-bold text-text">{label}</span>
        <span className="text-xs text-text-faint">PNG, JPEG or WebP, under 6 MB</span>
      </div>
      <div className="flex items-center gap-3">
        <span className={cn('flex shrink-0 items-center justify-center overflow-hidden bg-surface-hover text-text-faint', kind === 'avatarAssetId' ? 'size-avatar-xl rounded-pill' : 'h-12 w-24 rounded-sm')}>
          {preview ? <img src={preview} alt={`${label} preview`} className="size-full object-cover" /> : <ImagePlus aria-hidden="true" className="size-icon-lg" />}
        </span>
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <span role="status" className="truncate text-xs text-text-muted">{status}{upload ? ` · ${upload.file.name}` : ''}</span>
          <div className="flex flex-wrap gap-1">
            <Button variant="secondary" size="sm" disabled={disabled} onClick={() => inputRef.current?.click()} aria-controls={inputId} {...(autoFocus ? autofocus : {})}>Choose image</Button>
            {upload && !upload.pending ? (
              <>
                <Button variant="ghost" size="sm" icon={<RefreshCw />} onClick={upload.asset?.state === 'rejected' || !upload.asset ? onRetry : onRefresh}>{upload.asset?.state === 'rejected' || !upload.asset ? 'Try again' : 'Refresh'}</Button>
                <Button variant="ghost" size="sm" icon={<Trash2 />} onClick={onRemove}>Remove</Button>
              </>
            ) : null}
          </div>
        </div>
      </div>
      <input ref={inputRef} id={inputId} type="file" accept="image/png,image/jpeg,image/webp" disabled={disabled} className="sr-only" aria-label={`${label} image file`} onChange={e => { onSelect(e.target.files?.[0]); e.target.value = ''; }} />
    </div>
  );
}
