'use client';

import { useState, type ReactNode } from 'react';
import { ChevronDown, CircleHelp, Globe, Link2, LogOut, Wallet } from 'lucide-react';
import { useAccount } from 'wagmi';
import { cn } from '@/lib/utils';
import { Button, IconButton } from '@/ui/primitives';
import AccountPreferences from './account-preferences';
import { BalanceVisibilityToggle } from './balance-visibility';

const SUPPORT_URL = 'https://t.me/Sidiorafunsupport';

export interface AccountPanelProps {
  onLinkSocials: () => void;
  onSignOut: () => Promise<void> | void;
  signingOut?: boolean;
  signOutError?: string | null;
  className?: string;
}

function rowClass(interactive: boolean) {
  return cn('flex h-12 w-full items-center gap-3 rounded-sm px-3 text-left text-sm font-bold text-text transition-colors duration-fast', interactive && 'hover:bg-surface-hover');
}

function RowIcon({ children }: { children: ReactNode }) {
  return <span aria-hidden="true" className="inline-flex size-8 shrink-0 items-center justify-center rounded-pill bg-surface-3 text-text-muted [&>svg]:size-icon-md">{children}</span>;
}

/**
 * Own-profile rail: how the viewer is connected, the balance privacy toggle,
 * support, social linking (the published socials in the edit form), language
 * and a sign-out that ends the application session but leaves the wallet
 * extension connected.
 */
export default function AccountPanel({ onLinkSocials, onSignOut, signingOut = false, signOutError = null, className }: AccountPanelProps) {
  const [open, setOpen] = useState(true);
  const [language, setLanguage] = useState(false);
  const { connector } = useAccount();
  const provider = connector?.name ?? 'Wallet';
  const bodyId = 'account-panel-body';

  return (
    <section aria-labelledby="account-panel-title" className={cn('rounded-lg bg-surface-1 p-2', className)}>
      <div className="flex items-center justify-between gap-2 px-2 py-1">
        <h2 id="account-panel-title" className="text-md font-bold text-text">Account</h2>
        <IconButton
          label={open ? 'Collapse account panel' : 'Expand account panel'}
          icon={<ChevronDown className={cn('transition-transform duration-fast', open && 'rotate-180')} />}
          size="sm"
          aria-expanded={open}
          aria-controls={bodyId}
          onClick={() => setOpen(value => !value)}
        />
      </div>
      <div id={bodyId} hidden={!open} className="flex flex-col gap-1">
        <div className="flex items-center gap-3 rounded-sm bg-surface-2 px-3 py-2">
          <RowIcon><Wallet /></RowIcon>
          <div className="min-w-0 flex-1">
            <p className="text-2xs font-bold uppercase tracking-[.04em] text-text-faint">Connected with</p>
            <p className="truncate text-sm font-bold text-text">{provider} <span className="font-semibold text-text-muted">· external wallet</span></p>
          </div>
          <BalanceVisibilityToggle />
        </div>
        <a href={SUPPORT_URL} target="_blank" rel="noreferrer noopener" className={rowClass(true)}>
          <RowIcon><CircleHelp /></RowIcon>
          <span className="min-w-0 flex-1 truncate">Support</span>
          <span className="text-xs font-semibold text-text-faint">Telegram</span>
        </a>
        <button type="button" onClick={onLinkSocials} className={rowClass(true)}>
          <RowIcon><Link2 /></RowIcon>
          <span className="min-w-0 flex-1 truncate">Link socials</span>
          <span className="text-xs font-semibold text-text-faint">Website, X, Telegram, Discord</span>
        </button>
        <div>
          <button type="button" aria-expanded={language} aria-controls="account-language" onClick={() => setLanguage(value => !value)} className={rowClass(true)}>
            <RowIcon><Globe /></RowIcon>
            <span className="min-w-0 flex-1 truncate">Language</span>
            <ChevronDown aria-hidden="true" className={cn('size-icon-md shrink-0 text-text-faint transition-transform duration-fast', language && 'rotate-180')} />
          </button>
          <div id="account-language" hidden={!language} className="mt-1 rounded-sm bg-surface-2 p-3">
            {language ? <AccountPreferences section="locale" /> : null}
          </div>
        </div>
        <Button variant="danger-text" fullWidth icon={<LogOut />} loading={signingOut} disabled={signingOut} onClick={() => { void onSignOut(); }} className="mt-1">
          Log out
        </Button>
        {signOutError ? <p role="alert" className="px-3 text-xs text-danger">{signOutError}</p> : null}
        <p className="px-3 pb-1 text-xs text-text-faint">Logging out keeps your wallet connected.</p>
      </div>
    </section>
  );
}
