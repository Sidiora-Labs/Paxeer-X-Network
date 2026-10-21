'use client';

import { useState } from 'react';
import { ArrowLeft, ArrowRight, Loader2 } from 'lucide-react';
import { BsGoogle, BsDiscord, BsGithub, BsApple, BsTwitterX } from 'react-icons/bs';
import { useWallet } from '@/wallet/WalletProvider';
import type { IdentityProvider } from '@/wallet/identity';

const SOCIAL_PROVIDERS: Array<{ id: IdentityProvider; label: string; Icon: React.ComponentType<{ className?: string }> }> = [
    { id: 'google', label: 'Google', Icon: BsGoogle },
    { id: 'discord', label: 'Discord', Icon: BsDiscord },
    { id: 'github', label: 'GitHub', Icon: BsGithub },
    { id: 'apple', label: 'Apple', Icon: BsApple },
    { id: 'twitter', label: 'X', Icon: BsTwitterX },
];

interface EmbeddedSignInProps {
    onBack: () => void;
}

function failure(err: unknown): string {
    return err instanceof Error && err.message ? err.message : 'Sign-in failed';
}

export function EmbeddedSignIn({ onBack }: EmbeddedSignInProps) {
    const wallet = useWallet();
    const [email, setEmail] = useState('');
    const [code, setCode] = useState('');
    const [codeSent, setCodeSent] = useState(false);
    const [localError, setLocalError] = useState('');

    const busy = wallet.busy;
    const anyError = localError || wallet.error;
    const emailValid = /^\S+@\S+\.\S+$/.test(email.trim());
    const codeValid = /^\d{6,10}$/.test(code.trim());

    const handleSendCode = async () => {
        setLocalError('');
        if (!emailValid) { setLocalError('Enter a valid email address.'); return; }
        try {
            await wallet.sendEmailCode(email.trim());
            setCodeSent(true);
        } catch (err) {
            setLocalError(failure(err));
        }
    };

    const handleVerify = async () => {
        setLocalError('');
        if (!codeValid) { setLocalError('Enter the code from the email.'); return; }
        try {
            await wallet.verifyEmailCode(email.trim(), code.trim());
        } catch (err) {
            setLocalError(failure(err));
        }
    };

    const handleOAuth = async (id: IdentityProvider) => {
        setLocalError('');
        try { await wallet.signInWithProvider(id); }
        catch (err) { setLocalError(failure(err)); }
    };

    if (codeSent) {
        return (
            <div className="min-h-screen flex flex-col items-center justify-center px-6">
                <div className="flex flex-col items-center gap-6 max-w-sm w-full text-center">
                    <div className="flex size-16 items-center justify-center rounded-2xl bg-pax-accent/10">
                        <ArrowRight className="size-8 text-pax-accent" />
                    </div>
                    <div>
                        <h1 className="text-xl font-bold tracking-tight">Check Your Inbox</h1>
                        <p className="text-sm text-pax-muted mt-2 text-pretty max-w-[260px]">
                            Enter the code we emailed you, or open the sign-in link on this device.
                        </p>
                    </div>
                    {anyError && <p className="text-red-400 text-xs text-center">{anyError}</p>}
                    <div className="bg-white/[0.06] flex h-12 w-full items-center gap-3 overflow-hidden rounded-2xl pl-4 pr-1">
                        <input
                            aria-label="Email code"
                            inputMode="numeric"
                            autoComplete="one-time-code"
                            autoFocus
                            value={code}
                            onChange={(e) => setCode(e.target.value)}
                            onKeyDown={(e) => { if (e.key === 'Enter') void handleVerify(); }}
                            disabled={busy}
                            className="w-full bg-transparent text-sm outline-none placeholder:text-white/25 disabled:opacity-50"
                        />
                        <button
                            type="button"
                            aria-label="Verify code"
                            onClick={() => void handleVerify()}
                            disabled={!codeValid || busy}
                            className={`flex h-10 w-12 shrink-0 items-center justify-center rounded-xl transition-all active:scale-95 ${codeValid ? 'bg-pax-accent text-black' : 'bg-white/[0.08] text-pax-muted cursor-not-allowed'}`}
                        >
                            {busy ? <Loader2 className="size-4 animate-spin" /> : <ArrowRight className="h-5 w-5" />}
                        </button>
                    </div>
                    <button
                        onClick={() => { setCodeSent(false); setEmail(''); setCode(''); }}
                        className="text-sm text-pax-muted hover:text-white transition-colors press-scale"
                    >
                        Use a different email
                    </button>
                </div>
            </div>
        );
    }

    return (
        <div className="min-h-screen flex flex-col items-center justify-center px-5 py-10">
            <div className="flex flex-col gap-5 w-full max-w-sm">
                <button onClick={onBack} className="flex items-center gap-1.5 text-xs text-pax-muted press-scale self-start">
                    <ArrowLeft className="w-3.5 h-3.5" />
                    Back
                </button>

                <div className="text-center">
                    <h1 className="text-xl font-bold tracking-tight">Sign in to Paxeer</h1>
                    <p className="text-xs text-pax-muted mt-1.5 leading-relaxed">
                        One wallet across every Paxeer app — no seed phrase.
                    </p>
                </div>

                {anyError && <p className="text-red-400 text-xs text-center">{anyError}</p>}

                <div className="flex w-full items-center justify-center gap-2">
                    {SOCIAL_PROVIDERS.map(({ id, label, Icon }) => (
                        <button
                            key={id}
                            aria-label={label}
                            disabled={busy}
                            onClick={() => void handleOAuth(id)}
                            className="bg-white/[0.06] flex h-12 w-full items-center justify-center rounded-xl transition-all hover:bg-white/[0.10] active:scale-95 disabled:opacity-40"
                        >
                            {busy ? <Loader2 className="size-4 animate-spin" /> : <Icon className="size-4" />}
                        </button>
                    ))}
                </div>

                <div className="bg-white/[0.06] flex h-12 w-full items-center justify-start gap-3 overflow-hidden rounded-2xl pl-4 pr-1">
                    <input
                        type="email"
                        aria-label="Email"
                        autoComplete="email"
                        inputMode="email"
                        autoFocus
                        placeholder="you@example.com"
                        value={email}
                        onChange={(e) => setEmail(e.target.value)}
                        onKeyDown={(e) => { if (e.key === 'Enter') void handleSendCode(); }}
                        disabled={busy}
                        className="w-full bg-transparent text-sm outline-none placeholder:text-white/25 disabled:opacity-50"
                    />
                    <button
                        type="button"
                        aria-label="Send code"
                        onClick={() => void handleSendCode()}
                        disabled={!emailValid || busy}
                        className={`flex h-10 w-12 shrink-0 items-center justify-center rounded-xl transition-all active:scale-95 ${emailValid ? 'bg-pax-accent text-black' : 'bg-white/[0.08] text-pax-muted cursor-not-allowed'}`}
                    >
                        {busy ? <Loader2 className="size-4 animate-spin" /> : <ArrowRight className="h-5 w-5" />}
                    </button>
                </div>
            </div>
        </div>
    );
}
