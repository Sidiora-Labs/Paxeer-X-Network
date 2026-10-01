'use client';

/**
 * Send widget — orchestrator for the transfer flow.
 *
 * Composes:
 * - {@link SendForm}             — token / recipient / amount inputs
 * - {@link TokenSelectorSheet}   — bottom-sheet token picker
 * - {@link ContactPickerSheet}   — bottom-sheet contact picker
 * - {@link SendConfirmDialog}    — confirmation drawer
 * - {@link QrScanner}            — recipient scanner
 * - {@link TransferSuccess}      — success view rendered when txHash is set
 *
 * Owns:
 * - Selected-token state (driven by `preSelectTokenAddress` prop on mount)
 * - Sheet open/close flags
 * - Form state machine (delegated to {@link useSendForm})
 *
 * Replaces the legacy `SendPage` component. Same prop contract.
 */

import { useEffect, useState, useMemo } from 'react';
import { ethers } from 'ethers';
import { useWalletState } from '@/providers/WalletProvider';
import { SvgIcon } from '@/components/ui/SvgIcon';
import { QrScanner } from '@/components/QrScanner';
import { TransferSuccess } from '@/components/TransferSuccess';
import { useContacts } from '@/hooks/useContacts';
import { loadRecentRecipients } from '@/lib/recentRecipients';
import { SendForm } from './SendForm';
import { SendConfirmation } from './SendConfirmation';
import { TokenSelectorSheet } from './TokenSelectorSheet';
import { ContactPickerSheet } from './ContactPickerSheet';
import { useSendableTokens, type SendableToken } from './useSendableTokens';
import { useSendForm } from './useSendForm';
import { parseAtomicAmount, parseEip681 } from '@/lib/eip681';
import { getActiveRpcUrl, PAXEER_CONFIG } from '@/lib/constants';
import { estimateFeeWei, resolveFeeOverrides } from '@/lib/fees';
import { preferencesRepository } from '@/platform/storage/repositories';

export interface SendWidgetProps {
    onBack: () => void;
    preSelectTokenAddress?: string;
    onPaxscan?: (path?: string) => void;
}

const matchPreselect = (
    list: SendableToken[],
    preSelect: string | undefined,
): SendableToken | null => {
    if (!list.length) return null;
    if (!preSelect) return list[0];
    if (preSelect === 'pax') return list[0];
    return (
        list.find((t) => t.address?.toLowerCase() === preSelect.toLowerCase()) ?? list[0]
    );
};

