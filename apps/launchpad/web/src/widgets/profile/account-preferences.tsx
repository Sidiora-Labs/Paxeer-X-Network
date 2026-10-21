'use client';

import { useState } from 'react';
import { cn } from '@/lib/utils';
import { Button, SelectPill, Toggle, UnavailableState, humanError } from '@/ui/primitives';
import { AccountApiError, readPreferences, savePreferences, type AccountPreferences as Preferences } from './account-transport';
import { usePrivateAccountQuery } from './use-private-account-query';
import { actionFailureMessage, isNotServedError } from '@/core/market/unsupported';

const preferencesOperation = 'GET /v3/me/preferences';

const LOCALES: Array<{ value: string; label: string }> = [
  { value: 'en-US', label: 'English (US)' },
  { value: 'en-GB', label: 'English (UK)' },
  { value: 'de-DE', label: 'Deutsch' },
  { value: 'es-ES', label: 'Español' },
  { value: 'fr-FR', label: 'Français' },
  { value: 'pt-BR', label: 'Português (Brasil)' },
  { value: 'tr-TR', label: 'Türkçe' },
  { value: 'ja-JP', label: '日本語' },
  { value: 'ko-KR', label: '한국어' },
  { value: 'zh-CN', label: '中文（简体）' },
];

export function localeOptions(current: string) {
  return LOCALES.some(option => option.value === current) || !current ? LOCALES : [{ value: current, label: current }, ...LOCALES];
}

type Patch = Partial<Pick<Preferences, 'privateActivity' | 'locale'>>;

export interface AccountPreferencesProps {
  /** `locale` renders only the language control (account panel); `all` is the Settings tab. */
  section?: 'all' | 'locale';
  className?: string;
}

export default function AccountPreferences({ section = 'all', className }: AccountPreferencesProps) {
  const query = usePrivateAccountQuery('preferences', readPreferences);
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [conflict, setConflict] = useState(false);
  const preferences = query.data?.data.preferences;

  async function change(patch: Patch) {
    if (!preferences || saving || conflict) return;
    setSaving(true);
    try {
      await savePreferences({ expectedRevision: preferences.revision, ...patch });
      await query.refetch();
      setError(null);
    } catch (e) {
      setError(actionFailureMessage('Saving settings', e));
      setConflict(e instanceof AccountApiError && e.status === 412);
    } finally {
      setSaving(false);
    }
  }

  if (!query.owner) return null;

  // No preferences service answers here (503 retryable:false): the unavailable state, not an error.
  if (query.error && isNotServedError(query.error)) {
    const status = (query.error as { status?: number }).status ?? null;
    return section === 'locale'
      ? <div className={cn('flex flex-col gap-2', className)}><UnavailableState variant="inline" title="Saving your language" operation={preferencesOperation} status={status} className="text-xs" /></div>
      : (
        <section aria-labelledby="account-preferences-title" className={cn('flex flex-col gap-3', className)}>
          <h2 id="account-preferences-title" className="text-md font-bold text-text">Settings</h2>
          <UnavailableState variant="panel" title="Saving settings" operation={preferencesOperation} status={status} message="Check back soon" onRetry={() => { void query.refetch(); }} className="rounded-md bg-surface-2 p-4" />
        </section>
      );
  }

  const language = (
    <div className="flex flex-wrap items-center justify-between gap-3">
      <div>
        <p id="preferences-locale-label" className="text-sm font-bold text-text">Language</p>
        <p className="text-xs text-text-faint">Applies on every device you sign in on.</p>
      </div>
      <SelectPill
        label="Language"
        options={localeOptions(preferences?.locale ?? '')}
        value={preferences?.locale ?? null}
        onChange={locale => { void change({ locale }); }}
        disabled={!preferences || saving || conflict}
        loading={query.isPending || saving}
        size="md"
      />
    </div>
  );

  const status = (
    <>
      {query.error ? <p role="alert" className="text-xs text-danger">{humanError(query.error)}</p> : null}
      {error ? <p role="alert" className="text-xs text-danger">{error}</p> : null}
      {conflict ? <Button variant="secondary" size="sm" onClick={() => { void query.refetch().then(() => setConflict(false)); }}>Reload settings</Button> : null}
    </>
  );

  if (section === 'locale') return <div className={cn('flex flex-col gap-2', className)}>{language}{status}</div>;

  return (
    <section aria-labelledby="account-preferences-title" className={cn('flex flex-col gap-3', className)}>
      <h2 id="account-preferences-title" className="text-md font-bold text-text">Settings</h2>
      {preferences ? <p className="text-xs text-text-faint">Timezone {preferences.timezone} · currency {preferences.currency}</p> : null}
      <div className="rounded-md bg-surface-2 p-4">{language}</div>
      <div className="flex flex-wrap items-center justify-between gap-3 rounded-md bg-surface-2 p-4">
        <div>
          <p id="preferences-private-activity" className="text-sm font-bold text-text">Private activity</p>
          <p className="max-w-prose text-xs text-text-faint">Hides your activity on your profile. Onchain trades are always public.</p>
        </div>
        <Toggle
          checked={preferences?.privateActivity ?? false}
          onChange={privateActivity => { void change({ privateActivity }); }}
          labelledBy="preferences-private-activity"
          disabled={!preferences || conflict}
          loading={saving}
        />
      </div>
      {status}
      <p className="text-xs text-text-faint">Account deletion is not available yet.</p>
    </section>
  );
}
