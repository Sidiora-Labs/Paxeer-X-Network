'use client';

import { useState, useEffect } from 'react';
import { SvgIcon } from '@/components/ui/SvgIcon';
import Image from "next/image";

interface RampWidgetProps {
    onBack: () => void;
}

type RampTab = 'buy' | 'sell';

interface Provider {
    id: string;
    name: string;
    description: string;
    logo: string;
    methods: string[];
    fees: string;
    limits: string;
    comingSoon?: boolean;
}

interface Network {
    id: string;
    name: string;
    logo: string;
    comingSoon?: boolean;
}

const fiatProviders: Provider[] = [
    {
        id: 'stripe',
        name: 'Stripe',
        description: 'Credit & debit card payments',
        logo: '/icons/fiat-ramp/stripe.png',
        methods: ['Visa', 'Mastercard', 'Apple Pay'],
        fees: '2.9% + $0.30',
        limits: '$50 – $10,000',
    },
    {
        id: 'paypal',
        name: 'PayPal',
        description: 'PayPal balance & linked accounts',
        logo: '/icons/fiat-ramp/paypal.png',
        methods: ['PayPal Balance', 'Bank'],
        fees: '3.5%',
        limits: '$10 – $25,000',
    },
    {
        id: 'skrill',
        name: 'Skrill',
        description: 'E-wallet & bank transfers',
        logo: '/icons/fiat-ramp/skrill.png',
        methods: ['Skrill Wallet', 'Bank Transfer'],
        fees: '1.9%',
        limits: '$20 – $50,000',
    },
    {
        id: 'klarna',
        name: 'Klarna',
        description: 'Pay now or in installments',
        logo: '/icons/fiat-ramp/klarna.png',
        methods: ['Bank', 'Pay Later'],
        fees: '0% (bank)',
        limits: '$10 – $5,000',
        comingSoon: true,
    },
    {
        id: 'payfast',
        name: 'PayFast',
        description: 'South African payment gateway',
        logo: '/icons/fiat-ramp/payfast.png',
        methods: ['EFT', 'Credit Card', 'Ozow'],
        fees: '3.5% + R2',
        limits: 'R100 – R500,000',
        comingSoon: true,
    },
];

const crosschainNetworks: Network[] = [
    { id: 'eth', name: 'Ethereum', logo: '/icons/fiat-ramp/eth.svg' },
    { id: 'btc', name: 'Bitcoin', logo: '/icons/fiat-ramp/btc.svg' },
    { id: 'sol', name: 'Solana', logo: '/icons/fiat-ramp/sol.svg' },
    { id: 'tron', name: 'TRON', logo: '/icons/fiat-ramp/tron.svg' },
    { id: 'xrp', name: 'XRP Ledger', logo: '/icons/fiat-ramp/xrp.svg' },
    { id: 'cro', name: 'Cronos', logo: '/icons/fiat-ramp/cro.svg', comingSoon: true },
    { id: 'uni', name: 'Unichain', logo: '/icons/fiat-ramp/uni.svg', comingSoon: true },
];

