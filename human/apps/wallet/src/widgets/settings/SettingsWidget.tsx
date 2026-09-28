'use client';

import { useState, useCallback } from 'react';
import { useWalletState, useWalletActions } from '@/providers/WalletProvider';
import { useWalletKind } from '@/providers/WalletKindProvider';
import { useOptionalEmbeddedWallet } from '@/lib/wallet';
import { shortenAddress } from '@/lib/format';
import {
    announceRpcChanged,
    PAXEER_CONFIG,
    validateRpcEndpoint,
} from '@/lib/constants';
import { PassphrasePrompt } from '@/components/auth/PassphrasePrompt';
import { Check, Fingerprint, LogOut, Repeat, TriangleAlert, Trash2 } from 'lucide-react';
import { SvgIcon } from '@/components/ui/SvgIcon';
import { getAvatarPath } from '@/lib/avatar';
import { NotificationToggle, InstallButton } from '@/components/pwa/PWAComponents';
import { AccountSwitcherDrawer } from '@/components/ui/AccountSwitcherDrawer';
import { ImportPkView } from './ImportPkView';
import Image from "next/image";
import {
    dappTabsRepository,
    dappPermissionsRepository,
    preferencesRepository,
    type DappPermissionRecord,
} from '@/platform/storage/repositories';
import { useLocale } from '@/providers/LocaleProvider';
import { SUPPORTED_LOCALES, LOCALE_DISPLAY_NAMES, type Locale } from '@/lib/locale';
import { resetStorageForLifecycle } from '@/platform/storage/registry';
import { Switch } from '@/components/ui/primitives';
import type { ShellRouteName } from '@/domains/shell';
import {
    disableBiometricUnlock,
    enrollBiometricUnlock,
    isBiometricUnlockEnrolled,
    isBiometricUnlockSupported,
} from '@/lib/biometric-unlock';
import {
    revokeAllDappPermissions,
    revokeDappPermission,
} from '@/lib/dapp-permissions';
import {
    announceCurrencyChanged,
    ensureRates,
    FIAT_CURRENCIES,
    getRatesFreshness,
} from '@/lib/currency';

type SettingsView = 'main' | 'import-pk' | 'preferences' | 'network' | 'advanced' | 'notifications' | 'connected-dapps';

interface SettingsWidgetProps {
    onNavigate?: (route: ShellRouteName) => void;
    onPaxscan?: (path?: string) => void;
}

