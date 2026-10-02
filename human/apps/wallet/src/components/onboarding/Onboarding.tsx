'use client';

import { useState } from 'react';
import { Loader2 } from 'lucide-react';
import { useWallet } from '@/wallet/WalletProvider';
import { EmbeddedSignIn } from '@/components/onboarding/EmbeddedSignIn';
import { SvgIcon } from '@/components/ui/SvgIcon';
import Image from "next/image";

type Step = 'welcome' | 'embedded-signin' | 'embedded-setup';

interface OnboardingProps {
    initialStep?: 'embedded-signin' | 'embedded-setup';
}

export function Onboarding({ initialStep }: OnboardingProps = {}) {
    const wallet = useWallet();
    const [step, setStep] = useState<Step>(initialStep ?? 'welcome');
    const [error, setError] = useState('');

    const connectInjected = async (uuid: string) => {
        setError('');
        try {
            await wallet.connectInjected(uuid);
        } catch (err) {
            setError(err instanceof Error && err.message ? err.message : 'The wallet did not connect');
        }
    };

    if (step === 'embedded-setup' || (wallet.status === 'connecting' && wallet.mode === 'embedded')) {
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
                    {wallet.error && <p className="text-red-400 text-xs">{wallet.error}</p>}
                </div>
            </div>
        );
    }

    if (step === 'embedded-signin') {
        return <EmbeddedSignIn onBack={() => setStep('welcome')} />;
    }

    const embeddedAvailable = wallet.embeddedAvailable;

    return (
        <div className="min-h-screen flex flex-col items-center justify-center px-6 py-10">
            <div className="flex flex-col items-center gap-6 max-w-sm w-full">
                <div className="w-20 h-20 rounded-3xl bg-pax-accent/10 flex items-center justify-center">
                    <Image
                        src="/wallet/paxport_wallet.png"
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
                    <button
                        onClick={() => {
                            if (!embeddedAvailable) return;
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
                                    src="/wallet/paxport_wallet.png"
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
                                        : wallet.configError ?? 'The embedded wallet is not configured on this build.'}
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

                    {wallet.injected.map((detail) => (
                        <button
                            key={detail.info.uuid}
                            onClick={() => void connectInjected(detail.info.uuid)}
                            disabled={wallet.busy}
                            className="w-full text-left bg-pax-surface rounded-2xl px-4 py-3.5 press-scale transition-all hover:bg-white/[0.07] disabled:opacity-60"
                        >
                            <div className="flex items-center gap-3">
                                <div className="w-10 h-10 rounded-xl bg-white/[0.06] flex items-center justify-center shrink-0 overflow-hidden">
                                    <img src={detail.info.icon} alt="" className="w-7 h-7 object-contain" />
                                </div>
                                <div className="flex-1 min-w-0">
                                    <span className="text-sm font-bold">{detail.info.name}</span>
                                    <p className="text-[11px] text-pax-muted mt-0.5 leading-snug">
                                        Connect the browser wallet you already hold the keys for.
                                    </p>
                                </div>
                                <SvgIcon
                                    name="chevron-right"
                                    className="w-4 h-4 shrink-0"
                                    style={{ filter: 'brightness(0) invert(0.4)' }}
                                />
                            </div>
                        </button>
                    ))}
                </div>

                {(error || wallet.error) && <p className="text-red-400 text-xs text-center">{error || wallet.error}</p>}
            </div>
        </div>
    );
}