export function RampWidget({ onBack }: RampWidgetProps) {
    const [tab, setTab] = useState<RampTab>('buy');
    const [toast, setToast] = useState(false);

    useEffect(() => {
        if (!toast) return;
        const t = setTimeout(() => setToast(false), 2500);
        return () => clearTimeout(t);
    }, [toast]);

    const showToast = () => setToast(true);

    return (
        <div className="min-h-screen flex flex-col px-4 pt-4 safe-area-pt">
            <div className="flex items-center gap-3 mb-5">
                <button type="button" onClick={onBack} aria-label="Back" className="p-2 -ml-2 press-scale">
                    <SvgIcon name="arrow-left" className="w-5 h-5" style={{ filter: 'brightness(0) invert(0.6)' }} />
                </button>
                <h2 className="text-lg font-bold">Buy & Sell</h2>
            </div>

            <div className="flex gap-1.5 p-1 rounded-xl bg-white/5 mb-5">
                <button
                    onClick={() => setTab('buy')}
                    className={`flex-1 flex items-center justify-center gap-2 py-2.5 rounded-lg text-sm font-medium transition-all press-scale ${tab === 'buy' ? 'bg-pax-accent text-black' : 'text-pax-muted hover:text-white'
                        }`}
                >
                    <SvgIcon name="arrow-left" className="w-4 h-4 rotate-90" style={{ filter: 'brightness(0)' }} />
                    Buy / On-Ramp
                </button>
                <button
                    onClick={() => setTab('sell')}
                    className={`flex-1 flex items-center justify-center gap-2 py-2.5 rounded-lg text-sm font-medium transition-all press-scale ${tab === 'sell' ? 'bg-pax-accent text-black' : 'text-pax-muted hover:text-white'
                        }`}
                >
                    <SvgIcon name="arrow-left" className="w-4 h-4 -rotate-90" style={{ filter: 'brightness(0) invert(0.6)' }} />
                    Sell / Off-Ramp
                </button>
            </div>

            <div className="glass-card p-3 mb-5  ">
                <div className="flex items-center gap-3">
                    <Image
                        src="/icons/fiat-ramp/buttons-popular-payment-systems-masetcard-visa-apple-pay-google-website-rectangular-rounded-edges-vector-220153006-removebg-preview.png"
                        alt="Accepted payment methods"
                        className="h-8 object-contain"
                    />
                    <div className="flex-1 min-w-0">
                        <p className="text-[11px] text-pax-muted">
                            {tab === 'buy' ? 'Buy PAX & tokens with cards, wallets & bank transfers' : 'Sell PAX & tokens to fiat or other chains'}
                        </p>
                    </div>
                </div>
            </div>

            <div className="flex-1 pb-24 space-y-5">
                <section>
                    <div className="flex items-center gap-2 mb-3">
                        <SvgIcon name="tokens" className="w-4 h-4" style={{ filter: 'brightness(0) saturate(100%) invert(22%) sepia(93%) saturate(7388%) hue-rotate(222deg) brightness(98%) contrast(101%)' }} />
                        <h3 className="text-sm font-semibold">{tab === 'buy' ? 'Fiat On-Ramp' : 'Fiat Off-Ramp'}</h3>
                    </div>
                    <p className="text-[11px] text-pax-muted mb-3">
                        {tab === 'buy' ? 'Purchase PAX and stablecoins directly with fiat currency.' : 'Convert your PAX and tokens back to fiat currency.'}
                    </p>
                    <div className="space-y-2">
                        {fiatProviders.map((p) => (
                            <ProviderCard key={p.id} provider={p} action={tab} onTap={showToast} />
                        ))}
                    </div>
                </section>

                <section>
                    <div className="flex items-center gap-2 mb-3">
                        <SvgIcon name="globe" className="w-4 h-4" style={{ filter: 'brightness(0) saturate(100%) invert(22%) sepia(93%) saturate(7388%) hue-rotate(222deg) brightness(98%) contrast(101%)' }} />
                        <h3 className="text-sm font-semibold">{tab === 'buy' ? 'Cross-Chain On-Ramp' : 'Cross-Chain Off-Ramp'}</h3>
                    </div>
                    <p className="text-[11px] text-pax-muted mb-3">
                        {tab === 'buy' ? 'Bridge assets from other networks to Paxeer.' : 'Bridge your Paxeer assets to other networks.'}
                    </p>
                    <div className="grid grid-cols-2 gap-2">
                        {crosschainNetworks.map((n) => (
                            <NetworkCard key={n.id} network={n} action={tab} onTap={showToast} />
                        ))}
                    </div>
                </section>

                <div className="glass-card p-4   mt-4">
                    <div className="flex items-start gap-3">
                        <div className="w-10 h-10 rounded-xl bg-pax-accent/10 flex items-center justify-center shrink-0">
                            <SvgIcon name="shield" className="w-5 h-5" style={{ filter: 'brightness(0) saturate(100%) invert(22%) sepia(93%) saturate(7388%) hue-rotate(222deg) brightness(98%) contrast(101%)' }} />
                        </div>
                        <div>
                            <p className="text-sm font-semibold mb-1">Secure & Non-Custodial</p>
                            <p className="text-[11px] text-pax-muted leading-relaxed">
                                All on/off-ramp transactions are processed by regulated third-party providers.
                                Paxeer Wallet never holds your fiat — funds go directly between you and the provider.
                            </p>
                        </div>
                    </div>
                </div>
            </div>

            {toast && (
                <div className="fixed bottom-24 left-1/2 -translate-x-1/2 z-50 animate-scale-in">
                    <div className="flex items-center gap-2.5 px-5 py-3 rounded-2xl bg-pax-card   shadow-2xl">
                        <SvgIcon name="gear" className="w-4 h-4" style={{ filter: 'brightness(0) saturate(100%) invert(22%) sepia(93%) saturate(7388%) hue-rotate(222deg) brightness(98%) contrast(101%)' }} />
                        <span className="text-sm font-medium whitespace-nowrap">Integration Underway</span>
                    </div>
                </div>
            )}
        </div>
    );
}