export function SettingsWidget({ onNavigate, onPaxscan }: SettingsWidgetProps) {
    const { activeAccount, accounts } = useWalletState();
    const { lock, reauthenticate, exportMnemonic, exportPrivateKey, importPrivateKey, reset } = useWalletActions();
    const { p, t } = useLocale();
    // `addAccount` only exists in self-custody mode; resolve it lazily so the
    // embedded path doesn't pull on an action that throws on call.
    const walletActions = useWalletActions();
    const { kind, setKind, clearKind } = useWalletKind();
    const embedded = useOptionalEmbeddedWallet();
    const isEmbedded = kind === 'embedded';
    const isFunded = kind === 'funded';
    // Whether the user has a funded account on the server. Independent of
    // `kind` — lets us show "Switch to Funded" for users who already
    // provisioned an account but are currently viewing in embedded mode.
    const hasFundedAccount = !!embedded?.fundedSelf;
    // Whether the user has a Supabase session. Funded mode lives on top of
    // the Supabase auth surface, so signed-out / self-custody users can't
    // jump straight into funded — they have to sign in first.
    const hasEmbeddedSession = !!embedded?.isAuthenticated;

    const [view, setView] = useState<SettingsView>('main');
    const [accountSwitcherOpen, setAccountSwitcherOpen] = useState(false);

    const [showMnemonic, setShowMnemonic] = useState(false);
    const [mnemonic, setMnemonic] = useState('');
    const [copied, setCopied] = useState(false);
    const [confirmReset, setConfirmReset] = useState(false);
    const [showPrivateKey, setShowPrivateKey] = useState(false);
    const [privateKey, setPrivateKey] = useState('');
    const [copiedPk, setCopiedPk] = useState(false);
    const [passphrasePrompt, setPassphrasePrompt] = useState<'mnemonic' | 'pk' | 'biometric' | null>(null);
    const [passphraseError, setPassphraseError] = useState('');
    const [biometricEnabled, setBiometricEnabled] = useState(isBiometricUnlockEnrolled);
    const [biometricStatus, setBiometricStatus] = useState('');

    const [importPk, setImportPk] = useState('');
    const [importPkName, setImportPkName] = useState('');
    const [importPkError, setImportPkError] = useState('');
    const [importPkLoading, setImportPkLoading] = useState(false);

    const handleExport = async () => {
        if (showMnemonic) { setShowMnemonic(false); setMnemonic(''); return; }
        setPassphraseError(''); setPassphrasePrompt('mnemonic');
    };

    const handleCopyMnemonic = async () => {
        await navigator.clipboard.writeText(mnemonic);
        setCopied(true); setTimeout(() => setCopied(false), 2000);
    };

    const handleExportPrivateKey = async () => {
        if (showPrivateKey) { setShowPrivateKey(false); setPrivateKey(''); return; }
        if (!activeAccount) return;
        setPassphraseError(''); setPassphrasePrompt('pk');
    };

    const handlePassphraseComplete = useCallback(async (password: string) => {
        setPassphraseError('');
        try {
            await reauthenticate(password);
            if (passphrasePrompt === 'mnemonic') {
                const phrase = await exportMnemonic();
                setMnemonic(phrase); setShowMnemonic(true);
            } else if (passphrasePrompt === 'pk' && activeAccount) {
                const pk = await exportPrivateKey(activeAccount.address);
                setPrivateKey(pk); setShowPrivateKey(true);
            } else if (passphrasePrompt === 'biometric') {
                await enrollBiometricUnlock(password);
                setBiometricEnabled(true);
                setBiometricStatus(p.biometricEnabled);
            }
            setPassphrasePrompt(null);
        } catch (e: unknown) { setPassphraseError((e as Error).message || t.settings.verificationFailed); }
    }, [passphrasePrompt, activeAccount, reauthenticate, exportMnemonic, exportPrivateKey, p.biometricEnabled, t.settings.verificationFailed]);

    const handleBiometric = () => {
        setBiometricStatus('');
        if (biometricEnabled) {
            disableBiometricUnlock();
            setBiometricEnabled(false);
            setBiometricStatus(p.biometricDisabled);
            return;
        }
        if (!isBiometricUnlockSupported()) {
            setBiometricStatus(p.biometricUnavailable);
            return;
        }
        setPassphraseError('');
        setPassphrasePrompt('biometric');
    };

    const handleCopyPrivateKey = async () => {
        await navigator.clipboard.writeText(privateKey);
        setCopiedPk(true); setTimeout(() => setCopiedPk(false), 2000);
    };

    const handleReset = async () => {
        if (!confirmReset) { setConfirmReset(true); return; }
        await reset();
        resetStorageForLifecycle('reset');
    };
    const handleAddAccount = async () => { await walletActions.addAccount(`${t.account.title} ${accounts.length + 1}`); };

    // ── Embedded-mode actions ────────────────────────────────────────────
    const handleEmbeddedSignOut = async () => {
        if (!embedded) return;
        await embedded.signOut();
        resetStorageForLifecycle('logout');
        // Keep `kind === 'embedded'` so the next launch lands directly on the
        // sign-in screen instead of bouncing back to the welcome cards.
    };

    const handleSwitchWalletMode = async () => {
        // Switch explicitly to the *other* mode rather than clearing — the
        // migration logic in `WalletProvider` would otherwise bounce the user
        // straight back to their current mode (e.g. an existing self-custody
        // wallet would be auto-detected and re-selected on reload).
        //
        // We deliberately do NOT wipe encrypted data here. Existing
        // self-custody material stays in localStorage so users can flip back
        // via the same setting and unlock with their PIN. "Erase Wallet"
        // row in the Danger Zone is the dedicated destructive path.
        resetStorageForLifecycle('custody-switch');
        if (isEmbedded || isFunded) {
            await embedded?.signOut();
            setKind('self-custody');
        } else {
            await lock().catch(() => undefined);
            setKind('embedded');
        }
        if (typeof window !== 'undefined') window.location.reload();
    };

    /**
     * Pivot from a standard managed wallet into Funded mode. Used by the
     * "Become a Funded Trader" / "Switch to Funded" row.
     *
     * No sign-out is needed — funded and standard accounts share the same
     * Supabase identity. We just flip `kind` to 'funded' and the provider
     * chain takes over:
     *   1. `refreshFunded()` runs, sees `fundedSelf === null`, sets
     *      `hasWallet=false` → the shell falls back to onboarding.
     *   2. `Onboarding`'s resume effect detects `kind==='funded'`,
     *      authenticated, and no funded account → routes to the tier
     *      picker.
     *   3. Tier picker calls `provisionFunded(tier_id)` → the server
     *      disburses USDL + PAX → `fundedSelf` flips non-null →
     *      `refreshFunded` flips `hasWallet=true` → funded portfolio.
     *
     * If the user already has a funded account, step 2 short-circuits
     * and the funded portfolio renders immediately.
     */
    const handleSwitchToFunded = () => {
        resetStorageForLifecycle('custody-switch');
        setKind('funded');
    };

    /** Go back to the standard managed-wallet view from funded. */
    const handleSwitchFromFunded = () => {
        resetStorageForLifecycle('custody-switch');
        setKind('embedded');
    };

    // `clearKind` is intentionally retained on the hook surface for future
    // flows (e.g. account-recovery deep links). Not used here today.
    void clearKind;

    const handleImportPrivateKey = async () => {
        const pk = importPk.trim();
        if (!pk || (!pk.startsWith('0x') && pk.length !== 64 && pk.length !== 66)) { setImportPkError('Enter a valid private key (hex, with or without 0x prefix)'); return; }
        const name = importPkName.trim() || `Imported ${accounts.length + 1}`;
        setImportPkLoading(true); setImportPkError('');
        try { await importPrivateKey(pk.startsWith('0x') ? pk : `0x${pk}`, name); setImportPk(''); setImportPkName(''); setView('main'); }
        catch (e: unknown) { setImportPkError((e as Error).message || 'Failed to import private key'); }
        finally { setImportPkLoading(false); }
    };

    if (view === 'import-pk') {
        return (
            <ImportPkView
                importPk={importPk} setImportPk={setImportPk}
                importPkName={importPkName} setImportPkName={setImportPkName}
                importPkError={importPkError} importPkLoading={importPkLoading}
                accountCount={accounts.length}
                onImport={handleImportPrivateKey}
                onBack={() => { setView('main'); setImportPkError(''); }}
            />
        );
    }

    if (view === 'preferences') return <PreferencesView onBack={() => setView('main')} />;
    if (view === 'network') return <NetworkView onBack={() => setView('main')} />;
    if (view === 'advanced') return <AdvancedView onBack={() => setView('main')} />;
    if (view === 'notifications') return <NotificationsView onBack={() => setView('main')} />;
    if (view === 'connected-dapps') return <ConnectedDAppsView onBack={() => setView('main')} />;

    return (
        <div className="grid grid-cols-2 gap-2.5 px-3 pt-3 pb-4">
            <button
                onClick={() => {
                    // Account switcher is self-custody only — embedded users have a
                    // single server-provisioned wallet per identity.
                    if (!isEmbedded) setAccountSwitcherOpen(true);
                }}
                disabled={isEmbedded}
                className="col-span-2 bg-pax-surface rounded-[20px] p-4 press-scale hover:bg-white/[0.06] transition-colors text-left w-full disabled:hover:bg-pax-surface disabled:cursor-default"
            >
                <div className="flex items-center gap-3">
                    <Image
                        src={getAvatarPath(activeAccount?.address || '')}
                        alt={activeAccount?.name || 'Account'}
                        width={40}
                        height={40}
                        className="w-10 h-10 rounded-full object-cover"
                    />
                    <div className="flex-1 min-w-0">
                        <p className="text-sm font-medium truncate">{activeAccount?.name || 'Account 1'}</p>
                        <p className="text-xs text-pax-muted truncate">{activeAccount ? shortenAddress(activeAccount.address, 6) : '—'}</p>
                        {isEmbedded && (
                            <p className="text-[10px] text-pax-accent/80 mt-0.5">Paxeer Wallet · {t.settings.managedCustody}</p>
                        )}
                    </div>
                    {!isEmbedded && (
                        <SvgIcon name="chevron-right" className="w-4 h-4 shrink-0" style={{ filter: 'brightness(0) invert(0.3)' }} />
                    )}
                </div>
            </button>

            {!isEmbedded && (
                <AccountSwitcherDrawer
                    open={accountSwitcherOpen}
                    onClose={() => setAccountSwitcherOpen(false)}
                />
            )}

            {!isEmbedded && (
                <>
                    <div className="col-span-2 px-1 pt-1"><p className="text-[13px] font-bold text-pax-muted uppercase tracking-[0.06em]">{t.settings.accounts}</p></div>
                    <div className="col-span-2 bg-pax-surface rounded-[20px]  divide-white/[0.04] overflow-hidden">
                        <SettingsRow icon={<SvgIcon name="plus" className="w-4.5 h-4.5" style={{ filter: 'brightness(0) invert(0.6)' }} />} label={t.settings.addAccount} subtitle={t.settings.deriveNext} onClick={handleAddAccount} />
                        <SettingsRow icon={<SvgIcon name="key" className="w-4.5 h-4.5" style={{ filter: 'brightness(0) invert(0.6)' }} />} label={t.settings.importPrivateKey} subtitle={t.settings.importKeySubtitle} onClick={() => setView('import-pk')} />
                    </div>
                </>
            )}

            <div className="col-span-2 px-1 pt-1"><p className="text-[13px] font-bold text-pax-muted uppercase tracking-[0.06em]">{t.settings.addressBook}</p></div>
            <div className="col-span-2 bg-pax-surface rounded-[20px]  divide-white/[0.04] overflow-hidden">
                <SettingsRow icon={<SvgIcon name="user" className="w-4.5 h-4.5" style={{ filter: 'brightness(0) invert(0.6)' }} />} label={t.nav.contacts} subtitle={t.settings.contactsSubtitle} onClick={() => onNavigate?.('contacts')} />
            </div>

            {/* ── Security / Identity section ─────────────────────────────── */}
            <div className="col-span-2 px-1 pt-1"><p className="text-[13px] font-bold text-pax-muted uppercase tracking-[0.06em]">{isEmbedded ? t.account.title : t.settings.security}</p></div>
            <div className="col-span-2 bg-pax-surface rounded-[20px]  divide-white/[0.04] overflow-hidden">
                {isEmbedded ? (
                    <>
                        {embedded?.user?.email && (
                            <div className="px-4 py-3.5 text-sm">
                                <p className="text-[11px] text-pax-muted uppercase tracking-wider">{t.settings.signedInAs}</p>
                                <p className="text-sm mt-0.5 truncate">{embedded.user.email}</p>
                            </div>
                        )}
                        <button
                            onClick={handleEmbeddedSignOut}
                            className="w-full flex items-center gap-3 px-4 py-3.5 text-sm press-scale transition-colors hover:bg-white/5"
                        >
                            <span className="text-pax-muted"><LogOut className="w-[18px] h-[18px]" /></span>
                            <div className="flex-1 text-left min-w-0">
                                <span className="block">{t.settings.signOut}</span>
                                <span className="block text-[11px] text-pax-muted">{t.settings.signOutSubtitle}</span>
                            </div>
                            <SvgIcon name="chevron-right" className="w-4 h-4" style={{ filter: 'brightness(0) invert(0.3)' }} />
                        </button>
                        <SettingsRow icon={<SvgIcon name="external-link" className="w-4.5 h-4.5" style={{ filter: 'brightness(0) invert(0.6)' }} />} label={t.settings.viewOnExplorer} onClick={() => onPaxscan?.(`/address/${activeAccount?.address}`)} />
                    </>
                ) : (
                    <>
                        <SettingsRow icon={<SvgIcon name="lock" className="w-4.5 h-4.5" style={{ filter: 'brightness(0) invert(0.6)' }} />} label={t.settings.lockWallet} onClick={lock} />
                        <SettingsRow
                            icon={<Fingerprint className="w-4.5 h-4.5" />}
                            label={biometricEnabled ? p.disableBiometrics : p.enableBiometrics}
                            subtitle={p.biometricSubtitle}
                            onClick={handleBiometric}
                        />
                        <SettingsRow icon={<SvgIcon name="send" className="w-4.5 h-4.5" style={{ filter: 'brightness(0) invert(0.6)' }} />} label={showMnemonic ? t.settings.hideRecoveryPhrase : t.settings.exportRecoveryPhrase} onClick={handleExport} />
                        <SettingsRow icon={<SvgIcon name="key" className="w-4.5 h-4.5" style={{ filter: 'brightness(0) invert(0.6)' }} />} label={showPrivateKey ? t.settings.hidePrivateKey : t.settings.exportPrivateKey} subtitle={t.settings.exportPkSubtitle} onClick={handleExportPrivateKey} />
                        <SettingsRow icon={<SvgIcon name="external-link" className="w-4.5 h-4.5" style={{ filter: 'brightness(0) invert(0.6)' }} />} label={t.settings.viewOnExplorer} onClick={() => onPaxscan?.(`/address/${activeAccount?.address}`)} />
                    </>
                )}
            </div>

            {biometricStatus && (
                <p className="col-span-2 px-2 text-xs text-pax-muted" role="status">
                    {biometricStatus}
                </p>
            )}

            {showPrivateKey && privateKey && (
                <div className="col-span-2 bg-pax-surface rounded-[20px] p-4 animate-scale-in">
                    <div className="flex items-start gap-2 mb-3">
                        <SvgIcon name="warning" className="w-4 h-4" style={{ filter: 'invert(48%) sepia(79%) saturate(2476%) hue-rotate(338deg) brightness(118%) contrast(119%)' }} />
                        <p className="text-xs text-red-400/80">{t.settings.pkWarning}</p>
                    </div>
                    <div className="px-3 py-2.5 rounded-lg bg-white/5 font-mono text-xs break-all select-all">{privateKey}</div>
                    <button onClick={handleCopyPrivateKey} className="flex items-center gap-2 text-xs text-pax-muted press-scale mt-3">
                        {copiedPk ? <SvgIcon name="check" className="w-3.5 h-3.5" style={{ filter: 'invert(69%) sepia(61%) saturate(588%) hue-rotate(88deg) brightness(93%) contrast(93%)' }} /> : <SvgIcon name="copy" className="w-3.5 h-3.5" style={{ filter: 'brightness(0) invert(0.6)' }} />}
                        {copiedPk ? t.common.copied : t.settings.copyPk}
                    </button>
                </div>
            )}

            {showMnemonic && mnemonic && (
                <div className="col-span-2 bg-pax-surface rounded-[20px] p-4 animate-scale-in">
                    <div className="grid grid-cols-3 gap-2 mb-3">
                        {mnemonic.split(' ').map((word, i) => (
                            <div key={i} className="flex items-center gap-1 px-2 py-1.5 rounded-lg bg-white/5">
                                <span className="text-[10px] text-pax-muted w-4 text-right">{i + 1}</span>
                                <span className="text-xs font-medium">{word}</span>
                            </div>
                        ))}
                    </div>
                    <button onClick={handleCopyMnemonic} className="flex items-center gap-2 text-xs text-pax-muted press-scale">
                        {copied ? <SvgIcon name="check" className="w-3.5 h-3.5" style={{ filter: 'invert(69%) sepia(61%) saturate(588%) hue-rotate(88deg) brightness(93%) contrast(93%)' }} /> : <SvgIcon name="copy" className="w-3.5 h-3.5" style={{ filter: 'brightness(0) invert(0.6)' }} />}
                        {copied ? t.common.copied : t.settings.copyPhrase}
                    </button>
                </div>
            )}

            <div className="col-span-2 px-1 pt-1"><p className="text-[13px] font-bold text-pax-muted uppercase tracking-[0.06em]">{t.settings.general}</p></div>
            <div className="col-span-2 bg-pax-surface rounded-[20px]  divide-white/[0.04] overflow-hidden">
                <SettingsRow icon={<SvgIcon name="settings" className="w-4.5 h-4.5" style={{ filter: 'brightness(0) invert(0.6)' }} />} label={t.settings.displayPreferences} subtitle={`${t.settings.currency}, ${t.settings.language}`} onClick={() => setView('preferences')} />
                <SettingsRow icon={<SvgIcon name="wifi" className="w-4.5 h-4.5" style={{ filter: 'brightness(0) invert(0.6)' }} />} label={t.settings.networkRpc} subtitle={`${t.settings.network}, ${t.settings.customRpc}`} onClick={() => setView('network')} />
                <SettingsRow icon={<SvgIcon name="sliders" className="w-4.5 h-4.5" style={{ filter: 'brightness(0) invert(0.6)' }} />} label={t.settings.advanced} subtitle={t.settings.advancedSubtitle} onClick={() => setView('advanced')} />
                <SettingsRow icon={<SvgIcon name="bell" className="w-4.5 h-4.5" style={{ filter: 'brightness(0) invert(0.6)' }} />} label={t.settings.notifications} subtitle={t.settings.notificationsSubtitle} onClick={() => setView('notifications')} />
                {!isEmbedded && (
                    <SettingsRow icon={<SvgIcon name="external-link" className="w-4.5 h-4.5" style={{ filter: 'brightness(0) invert(0.6)' }} />} label={t.shell.connectedDapps} subtitle={t.settings.connectedDappsSubtitle} onClick={() => setView('connected-dapps')} />
                )}
            </div>

            <div className="col-span-2 px-1 pt-1"><p className="text-[13px] font-bold text-pax-muted uppercase tracking-[0.06em]">{t.settings.about}</p></div>
            <div className="col-span-2 bg-pax-surface rounded-[20px] p-1 space-y-1 overflow-hidden">
                <NotificationToggle />
                <div className="px-4 py-3"><InstallButton className="w-full justify-center" /></div>
            </div>

            <div className="col-span-2 px-1 pt-1"><p className="text-[13px] font-bold text-pax-muted uppercase tracking-[0.06em]">{t.settings.walletMode}</p></div>
            <div className="col-span-2 bg-pax-surface rounded-[20px]  divide-white/[0.04] overflow-hidden">
                {/*
                  Funded entry / exit. Only surfaces for users with an
                  active embedded session — funded accounts are bound to
                  Supabase identities, so a self-custody-only user can't
                  enter this flow until they've signed in.
                */}
                {hasEmbeddedSession && !isFunded && (
                    <button
                        onClick={handleSwitchToFunded}
                        className="w-full flex items-center gap-3 px-4 py-3.5 text-sm press-scale transition-colors hover:bg-white/5"
                    >
                        <span className="text-pax-accent">
                            <SvgIcon
                                name="bridge"
                                className="w-[18px] h-[18px]"
                                style={{
                                    filter:
                                        'brightness(0) saturate(100%) invert(22%) sepia(93%) saturate(7388%) hue-rotate(222deg) brightness(98%) contrast(101%)',
                                }}
                            />
                        </span>
                        <div className="flex-1 text-left min-w-0">
                            <span className="block">
                                {hasFundedAccount ? t.settings.switchToFunded : t.settings.becomeFunded}
                            </span>
                            <span className="block text-[11px] text-pax-muted">
                                {hasFundedAccount
                                    ? t.settings.switchToFundedSubtitle
                                    : t.settings.becomeFundedSubtitle}
                            </span>
                        </div>
                        <SvgIcon name="chevron-right" className="w-4 h-4" style={{ filter: 'brightness(0) invert(0.3)' }} />
                    </button>
                )}

                {isFunded && (
                    <button
                        onClick={handleSwitchFromFunded}
                        className="w-full flex items-center gap-3 px-4 py-3.5 text-sm press-scale transition-colors hover:bg-white/5"
                    >
                        <span className="text-pax-muted"><Repeat className="w-[18px] h-[18px]" /></span>
                        <div className="flex-1 text-left min-w-0">
                            <span className="block">{t.settings.switchToStandard}</span>
                            <span className="block text-[11px] text-pax-muted">{t.settings.switchToStandardSubtitle}</span>
                        </div>
                        <SvgIcon name="chevron-right" className="w-4 h-4" style={{ filter: 'brightness(0) invert(0.3)' }} />
                    </button>
                )}

                <button onClick={handleSwitchWalletMode} className="w-full flex items-center gap-3 px-4 py-3.5 text-sm press-scale transition-colors hover:bg-white/5">
                    <span className="text-pax-muted"><Repeat className="w-[18px] h-[18px]" /></span>
                    <div className="flex-1 text-left min-w-0">
                        <span className="block">{t.settings.switchWalletMode}</span>
                        <span className="block text-[11px] text-pax-muted">{isEmbedded || isFunded ? t.settings.selfCustodySubtitle : t.settings.managedSubtitle}</span>
                    </div>
                    <SvgIcon name="chevron-right" className="w-4 h-4" style={{ filter: 'brightness(0) invert(0.3)' }} />
                </button>
            </div>

            {!isEmbedded && (
                <>
                    <div className="col-span-2 px-1 pt-1"><p className="text-[13px] font-bold text-pax-error/60 uppercase tracking-[0.06em]">{t.settings.dangerZone}</p></div>
                    <div className="col-span-2 bg-pax-surface rounded-[20px] overflow-hidden">
                        <button onClick={handleReset} className="w-full flex items-center gap-3 px-4 py-3.5 text-sm press-scale transition-colors">
                            <Trash2 className="w-[18px] h-[18px] shrink-0 text-red-400" />
                            <span className={confirmReset ? 'text-red-400 font-medium' : 'text-red-400/60'}>
                                {confirmReset ? t.settings.eraseConfirm : t.settings.eraseWallet}
                            </span>
                        </button>
                    </div>
                </>
            )}

            <div className="col-span-2 mt-4 text-center">
                <p className="text-xs text-pax-muted/50">Paxeer Wallet v0.1.0</p>
                <p className="text-xs text-pax-muted/50">Chain {PAXEER_CONFIG.chainId}</p>
            </div>

            {passphrasePrompt && (
                <PassphrasePrompt
                    title={t.settings.freshAuth}
                    subtitle={passphrasePrompt === 'biometric'
                        ? p.biometricSubtitle
                        : p.enterPin}
                    error={passphraseError}
                    onSubmit={handlePassphraseComplete}
                    onCancel={() => { setPassphrasePrompt(null); setPassphraseError(''); }}
                />
            )}
        </div>
    );
}

