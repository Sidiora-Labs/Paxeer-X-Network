'use client';

import { useEffect, useId, useState, type FormEvent } from 'react';
import { Button, FormField } from '@/ui/primitives';
import { InteractionClient, type Inbox, type Privacy } from './interaction-client';

const walletPattern = /^0x[0-9a-fA-F]{40}$/u;
const selectClass = 'h-control-height-md w-full rounded-sm bg-surface-2 px-3 text-base text-text outline-none transition-colors duration-fast focus-visible:outline-2 focus-visible:outline-focus disabled:text-text-disabled';

export function PrivacySettings({ client }: { client: InteractionClient }) {
  const [privacy, setPrivacy] = useState<Privacy | null>(null); const [inbox, setInbox] = useState<Inbox>('nobody'); const [error, setError] = useState(''); const [busy, setBusy] = useState(false); const [wallet, setWallet] = useState(''); const id = useId(); const walletID = useId();
  useEffect(() => { const controller = new AbortController(); setPrivacy(null); void client.privacy(controller.signal).then(value => { if (!controller.signal.aborted) { setPrivacy(value.data); setInbox(value.data.inbox); } }).catch(error => { if (!controller.signal.aborted) { console.error('[privacy] load failed', error); setError("Can't load your settings right now"); } }); return () => controller.abort(); }, [client]);
  async function save(event: FormEvent) { event.preventDefault(); if (!privacy || busy) return; setBusy(true); setError(''); try { await client.setPrivacy(inbox, privacy.revision); const fresh = await client.privacy(); setPrivacy(fresh.data); setInbox(fresh.data.inbox); } catch (error) { console.error('[privacy] save failed', error); setError("Couldn't save, try again"); } finally { setBusy(false); } }
  async function block(remove: boolean) { if (busy || !walletPattern.test(wallet)) return; setBusy(true); setError(''); try { await client.block(wallet, remove); setWallet(''); } catch (error) { console.error('[privacy] block failed', error); setError("Couldn't update your block list, try again"); } finally { setBusy(false); } }
  const validWallet = walletPattern.test(wallet);
  return <section aria-label="Privacy" className="flex flex-col gap-4 rounded-md bg-surface-1 p-4">
    <form onSubmit={event => { void save(event); }} aria-busy={busy || undefined} className="flex flex-col gap-3">
      <div className="flex flex-col gap-1.5">
        <label htmlFor={id} className="text-sm font-bold text-text">Who can send you direct messages?</label>
        <select id={id} disabled={!privacy || busy} value={inbox} onChange={event => setInbox(event.target.value as Inbox)} className={selectClass}><option value="everyone">Everyone</option><option value="followed_only">People you follow</option><option value="nobody">Nobody</option></select>
      </div>
      <div className="flex justify-end"><Button type="submit" variant="primary" size="md" disabled={!privacy || busy} loading={busy}>Save</Button></div>
    </form>
    <div className="flex flex-col gap-3">
      <FormField id={walletID} label="Block or unblock a wallet" mono error={wallet !== '' && !validWallet ? 'Enter a valid wallet (0x…)' : null} inputProps={{ value: wallet, disabled: busy, onChange: event => setWallet(event.target.value), placeholder: '0x…' }} />
      <div className="flex flex-wrap justify-end gap-2">
        <Button variant="danger-text" size="sm" disabled={busy || !validWallet} onClick={() => { void block(false); }}>Block</Button>
        <Button variant="secondary" size="sm" disabled={busy || !validWallet} onClick={() => { void block(true); }}>Unblock</Button>
      </div>
      <p className="text-xs text-text-faint">Unblocking won't bring back follows or messages</p>
    </div>
    {error && <p role="alert" className="text-xs text-danger">{error}</p>}
  </section>;
}
