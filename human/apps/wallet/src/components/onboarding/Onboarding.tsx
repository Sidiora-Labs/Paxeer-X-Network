'use client';

import { useEffect, useRef, useState } from 'react';
import { Loader2 } from 'lucide-react';
import { useWalletActions } from '@/providers/WalletProvider';
import { useWalletKind } from '@/providers/WalletKindProvider';
import { useEmbeddedAvailability, useOptionalEmbeddedWallet } from '@/lib/wallet';
import { PinSetup } from '@/components/auth/PinSetup';
import { MnemonicBackup } from '@/components/onboarding/MnemonicBackup';
import { RestoreWallet } from '@/components/onboarding/RestoreWallet';
import { EmbeddedSignIn } from '@/components/onboarding/EmbeddedSignIn';
import { FundedTierPicker } from '@/components/onboarding/FundedTierPicker';
import { SvgIcon } from '@/components/ui/SvgIcon';
import Image from "next/image";
import { useLocale } from '@/providers/LocaleProvider';

type Step =
    | 'welcome'
    | 'embedded-signin'
    | 'embedded-setup'
    | 'funded-signin'
    | 'funded-tier-picker'
    | 'self-custody-menu'
    | 'create-passphrase'
    | 'restore-passphrase'
    | 'backup'
    | 'restore-mnemonic'
    | 'done';

interface OnboardingProps {
    /**
     * When set, the onboarding screen jumps straight into a sub-flow on
     * mount. Used by the shell to land users in the right screen without
     * a wasted welcome-screen tap when their kind / auth state already
     * implies a specific destination:
     *
     *   - `'embedded-signin'`    — kind is embedded but no session yet.
     *   - `'embedded-setup'`     — embedded session lives but standard
     *                              wallet hasn't been provisioned.
     *   - `'funded-signin'`      — kind is funded but no session yet.
     *   - `'funded-tier-picker'` — funded session lives but no funded
     *                              account has been provisioned.
     */
    initialStep?:
    | 'embedded-signin'
    | 'embedded-setup'
    | 'funded-signin'
    | 'funded-tier-picker';
}