// ── Deep views ────────────────────────────────────────────────────────────────

function SettingsSubPage({ title, onBack, children }: { title: string; onBack: () => void; children: React.ReactNode }) {
    return (
        <div className="min-h-screen px-4 pt-4 pb-8">
            <div className="flex items-center gap-3 mb-6">
                <button onClick={onBack} className="p-2 -ml-2 press-scale">
                    <SvgIcon name="arrow-left" className="w-5 h-5" style={{ filter: 'brightness(0) invert(0.6)' }} />
                </button>
                <h2 className="text-lg font-bold">{title}</h2>
            </div>
            {children}
        </div>
    );
}

function PreferencesView({ onBack }: { onBack: () => void }) {
    const { locale, setLocale: applyLocale, t } = useLocale();
    const [currency, setCurrency] = useState(() => preferencesRepository.read().currency);
    const [language, setLanguage] = useState<Locale>(() => locale);
    const [rateStatus, setRateStatus] = useState(getRatesFreshness);

    const refreshRates = async () => {
        await ensureRates();
        setRateStatus(getRatesFreshness());
    };

    return (
        <SettingsSubPage title={t.settings.displayPreferences} onBack={onBack}>
            <div className="space-y-5">
                <div>
                    <p className="text-[13px] font-bold text-pax-muted uppercase tracking-[0.06em] mb-3 px-1">{t.settings.currency}</p>
                    <div className="bg-pax-surface rounded-2xl overflow-hidden  divide-white/[0.04]">
                        {FIAT_CURRENCIES.map((c) => (
                            <button key={c} onClick={() => {
                                setCurrency(c);
                                preferencesRepository.update((current) => ({ ...current, currency: c }));
                                announceCurrencyChanged();
                                void refreshRates();
                            }}
                                className="w-full flex items-center justify-between px-4 py-3 press-scale hover:bg-white/5">
                                <span className="text-sm">{c}</span>
                                {currency === c && <SvgIcon name="check" className="w-4 h-4" style={{ filter: 'brightness(0) saturate(100%) invert(22%) sepia(93%) saturate(7388%) hue-rotate(222deg) brightness(98%) contrast(101%)' }} />}
                            </button>
                        ))}
                    </div>
                    <div className="mt-2 flex items-center justify-between px-1">
                        <p className="text-[11px] text-pax-muted">Live USD conversion rates: {rateStatus}</p>
                        <button onClick={refreshRates} className="text-[11px] text-pax-accent press-scale">{t.common.retry}</button>
                    </div>
                </div>
                <div>
                    <p className="text-[13px] font-bold text-pax-muted uppercase tracking-[0.06em] mb-3 px-1">{t.settings.language}</p>
                    <div className="bg-pax-surface rounded-2xl overflow-hidden  divide-white/[0.04]">
                        {SUPPORTED_LOCALES.map((l) => (
                            <button key={l} onClick={() => {
                                setLanguage(l);
                                applyLocale(l);
                            }}
                                className="w-full flex items-center justify-between px-4 py-3 press-scale hover:bg-white/5">
                                <span className="text-sm">{LOCALE_DISPLAY_NAMES[l]}</span>
                                {language === l && <SvgIcon name="check" className="w-4 h-4" style={{ filter: 'brightness(0) saturate(100%) invert(22%) sepia(93%) saturate(7388%) hue-rotate(222deg) brightness(98%) contrast(101%)' }} />}
                            </button>
                        ))}
                    </div>
                </div>
            </div>
        </SettingsSubPage>
    );
}

