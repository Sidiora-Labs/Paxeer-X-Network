'use client';

import { useState } from 'react';
import { useWalletState } from '@/providers/WalletProvider';
import { useWallet } from '@/wallet/WalletProvider';
import { shortenAddress } from '@/lib/format';
import {
    announceRpcChanged,
    PAXEER_CONFIG,
    validateRpcEndpoint,
} from '@/lib/constants';
import { Check, LogOut, Palette, TriangleAlert } from 'lucide-react';
import { SvgIcon } from '@/components/ui/SvgIcon';
import { getAvatarPath } from '@/lib/avatar';
import { NotificationToggle } from '@/components/pwa/PWAComponents';
import { InstallPrompt } from '@/pwa/InstallPrompt';
import Image from "next/image";
import { preferencesRepository } from '@/platform/storage/repositories';
import { useLocale } from '@/providers/LocaleProvider';
import { SUPPORTED_LOCALES, LOCALE_DISPLAY_NAMES, type Locale } from '@/lib/locale';
import { resetStorageForLifecycle } from '@/platform/storage/registry';
import { Switch } from '@/components/ui/primitives';
import type { ShellRouteName } from '@/domains/shell';
import {
    announceCurrencyChanged,
    ensureRates,
    FIAT_CURRENCIES,
    getRatesFreshness,
} from '@/lib/currency';
import { AppearanceSettings } from '@/theme/AppearanceSettings';
import { useThemeAccount } from '@/theme/ThemeProvider';

type SettingsView = 'main' | 'preferences' | 'appearance' | 'network' | 'advanced' | 'notifications';

interface SettingsWidgetProps {
    onNavigate?: (route: ShellRouteName) => void;
    onPaxscan?: (path?: string) => void;
}

export function SettingsWidget({ onNavigate, onPaxscan }: SettingsWidgetProps) {
    const { ready, activeAccount } = useWalletState();
    const { t } = useLocale();
    const wallet = useWallet();
    const isEmbedded = wallet.mode === 'embedded';

    const [view, setView] = useState<SettingsView>('main');
    useThemeAccount(ready ? (activeAccount?.address ?? null) : undefined);

    const handleSignOut = async () => {
        await wallet.signOut();
        resetStorageForLifecycle('logout');
    };

    if (view === 'preferences') return <PreferencesView onBack={() => setView('main')} />;
    if (view === 'appearance') return <AppearanceView onBack={() => setView('main')} />;
    if (view === 'network') return <NetworkView onBack={() => setView('main')} />;
    if (view === 'advanced') return <AdvancedView onBack={() => setView('main')} />;
    if (view === 'notifications') return <NotificationsView onBack={() => setView('main')} />;

    return (
        <div className="grid grid-cols-2 gap-2.5 px-3 pt-3 pb-4">
            <div className="col-span-2 bg-pax-surface rounded-[20px] p-4 text-left w-full">
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
                </div>
            </div>

            <div className="col-span-2 px-1 pt-1"><p className="text-[13px] font-bold text-pax-muted uppercase tracking-[0.06em]">{t.settings.addressBook}</p></div>
            <div className="col-span-2 bg-pax-surface rounded-[20px]  divide-white/[0.04] overflow-hidden">
                <SettingsRow icon={<SvgIcon name="user" className="w-4.5 h-4.5" style={{ filter: 'brightness(0) invert(0.6)' }} />} label={t.nav.contacts} subtitle={t.settings.contactsSubtitle} onClick={() => onNavigate?.('contacts')} />
            </div>

            <div className="col-span-2 px-1 pt-1"><p className="text-[13px] font-bold text-pax-muted uppercase tracking-[0.06em]">{t.account.title}</p></div>
            <div className="col-span-2 bg-pax-surface rounded-[20px]  divide-white/[0.04] overflow-hidden">
                {wallet.identity?.email && (
                    <div className="px-4 py-3.5 text-sm">
                        <p className="text-[11px] text-pax-muted uppercase tracking-wider">{t.settings.signedInAs}</p>
                        <p className="text-sm mt-0.5 truncate">{wallet.identity.email}</p>
                    </div>
                )}
                <button
                    onClick={handleSignOut}
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
            </div>

            <div className="col-span-2 px-1 pt-1"><p className="text-[13px] font-bold text-pax-muted uppercase tracking-[0.06em]">{t.settings.general}</p></div>
            <div className="col-span-2 bg-pax-surface rounded-[20px]  divide-white/[0.04] overflow-hidden">
                <SettingsRow icon={<SvgIcon name="settings" className="w-4.5 h-4.5" style={{ filter: 'brightness(0) invert(0.6)' }} />} label={t.settings.displayPreferences} subtitle={`${t.settings.currency}, ${t.settings.language}`} onClick={() => setView('preferences')} />
                <SettingsRow icon={<Palette className="w-[18px] h-[18px]" />} label="Appearance" subtitle="Theme, accent, font, size and density" onClick={() => setView('appearance')} />
                <SettingsRow icon={<SvgIcon name="wifi" className="w-4.5 h-4.5" style={{ filter: 'brightness(0) invert(0.6)' }} />} label={t.settings.networkRpc} subtitle={`${t.settings.network}, ${t.settings.customRpc}`} onClick={() => setView('network')} />
                <SettingsRow icon={<SvgIcon name="sliders" className="w-4.5 h-4.5" style={{ filter: 'brightness(0) invert(0.6)' }} />} label={t.settings.advanced} subtitle={t.settings.advancedSubtitle} onClick={() => setView('advanced')} />
                <SettingsRow icon={<SvgIcon name="bell" className="w-4.5 h-4.5" style={{ filter: 'brightness(0) invert(0.6)' }} />} label={t.settings.notifications} subtitle={t.settings.notificationsSubtitle} onClick={() => setView('notifications')} />
            </div>

            <div className="col-span-2 px-1 pt-1"><p className="text-[13px] font-bold text-pax-muted uppercase tracking-[0.06em]">{t.settings.about}</p></div>
            <div className="col-span-2 bg-pax-surface rounded-[20px] p-1 space-y-1 overflow-hidden">
                <NotificationToggle />
                <InstallPrompt />
            </div>

            <div className="col-span-2 mt-4 text-center">
                <p className="text-xs text-pax-muted/50">Paxeer Wallet v0.1.0</p>
                <p className="text-xs text-pax-muted/50">Chain {PAXEER_CONFIG.chainId}</p>
            </div>
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

function AppearanceView({ onBack }: { onBack: () => void }) {
    return (
        <SettingsSubPage title="Appearance" onBack={onBack}>
            <AppearanceSettings />
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