export function SendWidget({ onBack, preSelectTokenAddress, onPaxscan }: SendWidgetProps) {
    const { activeAccount, accounts } = useWalletState();
    const { contacts, findByAddress } = useContacts();
    const { tokens, loading: tokensLoading } = useSendableTokens(activeAccount?.address);
    const form = useSendForm();

    const [selectedToken, setSelectedToken] = useState<SendableToken | null>(null);
    const [selectorOpen, setSelectorOpen] = useState(false);
    const [contactPickerOpen, setContactPickerOpen] = useState(false);
    const [scannerOpen, setScannerOpen] = useState(false);
    const [scanError, setScanError] = useState('');
    const [feePreview, setFeePreview] = useState<{
        amountPax: string;
        mode: string;
        nonce: number | null;
        source: string;
    } | null>(null);

    // Other wallet accounts (excluding active) for the picker
    const ownAccounts = useMemo(
        () => accounts.filter((a) => a.address !== activeAccount?.address).map((a) => ({ address: a.address, name: a.name || 'Account' })),
        [accounts, activeAccount],
    );

    // Recent recipients loaded fresh each time the picker opens
    const [recentRecipients, setRecentRecipients] = useState(() => loadRecentRecipients());
    const handleOpenPicker = () => { setRecentRecipients(loadRecentRecipients()); setContactPickerOpen(true); };

    // ── Once tokens load, lock in the initial selection ──────────────────
    useEffect(() => {
        if (selectedToken || tokens.length === 0) return;
        setSelectedToken(matchPreselect(tokens, preSelectTokenAddress));
    }, [tokens, preSelectTokenAddress, selectedToken]);

    useEffect(() => {
        if (!selectedToken || !activeAccount?.address || !form.to || !form.amount) {
            setFeePreview(null);
            return;
        }
        let cancelled = false;
        const timer = window.setTimeout(async () => {
            try {
                const provider = new ethers.JsonRpcProvider(getActiveRpcUrl());
                const fees = await resolveFeeOverrides(provider);
                const configuredNonce = preferencesRepository.read().customNonce;
                const nonce = configuredNonce ?? await provider.getTransactionCount(
                    activeAccount.address,
                    'pending',
                );
                const gasLimit = selectedToken.address ? 500_000n : 210_000n;
                const feeWei = estimateFeeWei(gasLimit, fees);
                if (!cancelled) {
                    setFeePreview({
                        amountPax: Number(ethers.formatEther(feeWei)).toLocaleString(undefined, {
                            minimumFractionDigits: 2,
                            maximumFractionDigits: 6,
                        }),
                        mode: fees.mode,
                        nonce,
                        source: fees.source,
                    });
                }
            } catch {
                if (!cancelled) setFeePreview(null);
            }
        }, 300);
        return () => {
            cancelled = true;
            window.clearTimeout(timer);
        };
    }, [selectedToken, activeAccount?.address, form.to, form.amount]);

    // ── Success view ─────────────────────────────────────────────────────
    if (form.transfer) {
        return (
            <TransferSuccess
                fromLabel={activeAccount ? activeAccount.name || 'Account' : 'Your wallet'}
                fromAmount={form.amount}
                fromSymbol={selectedToken?.symbol || 'PAX'}
                toLabel={`${form.to.slice(0, 6)}...${form.to.slice(-4)}`}
                transfer={form.transfer}
                onExplorerView={() => onPaxscan?.(`/tx/${form.txHash}`)}
                onDone={() => {
                    form.clearTransfer();
                    onBack();
                }}
            />
        );
    }

    const resolvedContact = form.to ? findByAddress(form.to) : undefined;
    const handleScan = (data: string) => {
        const parsed = parseEip681(data);
        if (!parsed) {
            setScanError('This QR code is not a valid wallet address or EIP-681 payment request.');
            return;
        }
        if (parsed.chainId && parsed.chainId !== PAXEER_CONFIG.chainId) {
            setScanError(`This payment request is for chain ${parsed.chainId}, not chain ${PAXEER_CONFIG.chainId}.`);
            return;
        }
        form.setTo(parsed.address);
        if (parsed.tokenAddress) {
            const token = tokens.find(
                (candidate) =>
                    candidate.address?.toLowerCase() === parsed.tokenAddress?.toLowerCase(),
            );
            if (!token) {
                setScanError('The requested token is not available in this wallet.');
                return;
            }
            setSelectedToken(token);
            if (parsed.uint256) {
                const atomic = parseAtomicAmount(parsed.uint256);
                if (atomic === null) {
                    setScanError('The QR token amount is invalid.');
                    return;
                }
                form.setAmount(ethers.formatUnits(atomic, token.decimals));
            }
        } else if (parsed.value) {
            const atomic = parseAtomicAmount(parsed.value);
            if (atomic === null) {
                setScanError('The QR payment amount is invalid.');
                return;
            }
            form.setAmount(ethers.formatEther(atomic));
        } else if (parsed.amount) {
            form.setAmount(parsed.amount);
        }
        setScanError('');
        setScannerOpen(false);
    };

    return (
        <div className="min-h-screen flex flex-col px-4 pt-4 safe-area-pt">
            <div className="flex items-center gap-3 mb-6">
                <button type="button" onClick={onBack} aria-label="Back" className="p-2 -ml-2 press-scale">
                    <SvgIcon
                        name="arrow-left"
                        className="w-5 h-5"
                        style={{ filter: 'brightness(0) invert(0.6)' }}
                    />
                </button>
                <h2 className="text-lg font-bold">Send</h2>
            </div>

            <SendForm
                selectedToken={selectedToken}
                to={form.to}
                amount={form.amount}
                resolvedContact={resolvedContact}
                hasContacts={ownAccounts.length > 0 || recentRecipients.length > 0 || contacts.length > 0}
                onOpenTokenSelector={() => setSelectorOpen(true)}
                onOpenContactPicker={handleOpenPicker}
                onOpenScanner={() => setScannerOpen(true)}
                onChangeTo={form.setTo}
                onChangeAmount={form.setAmount}
                onApplyPercentage={(pct) => form.applyPercentage(pct, selectedToken)}
            />

            {form.error && <p className="text-red-400 text-xs">{form.error}</p>}
            {scanError && <p className="text-red-400 text-xs" role="alert">{scanError}</p>}

            <div className="py-4 pb-24 flex justify-center">
                <SendConfirmation
                    triggerLabel={`Send ${selectedToken?.symbol || ''}`}
                    token={selectedToken ? { symbol: selectedToken.symbol, iconUrl: selectedToken.iconUrl } : undefined}
                    amount={form.amount}
                    to={form.to}
                    loading={form.loading}
                    disabled={form.loading || !form.to || !form.amount || !selectedToken}
                    onOpen={() => form.validateBeforeConfirm(selectedToken)}
                    onConfirm={() => form.submit(selectedToken)}
                    fee={feePreview}
                />
            </div>

            <TokenSelectorSheet
                open={selectorOpen}
                onClose={() => setSelectorOpen(false)}
                tokens={tokens}
                loading={tokensLoading}
                selected={selectedToken}
                onSelect={setSelectedToken}
            />

            <QrScanner open={scannerOpen} onClose={() => setScannerOpen(false)} onScan={handleScan} />

            <ContactPickerSheet
                open={contactPickerOpen}
                onClose={() => setContactPickerOpen(false)}
                contacts={contacts}
                ownAccounts={ownAccounts}
                recentRecipients={recentRecipients}
                onSelect={form.setTo}
            />
        </div>
    );
}