function NetworkView({ onBack }: { onBack: () => void }) {
    const { p, t } = useLocale();
    const [customRpc, setCustomRpc] = useState(() => preferencesRepository.read().customRpc ?? '');
    const [saved, setSaved] = useState(false);
    const [testing, setTesting] = useState(false);
    const [status, setStatus] = useState('');
    const [error, setError] = useState('');

    const handleSave = async () => {
        setTesting(true);
        setSaved(false);
        setStatus('');
        setError('');
        try {
            const result = await validateRpcEndpoint(customRpc);
            preferencesRepository.update((current) => ({
                ...current,
                customRpc: result.url,
            }));
            setCustomRpc(result.url);
            announceRpcChanged();
            setStatus(`Connected to chain ${result.chainId} in ${result.latencyMs} ms`);
            setSaved(true);
            setTimeout(() => setSaved(false), 2000);
        } catch (caught) {
            setError(caught instanceof Error ? caught.message : 'RPC validation failed.');
        } finally {
            setTesting(false);
        }
    };

    const handleReset = () => {
        preferencesRepository.update((current) => ({ ...current, customRpc: null }));
        setCustomRpc('');
        setError('');
        setStatus('Using the default Paxeer RPC');
        announceRpcChanged();
    };

    return (
        <SettingsSubPage title={t.settings.networkRpc} onBack={onBack}>
            <div className="space-y-4">
                <div className="bg-pax-surface rounded-2xl p-4 space-y-2">
                    <div className="flex justify-between">
                        <span className="text-xs text-pax-muted">{t.settings.network}</span>
                        <span className="text-xs font-medium">HyperPaxeer</span>
                    </div>
                    <div className="flex justify-between">
                        <span className="text-xs text-pax-muted">Chain ID</span>
                        <span className="text-xs font-mono">{PAXEER_CONFIG.chainId}</span>
                    </div>
                </div>

                <div className="bg-pax-surface rounded-2xl p-4 space-y-3">
                    <p className="text-sm font-semibold">{t.settings.customRpc}</p>
                    <input
                        type="url"
                        value={customRpc}
                        onChange={(e) => setCustomRpc(e.target.value)}
                        placeholder={t.settings.rpcPlaceholder}
                        className="w-full px-3 py-2.5 rounded-xl bg-white/[0.06]   text-sm outline-none  placeholder:text-white/20"
                    />
                    <p className="text-[11px] text-pax-muted">{t.settings.rpcHint}</p>
                    {status && <p className="text-[11px] text-emerald-400" role="status">{status}</p>}
                    {error && <p className="text-[11px] text-red-400" role="alert">{error}</p>}
                    <button onClick={handleSave} disabled={!customRpc.trim() || testing}
                        className="w-full py-2.5 rounded-xl bg-pax-accent text-black text-sm font-semibold press-scale disabled:opacity-30">
                        {testing ? p.testingRpc : saved ? (
                            <span className="inline-flex items-center gap-1.5">
                                <Check aria-hidden="true" className="h-4 w-4" />
                                {t.settings.saved}
                            </span>
                        ) : t.settings.saveRpc}
                    </button>
                    <button
                        onClick={handleReset}
                        disabled={!preferencesRepository.read().customRpc}
                        className="w-full py-2.5 rounded-xl bg-white/[0.06] text-sm font-medium press-scale disabled:opacity-30"
                    >
                        {p.defaultRpc}
                    </button>
                </div>
            </div>
        </SettingsSubPage>
    );
}