function ProviderCard({ provider, action, onTap }: { provider: Provider; action: RampTab; onTap: () => void }) {
    return (
        <button
            onClick={onTap}
            className={`w-full glass-card p-3.5  transition-all press-scale text-left ${provider.comingSoon ? ' opacity-50 cursor-not-allowed' : '  hover:bg-white/[0.02]'
                }`}
        >
            <div className="flex items-center gap-3">
                <div className="relative w-11 h-11 rounded-xl bg-white/5 flex items-center justify-center shrink-0 overflow-hidden p-1.5">
                    <Image src={provider.logo} alt={provider.name} fill sizes="48px" className="w-full h-full object-contain" />
                </div>
                <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2">
                        <p className="text-sm font-semibold">{provider.name}</p>
                        {provider.comingSoon && (
                            <span className="text-[9px] px-1.5 py-0.5 rounded-full bg-amber-500/15 text-amber-400 font-medium">Soon</span>
                        )}
                    </div>
                    <p className="text-[11px] text-pax-muted">{provider.description}</p>
                </div>
                {!provider.comingSoon && (
                    <SvgIcon name="chevron-right" className="w-4 h-4" style={{ filter: 'brightness(0) invert(0.3)' }} />
                )}
            </div>
            <div className="flex items-center gap-4 mt-2.5 pt-2.5  ">
                <div>
                    <p className="text-[10px] text-pax-muted/60 uppercase tracking-wide">Fees</p>
                    <p className="text-[11px] text-pax-muted">{provider.fees}</p>
                </div>
                <div>
                    <p className="text-[10px] text-pax-muted/60 uppercase tracking-wide">Limits</p>
                    <p className="text-[11px] text-pax-muted">{provider.limits}</p>
                </div>
                <div className="flex-1 text-right">
                    <p className="text-[10px] text-pax-muted/60 uppercase tracking-wide">Methods</p>
                    <p className="text-[11px] text-pax-muted truncate">{provider.methods.join(', ')}</p>
                </div>
            </div>
        </button>
    );
}

function NetworkCard({ network, action, onTap }: { network: Network; action: RampTab; onTap: () => void }) {
    return (
        <button
            onClick={onTap}
            className={`glass-card p-3.5  transition-all press-scale text-left ${network.comingSoon ? ' opacity-50' : '  hover:bg-white/[0.02]'
                }`}
        >
            <div className="flex items-center gap-2.5">
                <div className="relative w-9 h-9 rounded-full bg-white/5 flex items-center justify-center shrink-0 overflow-hidden p-1">
                    <Image src={network.logo} alt={network.name} fill sizes="48px" className="w-full h-full object-contain" />
                </div>
                <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-1.5">
                        <p className="text-xs font-semibold">{network.name}</p>
                        {network.comingSoon && (
                            <span className="text-[8px] px-1 py-0.5 rounded-full bg-amber-500/15 text-amber-400 font-medium">Soon</span>
                        )}
                    </div>
                    <p className="text-[10px] text-pax-muted">
                        {action === 'buy' ? 'Bridge to Paxeer' : 'Bridge from Paxeer'}
                    </p>
                </div>
            </div>
        </button>
    );
}
