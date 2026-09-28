'use client';

import { useState } from 'react';
import { ArrowLeft, ArrowRight, KeyRound, Loader2 } from 'lucide-react';
import { motion } from 'framer-motion';
import { BsGoogle, BsDiscord, BsGithub, BsApple, BsTwitterX } from 'react-icons/bs';
import { useEmbeddedWallet, type EmbeddedSignInProvider } from '@/lib/wallet';

const SOCIAL_PROVIDERS: Array<{ id: EmbeddedSignInProvider; label: string; Icon: React.ComponentType<{ className?: string }> }> = [
    { id: 'google', label: 'Google', Icon: BsGoogle },
    { id: 'discord', label: 'Discord', Icon: BsDiscord },
    { id: 'github', label: 'GitHub', Icon: BsGithub },
    { id: 'apple', label: 'Apple', Icon: BsApple },
    { id: 'twitter', label: 'X', Icon: BsTwitterX },
];

const AUTH_METHODS = ['Email', 'Passkey'] as const;
type AuthMethod = typeof AUTH_METHODS[number];

interface EmbeddedSignInProps {
    onBack: () => void;
}

export function EmbeddedSignIn({ onBack }: EmbeddedSignInProps) {
    const embedded = useEmbeddedWallet();
    const [method, setMethod] = useState<AuthMethod>('Email');
    const [email, setEmail] = useState('');
    const [magicLinkSent, setMagicLinkSent] = useState(false);
    const [localError, setLocalError] = useState('');

    const busy = embedded.authBusy;
    const anyError = localError || embedded.authError;
    const emailValid = /^\S+@\S+\.\S+$/.test(email.trim());

    const handleSubmit = async () => {
        if (method === 'Passkey') return; // future
        setLocalError('');
        if (!emailValid) { setLocalError('Enter a valid email address.'); return; }
        const r = await embedded.signInWithEmail(email.trim());
        if (r.ok) { setMagicLinkSent(true); }
        else if (r.error) { setLocalError(r.error); }
    };

    const handleOAuth = async (id: EmbeddedSignInProvider) => {
        setLocalError('');
        try { await embedded.signInWithOAuth(id); }
        catch (err) { setLocalError((err as Error).message || 'Sign-in failed'); }
    };

    // ── Magic-link sent ─────────────────────────────────────────────────
    if (magicLinkSent) {
        return (
            <div className="min-h-screen flex flex-col items-center justify-center px-6">
                <div className="flex flex-col items-center gap-6 max-w-sm w-full text-center">
                    <div className="flex size-16 items-center justify-center rounded-2xl bg-pax-accent/10">
                        <ArrowRight className="size-8 text-pax-accent" />
                    </div>
                    <div>
                        <h1 className="text-xl font-bold tracking-tight">Check Your Inbox</h1>
                        <p className="text-sm text-pax-muted mt-2 text-pretty max-w-[260px]">
                            We emailed you a sign-in link. Open it on this device to finish signing in — no password needed.
                        </p>
                    </div>
                    <button
                        onClick={() => { setMagicLinkSent(false); setEmail(''); }}
                        className="text-sm text-pax-muted hover:text-white transition-colors press-scale"
                    >
                        Use a different email
                    </button>
                </div>
            </div>
        );
    }

    // ── Sign-in form ────────────────────────────────────────────────────
    return (
        <div className="min-h-screen flex flex-col items-center justify-center px-5 py-10">
            <div className="flex flex-col gap-5 w-full max-w-sm">

                {/* Back */}
                <button onClick={onBack} className="flex items-center gap-1.5 text-xs text-pax-muted press-scale self-start">
                    <ArrowLeft className="w-3.5 h-3.5" />
                    Back
                </button>

                {/* Header */}
                <div className="text-center">
                    <h1 className="text-xl font-bold tracking-tight">Sign in to Paxeer</h1>
                    <p className="text-xs text-pax-muted mt-1.5 leading-relaxed">
                        One wallet across every Paxeer app — no seed phrase.
                    </p>
                </div>

                {/* Errors */}
                {anyError && <p className="text-red-400 text-xs text-center">{anyError}</p>}

                {/* Social icon row */}
                <div className="flex w-full items-center justify-center gap-2">
                    {SOCIAL_PROVIDERS.map(({ id, label, Icon }) => (
                        <button
                            key={id}
                            aria-label={label}
                            disabled={busy}
                            onClick={() => handleOAuth(id)}
                            className="bg-white/[0.06] flex h-12 w-full items-center justify-center rounded-xl transition-all hover:bg-white/[0.10] active:scale-95 disabled:opacity-40"
                        >
                            {busy ? <Loader2 className="size-4 animate-spin" /> : <Icon className="size-4" />}
                        </button>
                    ))}
                </div>

                {/* Method tab bar */}
                <div className="bg-white/[0.06] flex h-12 w-full items-center rounded-2xl px-1">
                    <div className="relative mx-auto flex w-full items-center">
                        <ul className="mx-auto flex w-full flex-row justify-center gap-2">
                            {AUTH_METHODS.map((m) => (
                                <button
                                    key={m}
                                    onClick={() => setMethod(m)}
                                    className={`relative flex h-10 w-full cursor-pointer items-center justify-center px-3 py-1.5 text-center text-sm font-semibold transition-colors ${method === m ? 'text-white' : 'text-pax-muted'}`}
                                >
                                    {method === m && (
                                        <motion.div
                                            layoutId="signin-method-pill"
                                            className="bg-white/[0.08] absolute inset-0 rounded-xl"
                                        />
                                    )}
                                    <span className="relative select-none">{m}</span>
                                </button>
                            ))}
                        </ul>
                    </div>
                </div>

                {/* Input row */}
                <div className="bg-white/[0.06] flex h-12 w-full items-center justify-start gap-3 overflow-hidden rounded-2xl pl-4 pr-1">
                    {method === 'Passkey' ? (
                        <div className="flex items-center gap-3 w-full">
                            <KeyRound className="text-pax-muted h-5 w-5 shrink-0" />
                            <span className="text-sm text-pax-muted">Sign in with passkey</span>
                        </div>
                    ) : (
                        <input
                            type="email"
                            autoComplete="email"
                            inputMode="email"
                            autoFocus
                            placeholder="you@example.com"
                            value={email}
                            onChange={(e) => setEmail(e.target.value)}
                            onKeyDown={(e) => { if (e.key === 'Enter') handleSubmit(); }}
                            disabled={busy}
                            className="w-full bg-transparent text-sm outline-none placeholder:text-white/25 disabled:opacity-50"
                        />
                    )}
                    <button
                        type="button"
                        onClick={handleSubmit}
                        disabled={method === 'Email' ? (!emailValid || busy) : false}
                        className={`flex h-10 w-12 shrink-0 items-center justify-center rounded-xl transition-all active:scale-95 ${(method === 'Email' && emailValid) || method === 'Passkey' ? 'bg-pax-accent text-black' : 'bg-white/[0.08] text-pax-muted cursor-not-allowed'}`}
                    >
                        {busy ? <Loader2 className="size-4 animate-spin" /> : <ArrowRight className="h-5 w-5" />}
                    </button>
                </div>

                {/* Divider */}
                <div className="relative">
                    <div className="absolute inset-0 flex h-10 items-center">
                        <span className="w-full  " />
                    </div>
                    <div className="relative flex h-10 justify-center text-xs uppercase">
                        <span className="bg-pax-bg text-pax-muted flex items-center px-2 font-medium">Or</span>
                    </div>
                </div>

                {/* Self-custody shortcut */}
                <button
                    onClick={onBack}
                    className="flex h-12 w-full cursor-pointer select-none items-center justify-center gap-2 rounded-full   bg-white/[0.04] text-sm font-semibold text-white transition-all hover:bg-white/[0.08] active:scale-95"
                >
                    Self-Custody Wallet
                </button>

                <p className="text-[10px] text-pax-muted/60 text-center leading-relaxed">
                    Signing requests dispatched through{' '}
                    <span className="text-pax-muted">connect.paxportwallet.com</span>
                </p>
            </div>
        </div>
    );
}