function AdvancedView({ onBack }: { onBack: () => void }) {
    const { t } = useLocale();
    const initial = preferencesRepository.read();
    const [devMode, setDevMode] = useState(initial.developerMode);
    const [hexData, setHexData] = useState(initial.showHexData);
    const storedNonce = initial.customNonce;
    const [nonceMode, setNonceMode] = useState<'auto' | 'manual'>(storedNonce !== null ? 'manual' : 'auto');
    const [customNonce, setCustomNonce] = useState(storedNonce !== null ? String(storedNonce) : '');
    const [feeMode, setFeeMode] = useState(initial.feeMode);
    const [maxFee, setMaxFee] = useState(initial.customMaxFeeGwei ?? '');
    const [priorityFee, setPriorityFee] = useState(initial.customPriorityFeeGwei ?? '');

    const handleNonceModeChange = (mode: 'auto' | 'manual') => {
        setNonceMode(mode);
        if (mode === 'auto') {
            setCustomNonce('');
            preferencesRepository.update((current) => ({ ...current, customNonce: null }));
        }
    };

    const handleNonceChange = (value: string) => {
        setCustomNonce(value);
        const parsed = parseInt(value, 10);
        if (!isNaN(parsed) && parsed >= 0) {
            preferencesRepository.update((current) => ({ ...current, customNonce: parsed }));
        } else if (value === '') {
            preferencesRepository.update((current) => ({ ...current, customNonce: null }));
        }
    };

    const handleFeeMode = (mode: typeof feeMode) => {
        setFeeMode(mode);
        preferencesRepository.update((current) => ({ ...current, feeMode: mode }));
    };

    const handleCustomFee = (
        field: 'customMaxFeeGwei' | 'customPriorityFeeGwei',
        value: string,
    ) => {
        const normalized = value.replace(/[^\d.]/g, '');
        if (field === 'customMaxFeeGwei') setMaxFee(normalized);
        else setPriorityFee(normalized);
        if (normalized === '' || /^\d+(?:\.\d{1,9})?$/.test(normalized)) {
            preferencesRepository.update((current) => ({
                ...current,
                [field]: normalized || null,
            }));
        }
    };

    return (
        <SettingsSubPage title={t.settings.advanced} onBack={onBack}>
            <div className="space-y-4">
                <div className="bg-pax-surface rounded-2xl  divide-white/[0.04] overflow-hidden">
                    <div className="px-4 py-2">
                        <Switch
                            checked={devMode}
                            label={t.settings.developerMode}
                            description={t.settings.debugInformation}
                            onCheckedChange={(next) => {
                            setDevMode(next);
                            preferencesRepository.update((current) => ({ ...current, developerMode: next }));
                        }} />
                    </div>
                    {devMode && <div className="px-4 py-2">
                        <Switch
                            checked={hexData}
                            label={t.settings.showHexData}
                            description={t.settings.displayInputData}
                            onCheckedChange={(next) => {
                            setHexData(next);
                            preferencesRepository.update((current) => ({ ...current, showHexData: next }));
                        }} />
                    </div>}
                </div>

                {devMode && <div className="bg-pax-surface rounded-2xl p-4 space-y-3">
                    <p className="text-sm font-semibold">Transaction fee</p>
                    <div className="grid grid-cols-2 gap-2">
                        {(['auto', 'economy', 'priority', 'custom'] as const).map((mode) => (
                            <button
                                key={mode}
                                onClick={() => handleFeeMode(mode)}
                                className={`rounded-lg px-3 py-2 text-xs font-medium capitalize ${feeMode === mode ? 'bg-pax-accent text-black' : 'bg-white/[0.06] text-pax-muted'}`}
                            >
                                {mode}
                            </button>
                        ))}
                    </div>
                    {feeMode === 'custom' && (
                        <div className="grid grid-cols-2 gap-2">
                            <label className="space-y-1 text-[11px] text-pax-muted">
                                Max fee (Gwei)
                                <input
                                    inputMode="decimal"
                                    value={maxFee}
                                    onChange={(event) => handleCustomFee('customMaxFeeGwei', event.target.value)}
                                    className="w-full rounded-lg bg-white/[0.06] px-3 py-2 text-sm text-white outline-none"
                                />
                            </label>
                            <label className="space-y-1 text-[11px] text-pax-muted">
                                Priority fee (Gwei)
                                <input
                                    inputMode="decimal"
                                    value={priorityFee}
                                    onChange={(event) => handleCustomFee('customPriorityFeeGwei', event.target.value)}
                                    className="w-full rounded-lg bg-white/[0.06] px-3 py-2 text-sm text-white outline-none"
                                />
                            </label>
                        </div>
                    )}
                    <p className="text-[11px] text-pax-muted">The selected fee policy is used by the self-custody signer and shown before confirmation.</p>
                </div>}

                {devMode && <div className="bg-pax-surface rounded-2xl p-4 space-y-3">
                    <div className="flex items-center justify-between">
                        <p className="text-sm font-semibold">{t.settings.customNonce}</p>
                        <div className="flex rounded-lg bg-white/[0.06] overflow-hidden">
                            <button
                                onClick={() => handleNonceModeChange('auto')}
                                className={`px-3 py-1 text-xs font-medium ${nonceMode === 'auto' ? 'bg-pax-accent text-black' : 'text-pax-muted'}`}
                            >
                                Auto
                            </button>
                            <button
                                onClick={() => handleNonceModeChange('manual')}
                                className={`px-3 py-1 text-xs font-medium ${nonceMode === 'manual' ? 'bg-pax-accent text-black' : 'text-pax-muted'}`}
                            >
                                Manual
                            </button>
                        </div>
                    </div>
                    {nonceMode === 'manual' && (
                        <>
                            <input type="number" value={customNonce} onChange={(e) => handleNonceChange(e.target.value)}
                                placeholder={t.settings.customNoncePlaceholder} min={0}
                                className="w-full px-3 py-2.5 rounded-xl bg-white/[0.06]   text-sm outline-none  placeholder:text-white/20"
                            />
                            <p className="flex items-start gap-2 text-[11px] text-amber-400/80">
                                <TriangleAlert aria-hidden="true" className="mt-0.5 h-3.5 w-3.5 shrink-0" />
                                <span>{t.settings.customNonceWarning}</span>
                            </p>
                        </>
                    )}
                    {nonceMode === 'auto' && (
                        <p className="text-[11px] text-pax-muted">Nonce is managed automatically from the chain.</p>
                    )}
                    {nonceMode === 'manual' && (
                        <p className="text-[11px] text-pax-muted">The manual nonce is consumed once and then returns to automatic mode.</p>
                    )}
                </div>}
            </div>
        </SettingsSubPage>
    );
}