export function Onboarding({ initialStep }: OnboardingProps = {}) {
    const { p } = useLocale();
    const actions = useWalletActions();
    const { kind, setKind, clearKind } = useWalletKind();
    const embedded = useOptionalEmbeddedWallet();
    const embeddedAvailable = useEmbeddedAvailability();
    const [step, setStep] = useState<Step>(initialStep ?? 'welcome');
    const [mnemonic, setMnemonic] = useState('');
    const [password, setPassword] = useState('');
    const [error, setError] = useState('');

    // ── Resume flow on remount ───────────────────────────────────────
    //
    // Onboarding can mount in three flavours depending on prior state:
    //   1. Fresh install — kind=null, step='welcome'.
    //   2. Embedded user signed in but wallet not yet provisioned —
    //      kind='embedded' and isAuthenticated && !publicWallet. We need
    //      to land on the embedded-setup step which auto-provisions.
    //   3. Funded user signed in but no funded account yet —
    //      kind='funded' and isAuthenticated && !fundedSelf. We need to
    //      land on the tier picker.
    useEffect(() => {
        if (!embedded) return;
        if (step !== 'welcome') return;
        if (kind === 'embedded' && embedded.isAuthenticated && !embedded.publicWallet) {
            setStep('embedded-setup');
            return;
        }
        if (kind === 'funded' && embedded.isAuthenticated && !embedded.fundedSelf) {
            setStep('funded-tier-picker');
        }
    }, [
        embedded,
        kind,
        step,
        embedded?.isAuthenticated,
        embedded?.publicWallet,
        embedded?.fundedSelf,
    ]);

    // ── Embedded provision ────────────────────────────────────────────
    // The embedded-setup step kicks the explicit provision once and waits
    // for the provider's `publicWallet` to flip non-null (which routes
    // out of onboarding via the WalletShell `hasWallet` gate).
    const embeddedProvisionTriggeredRef = useRef(false);
    useEffect(() => {
        if (step !== 'embedded-setup') return;
        if (!embedded) return;
        if (embeddedProvisionTriggeredRef.current) return;
        if (!embedded.isAuthenticated) return;
        if (embedded.publicWallet) return;
        embeddedProvisionTriggeredRef.current = true;
        void embedded.provisionStandard().catch((e) => {
            embeddedProvisionTriggeredRef.current = false;
            setError((e as Error).message || 'Failed to set up wallet');
        });
    }, [step, embedded, embedded?.isAuthenticated, embedded?.publicWallet]);

    const handleCreatePin = async (newPin: string) => {
        setPassword(newPin);
        try {
            const result = await actions.createWallet(newPin);
            setMnemonic(result.mnemonic);
            setStep('backup');
        } catch (e) {
            setError((e as Error).message || 'Failed to create wallet');
        }
    };

    const handleBackupDone = async () => {
        // Keep onboarding mounted until the recovery phrase has been shown.
        // Activating self-custody earlier makes WalletShell replace this flow
        // with the unlocked portfolio before backup can complete.
        setKind('self-custody');
        setStep('done');
    };

    const handleRestorePin = (newPin: string) => {
        setPassword(newPin);
        setStep('restore-mnemonic');
    };

    const handleRestore = async (words: string) => {
        try {
            await actions.restoreWallet(password, words);
            setKind('self-custody');
        } catch (e) {
            setError((e as Error).message || 'Invalid mnemonic');
            throw e;
        }
    };

    // ── Welcome ─────────────────────────────────────────────────────────
    if (step === 'welcome') {
        return (
            <div className="min-h-screen flex flex-col items-center justify-center px-6 py-10">
                <div className="flex flex-col items-center gap-6 max-w-sm w-full">
                    {/* Logo */}
                    <div className="w-20 h-20 rounded-3xl bg-pax-accent/10 flex items-center justify-center">
                        <Image
                            src="/paxport_wallet.png"
                            alt="shield"
                            width={48}
                            height={48}
                            className="w-12 h-12"
                        />
                    </div>

                    <div className="text-center">
                        <h1 className="text-2xl font-bold tracking-tight">Paxeer Wallet</h1>
                        <p className="text-sm text-pax-muted mt-2">
                            Pick how you want to manage your wallet on Paxeer Network.
                        </p>
                    </div>

                    <div className="w-full flex flex-col gap-3 mt-2">
                        {/* Paxeer Wallet (embedded). Always rendered so users know the
                option exists; disables with a clear hint when the Supabase
                env vars aren't baked into the client bundle. */}
                        <button
                            onClick={() => {
                                if (!embeddedAvailable) return;
                                setKind('embedded');
                                setStep('embedded-signin');
                            }}
                            disabled={!embeddedAvailable}
                            className={
                                'w-full text-left bg-pax-surface rounded-2xl px-4 py-3.5 press-scale transition-all  ' +
                                (embeddedAvailable
                                    ? 'hover:bg-white/[0.07] '
                                    : 'opacity-60 cursor-not-allowed ')
                            }
                        >
                            <div className="flex items-center gap-3">
                                <div className="w-10 h-10 rounded-xl bg-pax-accent/10 flex items-center justify-center shrink-0 overflow-hidden">
                                    <Image
                                        src="/paxport_wallet.png"
                                        alt=""
                                        width={32}
                                        height={32}
                                        className="w-8 h-8 object-contain"
                                    />
                                </div>
                                <div className="flex-1 min-w-0">
                                    <div className="flex items-center gap-2 flex-wrap">
                                        <span className="text-sm font-bold">Paxeer Wallet</span>
                                        {embeddedAvailable ? (
                                            <span className="text-[10px] uppercase tracking-wider px-1.5 py-0.5 rounded-md bg-pax-accent/15 text-pax-accent">
                                                Recommended
                                            </span>
                                        ) : (
                                            <span className="text-[10px] uppercase tracking-wider px-1.5 py-0.5 rounded-md bg-white/[0.06] text-pax-muted">
                                                Unavailable
                                            </span>
                                        )}
                                    </div>
                                    <p className="text-[11px] text-pax-muted mt-0.5 leading-snug">
                                        {embeddedAvailable
                                            ? 'Sign in with email or social. Same wallet on every Paxeer app — no seed phrase.'
                                            : 'Supabase env vars are not set on this build. Add NEXT_PUBLIC_SUPABASE_URL + NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY and rebuild to enable.'}
                                    </p>
                                </div>
                                {embeddedAvailable && (
                                    <SvgIcon
                                        name="chevron-right"
                                        className="w-4 h-4 shrink-0"
                                        style={{ filter: 'brightness(0) invert(0.4)' }}
                                    />
                                )}
                            </div>
                        </button>

                        {/* Funded Account. Uses the same Supabase auth surface as
                            embedded, but signs through the funded policy engine
                            which gates every tx against a tier whitelist. UI hides
                            Send / Receive / Buy when in this mode. */}
                        <button
                            onClick={() => {
                                if (!embeddedAvailable) return;
                                setKind('funded');
                                setStep('funded-signin');
                            }}
                            disabled={!embeddedAvailable}
                            className={
                                'w-full text-left bg-pax-surface rounded-2xl px-4 py-3.5 press-scale transition-all  ' +
                                (embeddedAvailable
                                    ? 'hover:bg-white/[0.07] '
                                    : 'opacity-60 cursor-not-allowed ')
                            }
                        >
                            <div className="flex items-center gap-3">
                                <div className="w-10 h-10 rounded-xl bg-white/[0.06] flex items-center justify-center shrink-0">
                                    <SvgIcon
                                        name="bridge"
                                        className="w-5 h-5"
                                        style={{
                                            filter:
                                                'brightness(0) saturate(100%) invert(22%) sepia(93%) saturate(7388%) hue-rotate(222deg) brightness(98%) contrast(101%)',
                                        }}
                                    />
                                </div>
                                <div className="flex-1 min-w-0">
                                    <div className="flex items-center gap-2 flex-wrap">
                                        <span className="text-sm font-bold">Funded Account</span>
                                        <span className="text-[10px] uppercase tracking-wider px-1.5 py-0.5 rounded-md bg-white/[0.06] text-pax-muted">
                                            Trade with capital
                                        </span>
                                    </div>
                                    <p className="text-[11px] text-pax-muted mt-0.5 leading-snug">
                                        Trade with funded capital on whitelisted apps. No deposit, profit share applies.
                                    </p>
                                </div>
                                {embeddedAvailable && (
                                    <SvgIcon
                                        name="chevron-right"
                                        className="w-4 h-4 shrink-0"
                                        style={{ filter: 'brightness(0) invert(0.4)' }}
                                    />
                                )}
                            </div>
                        </button>

                        <button
                            onClick={() => setStep('self-custody-menu')}
                            className="w-full text-left bg-pax-surface rounded-2xl px-4 py-3.5 press-scale transition-all hover:bg-white/[0.07]"
                        >
                            <div className="flex items-center gap-3">
                                <div className="w-10 h-10 rounded-xl bg-white/[0.06] flex items-center justify-center shrink-0">
                                    <SvgIcon
                                        name="key"
                                        className="w-5 h-5"
                                        style={{ filter: 'brightness(0) invert(0.7)' }}
                                    />
                                </div>
                                <div className="flex-1 min-w-0">
                                    <span className="text-sm font-bold">Self-Custody Wallet</span>
                                    <p className="text-[11px] text-pax-muted mt-0.5 leading-snug">
                                        PIN + recovery phrase. You hold the keys. Best for power users.
                                    </p>
                                </div>
                                <SvgIcon
                                    name="chevron-right"
                                    className="w-4 h-4 shrink-0"
                                    style={{ filter: 'brightness(0) invert(0.4)' }}
                                />
                            </div>
                        </button>
                    </div>

                    {error && <p className="text-red-400 text-xs text-center">{error}</p>}
                </div>
            </div>
        );
    }

    // ── Embedded sign-in ────────────────────────────────────────────────
    if (step === 'embedded-signin') {
        return (
            <EmbeddedSignIn
                onBack={() => {
                    // Roll back the kind selection so the shell renders
                    // the welcome screen again. Just resetting `step`
                    // wouldn't be enough — ShellWidget gates on `kind`
                    // and would route us right back into this screen.
                    clearKind();
                    setStep('welcome');
                }}
            />
        );
    }

    // ── Funded sign-in (reuses EmbeddedSignIn — same Supabase auth) ────
    if (step === 'funded-signin') {
        return (
            <EmbeddedSignIn
                onBack={() => {
                    clearKind();
                    setStep('welcome');
                }}
            />
        );
    }

    // ── Embedded auto-provision step ───────────────────────────────────
    // The `embeddedProvisionTriggeredRef` effect above kicks the explicit
    // provision once we land here; we just render a deterministic loading
    // state until the provider's `publicWallet` flips non-null (which
    // routes out of onboarding through the WalletShell `hasWallet` gate).
    if (step === 'embedded-setup') {
        return (
            <div className="min-h-screen flex items-center justify-center px-6 py-10">
                <div className="flex flex-col items-center gap-4 max-w-sm w-full text-center">
                    <Loader2 className="w-10 h-10 animate-spin text-pax-accent" />
                    <div>
                        <h1 className="text-lg font-bold">Setting up your wallet</h1>
                        <p className="text-sm text-pax-muted mt-1">
                            Generating an EVM account and binding it to your sign-in.
                        </p>
                    </div>
                    {error && <p className="text-red-400 text-xs">{error}</p>}
                </div>
            </div>
        );
    }

    // ── Self-custody sub-menu ──────────────────────────────────────────
    if (step === 'self-custody-menu') {
        return (
            <div className="min-h-screen flex flex-col items-center justify-center px-6 py-10">
                <div className="flex flex-col items-center gap-6 max-w-sm w-full">
                    <button
                        onClick={() => {
                            // Same rationale as the embedded/funded back
                            // buttons — clear `kind` so the shell drops
                            // us back to the welcome screen rather than
                            // re-routing into self-custody on next render.
                            clearKind();
                            setStep('welcome');
                        }}
                        className="self-start flex items-center gap-1.5 text-xs text-pax-muted press-scale"
                    >
                        <SvgIcon
                            name="arrow-left"
                            className="w-3.5 h-3.5"
                            style={{ filter: 'brightness(0) invert(0.6)' }}
                        />
                        Back
                    </button>

                    <div className="w-20 h-20 rounded-3xl bg-pax-accent/10 flex items-center justify-center">
                        <SvgIcon
                            name="key"
                            className="w-12 h-12"
                            style={{
                                filter:
                                    'brightness(0) saturate(100%) invert(22%) sepia(93%) saturate(7388%) hue-rotate(222deg) brightness(98%) contrast(101%)',
                            }}
                        />
                    </div>

                    <div className="text-center">
                        <h1 className="text-2xl font-bold tracking-tight">Self-Custody</h1>
                        <p className="text-sm text-pax-muted mt-2">
                            You own the keys. Make sure you back up your recovery phrase.
                        </p>
                    </div>

                    <div className="w-full flex flex-col gap-3 mt-2">
                        <button
                            onClick={() => setStep('create-passphrase')}
                            className="w-full flex items-center justify-center gap-2 h-13 rounded-2xl bg-pax-accent text-black font-semibold text-sm press-scale transition-all hover:brightness-110"
                            style={{ height: '52px' }}
                        >
                            <SvgIcon
                                name="plus"
                                className="w-5 h-5"
                                style={{ filter: 'brightness(0)' }}
                            />
                            Create New Wallet
                        </button>
                        <button
                            onClick={() => setStep('restore-passphrase')}
                            className="w-full flex items-center justify-center gap-2 h-13 rounded-2xl bg-white/[0.08] text-white font-medium text-sm press-scale transition-all hover:bg-white/10"
                            style={{ height: '52px' }}
                        >
                            <SvgIcon
                                name="refresh"
                                className="w-5 h-5"
                                style={{ filter: 'brightness(0) invert(1)' }}
                            />
                            Restore from Phrase
                        </button>
                    </div>

                    {error && <p className="text-red-400 text-xs text-center">{error}</p>}
                </div>
            </div>
        );
    }

    if (step === 'create-passphrase') {
        return (
            <PinSetup
                onComplete={handleCreatePin}
                onBack={() => setStep('self-custody-menu')}
                title={p.createPin}
                subtitle="This PIN protects the encrypted wallet stored in this browser"
            />
        );
    }

    if (step === 'restore-passphrase') {
        return (
            <PinSetup
                onComplete={handleRestorePin}
                onBack={() => setStep('self-custody-menu')}
                title={p.createPin}
                subtitle="Protect the restored wallet with a 6-digit PIN"
            />
        );
    }

    if (step === 'backup') {
        return <MnemonicBackup mnemonic={mnemonic} onDone={handleBackupDone} />;
    }

    if (step === 'restore-mnemonic') {
        return (
            <RestoreWallet
                onRestore={handleRestore}
                onBack={() => setStep('self-custody-menu')}
            />
        );
    }

    // done — provider will re-render with hasWallet=true
    return null;
}
