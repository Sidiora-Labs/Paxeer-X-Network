'use client';

import { useEffect, useState } from 'react';
import { ArrowDownToLine, ArrowUpFromLine, Gift, LogOut, RefreshCw, Star, User } from 'lucide-react';
import { Avatar, Button, ModalFrame, TabStrip } from '@/ui/primitives';
import CopyAddress from '@/ui/shared/copy-address';
import { shortAddress } from '@/utils/format';
import FundingPanel from '@/widgets/funding/funding-panel';
import TransferPanel from '@/widgets/funding/transfer-panel';
import ReconnectPanel from '@/widgets/wallet-recovery/reconnect-panel';
import { WalletMark } from './sign-in-wallet-row';

export type AccountMenuTab = 'account' | 'receive' | 'send' | 'reconnect';

interface WalletModalProps {
  open: boolean;
  tab: AccountMenuTab;
  onTabChange: (tab: AccountMenuTab) => void;
  onClose: () => void;
  address: string;
  displayName: string | null;
  avatarUrl: string | null;
  connectorName: string | null;
  connectorIcon: string | null | undefined;
  onLogOut: () => Promise<void>;
}

const titles: Record<AccountMenuTab, string> = { account: 'Account', receive: 'Deposit', send: 'Send', reconnect: 'Reconnect wallet' };
const tabs: { id: AccountMenuTab; label: string }[] = [
  { id: 'account', label: 'Account' },
  { id: 'receive', label: 'Deposit' },
  { id: 'send', label: 'Send' },
  { id: 'reconnect', label: 'Reconnect' },
];

/** Signed-in account menu: identity header, Deposit (funding receive), Send, Reconnect, shortcuts and Log out. */
export default function WalletModal({ open, tab, onTabChange, onClose, address, displayName, avatarUrl, connectorName, connectorIcon, onLogOut }: WalletModalProps) {
  const [loggingOut, setLoggingOut] = useState(false);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => { if (!open) setError(null); }, [open]);

  const logOut = async () => {
    if (loggingOut) return;
    setLoggingOut(true); setError(null);
    try { await onLogOut(); onClose(); }
    catch (failure) { console.error('[account] log out failed:', failure); setError("Couldn't log out. Try again"); }
    finally { setLoggingOut(false); }
  };

  const name = displayName ?? shortAddress(address, 6, 4);
  const idPrefix = 'account-menu';

  return (
    <ModalFrame open={open} onClose={onClose} size="sm" title={titles[tab]} hideTitle closeLabel="Close account menu">
      <div className="flex flex-col gap-4">
        <div className="flex items-center gap-3">
          <Avatar src={avatarUrl} name={name} size="lg" badge={connectorIcon || connectorName ? <WalletMark icon={connectorIcon} name={connectorName ?? 'Wallet'} size="sm" className="rounded-pill bg-surface-2" /> : undefined} />
          <div className="min-w-0 flex-1">
            <p className="truncate text-base font-bold text-text">{name}</p>
            <p className="flex items-center gap-1 text-xs text-text-muted">
              <span className="font-mono-sl">{shortAddress(address, 6, 4)}</span>
              <CopyAddress address={address} variant="icon" />
              {connectorName ? <span className="text-text-faint">via {connectorName}</span> : null}
            </p>
          </div>
        </div>

        <TabStrip tabs={tabs} active={tab} onChange={onTabChange} label="Account sections" variant="filled" idPrefix={idPrefix} />

        <div id={`${idPrefix}-panel-${tab}`} role="tabpanel" aria-labelledby={`${idPrefix}-tab-${tab}`} className="flex flex-col gap-2">
          {tab === 'receive' ? <FundingPanel /> : null}
          {tab === 'send' ? <TransferPanel /> : null}
          {tab === 'reconnect' ? <ReconnectPanel /> : null}
          {tab === 'account' ? (
            <>
              <nav aria-label="Account shortcuts" className="flex flex-col gap-1">
                <Button variant="ghost" fullWidth href={`/profile/${address}`} icon={<User />} className="justify-start">Profile</Button>
                <Button variant="ghost" fullWidth href="/watchlist" icon={<Star />} className="justify-start">Watchlist</Button>
                <Button variant="ghost" fullWidth href="/rewards" icon={<Gift />} className="justify-start">Rewards</Button>
                <Button variant="ghost" fullWidth icon={<ArrowDownToLine />} className="justify-start" onClick={() => onTabChange('receive')}>Deposit</Button>
                <Button variant="ghost" fullWidth icon={<ArrowUpFromLine />} className="justify-start" onClick={() => onTabChange('send')}>Send</Button>
                <Button variant="ghost" fullWidth icon={<RefreshCw />} className="justify-start" onClick={() => onTabChange('reconnect')}>Reconnect wallet</Button>
              </nav>
              <div className="pt-2">
                <Button variant="danger-text" fullWidth icon={<LogOut />} loading={loggingOut} onClick={() => { void logOut(); }} className="justify-start">Log out</Button>
              </div>
            </>
          ) : null}
          {error ? <p role="alert" className="text-sm text-danger">{error}</p> : null}
        </div>
      </div>
    </ModalFrame>
  );
}