function NotificationsView({ onBack }: { onBack: () => void }) {
    const { t } = useLocale();
    const prefs = [
        { key: 'tx_received', label: 'Transaction received', sub: 'Notify when PAX or tokens arrive' },
        { key: 'tx_sent', label: 'Transaction sent', sub: 'Confirm outgoing transactions' },
        { key: 'price_alert', label: 'Price alerts', sub: 'When PAX hits a target price' },
        { key: 'security', label: 'Security alerts', sub: 'Suspicious activity warnings' },
        { key: 'news', label: 'Network updates', sub: 'Paxeer protocol announcements' },
    ];
    const [enabled, setEnabled] = useState<Record<string, boolean>>(
        () => preferencesRepository.read().notifications,
    );

    const toggle = (key: string) => {
        const next = { ...enabled, [key]: !enabled[key] };
        setEnabled(next);
        preferencesRepository.update((current) => ({
            ...current,
            notifications: next,
        }));
    };

    return (
        <SettingsSubPage title={t.settings.notifications} onBack={onBack}>
            <div className="bg-pax-surface rounded-2xl  divide-white/[0.04] overflow-hidden">
                {prefs.map((p) => (
                    <div key={p.key} className="px-4 py-2">
                        <Switch
                            checked={Boolean(enabled[p.key])}
                            label={p.label}
                            description={p.sub}
                            onCheckedChange={() => toggle(p.key)}
                        />
                    </div>
                ))}
            </div>
        </SettingsSubPage>
    );
}

function ConnectedDAppsView({ onBack }: { onBack: () => void }) {
    const { t } = useLocale();
    const [sessions, setSessions] = useState<DappPermissionRecord[]>(
        () => Object.values(dappPermissionsRepository.read()),
    );

    const revoke = (origin: string) => {
        const next = sessions.filter((s) => s.origin !== origin);
        setSessions(next);
        revokeDappPermission(origin);
    };

    return (
        <SettingsSubPage title={t.shell.connectedDapps} onBack={onBack}>
            {sessions.length === 0 ? (
                <div className="flex flex-col items-center justify-center py-16 gap-3 text-center">
                    <div className="w-14 h-14 rounded-2xl bg-white/[0.06] flex items-center justify-center">
                        <SvgIcon name="external-link" className="w-6 h-6" style={{ filter: 'brightness(0) invert(0.3)' }} />
                    </div>
                    <p className="text-sm text-pax-muted">{t.settings.noConnectedDapps}</p>
                    <p className="text-xs text-pax-muted/60">{t.settings.noConnectedDappsHint}</p>
                </div>
            ) : (
                <div className="space-y-2">
                    {sessions.map((s) => (
                        <div key={s.origin} className="bg-pax-surface rounded-2xl px-4 py-3.5 flex items-center gap-3">
                            <div className="w-10 h-10 rounded-xl bg-white/[0.06] flex items-center justify-center shrink-0">
                                <SvgIcon name="external-link" className="w-4.5 h-4.5" style={{ filter: 'brightness(0) invert(0.4)' }} />
                            </div>
                            <div className="flex-1 min-w-0">
                                <p className="text-sm font-medium truncate">{new URL(s.origin).hostname}</p>
                                <p className="text-[11px] text-pax-muted truncate">{s.origin}</p>
                                <p className="text-[10px] text-pax-muted/60 font-mono">{shortenAddress(s.address, 6)} · Chain {s.chainId}</p>
                                <p className="text-[10px] text-pax-muted/60">{s.methods.length} method(s) · {new Date(s.lastUsedAt).toLocaleString()}</p>
                            </div>
                            <button onClick={() => revoke(s.origin)}
                                className="text-xs text-red-400 press-scale px-2 py-1 rounded-lg hover:bg-red-500/10 transition-colors shrink-0">
                                {t.settings.revoke}
                            </button>
                        </div>
                    ))}
                    <button onClick={() => { setSessions([]); revokeAllDappPermissions(); }}
                        className="w-full py-3 rounded-2xl bg-red-500/10 text-red-400 text-sm font-medium press-scale mt-2  ">
                        {t.settings.revokeAll}
                    </button>
                </div>
            )}
        </SettingsSubPage>
    );
}

function SettingsRow({ icon, label, subtitle, onClick }: { icon: React.ReactNode; label: string; subtitle?: string; onClick: () => void }) {
    return (
        <button onClick={onClick} className="w-full flex items-center gap-3 px-4 py-3.5 text-sm press-scale transition-colors hover:bg-white/5">
            <span className="text-pax-muted">{icon}</span>
            <div className="flex-1 text-left min-w-0">
                <span className="block">{label}</span>
                {subtitle && <span className="block text-[11px] text-pax-muted">{subtitle}</span>}
            </div>
            <SvgIcon name="chevron-right" className="w-4 h-4" style={{ filter: 'brightness(0) invert(0.3)' }} />
        </button>
    );
}
