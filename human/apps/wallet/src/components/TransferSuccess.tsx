'use client';

import { useState, useEffect } from 'react';
import { observeTransfer, recoverTransferObservation, submittedTransfer, type TransferIdentity, type TransferObservation, type TransferTruth } from '@paxeer/wallet';
import { StatusLadder } from '@/account/StatusLadder';
import { getActiveRpcUrl, PAXEER_CONFIG, RPC_CHANGED_EVENT } from '@/lib/constants';
import { motion, AnimatePresence } from 'framer-motion';
import { SvgIcon } from '@/components/ui/SvgIcon';
import { openExternalUrl } from '@/lib/security/navigation';

interface TransferSuccessProps {
  fromLabel: string;
  fromAmount: string;
  fromSymbol: string;
  toLabel: string;
  toAmount?: string;
  toSymbol?: string;
  transfer: TransferIdentity;
  explorerUrl?: string;
  submissionWarning?: string;
  onExplorerView?: () => void;
  onDone: (terminal: boolean) => void;
}

const draw = {
  hidden: { pathLength: 0, opacity: 0 },
  visible: (i: number) => ({
    pathLength: 1,
    opacity: 1,
    transition: {
      pathLength: {
        delay: i * 0.2,
        type: 'spring',
        duration: 1.5,
        bounce: 0.2,
        ease: [0.22, 1, 0.36, 1],
      },
      opacity: { delay: i * 0.2, duration: 0.3 },
    },
  }),
};

function AnimatedCheckmark({ size = 80 }: { size?: number }) {
  return (
    <motion.svg
      animate="visible"
      height={size}
      initial="hidden"
      viewBox="0 0 100 100"
      width={size}
    >
      <title>Success</title>
      <motion.circle
        custom={0}
        cx="50"
        cy="50"
        r="42"
        stroke="var(--color-status-success)"
        style={{
          strokeWidth: 2,
          strokeLinecap: 'round',
          fill: 'transparent',
        }}
        variants={draw as any}
      />
      <motion.path
        custom={1}
        d="M32 50L45 63L68 35"
        stroke="var(--color-status-success)"
        style={{
          strokeWidth: 2.5,
          strokeLinecap: 'round',
          strokeLinejoin: 'round',
          fill: 'transparent',
        }}
        variants={draw as any}
      />
    </motion.svg>
  );
}

const OBSERVATION_INTERVAL_MS = 2000;
const OBSERVATION_KEY = 'paxeer.wallet.transferObservation';

const TRUTH_TITLES: Readonly<Record<TransferTruth, string>> = {
  submitted: 'Transfer Submitted',
  pending: 'Transfer Pending',
  unknown: 'Transfer Status Unknown',
  replaced: 'Transfer Replaced',
  reverted: 'Transfer Reverted',
  included: 'Transfer Included',
};

function storageKey(identity: TransferIdentity): string {
  return `${OBSERVATION_KEY}:${identity.chainId}:${identity.hash.toLowerCase()}`;
}

function recoverObservation(identity: TransferIdentity): TransferObservation {
  try {
    const raw = window.localStorage.getItem(storageKey(identity));
    return raw ? recoverTransferObservation(identity, JSON.parse(raw)) : submittedTransfer(identity);
  } catch {
    return submittedTransfer(identity);
  }
}

async function rpcRequest(url: string, method: string, params: readonly unknown[]): Promise<unknown> {
  const response = await fetch(url, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ jsonrpc: '2.0', id: 1, method, params }),
    signal: AbortSignal.timeout(10_000),
    cache: 'no-store',
  });
  if (!response.ok) throw new Error(`RPC answered HTTP ${response.status}`);
  const body = await response.json() as { jsonrpc?: unknown; id?: unknown; result?: unknown; error?: { message?: string } };
  if (body.jsonrpc !== '2.0' || body.id !== 1) throw new Error('Malformed RPC response');
  if (body.error) throw new Error(body.error.message || `RPC ${method} failed`);
  if (!Object.prototype.hasOwnProperty.call(body, 'result')) throw new Error('RPC response omitted its result');
  return body.result;
}

export function useTransferObservation(identity: TransferIdentity): { observation: TransferObservation; error: string } {
  const [observation, setObservation] = useState<TransferObservation>(() => submittedTransfer(identity));
  const [error, setError] = useState('');

  useEffect(() => {
    let cancelled = false;
    let running = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let current = recoverObservation(identity);
    setObservation(current);
    setError('');
    const explorerStatusUrl = process.env.NEXT_PUBLIC_PAXEER_EXPLORER_URL || process.env.NEXT_PUBLIC_EXPLORER_STATUS_URL || PAXEER_CONFIG.blockscoutApiBase;
    const tick = async () => {
      if (cancelled || running) return;
      if (timer) clearTimeout(timer);
      running = true;
      try {
        const rpcUrl = getActiveRpcUrl();
        const next = await observeTransfer((method, params) => rpcRequest(rpcUrl, method, params), current, { url: explorerStatusUrl });
        if (cancelled) return;
        current = next;
        setObservation(next);
        setError(next.warning ?? '');
        try {
          window.localStorage.setItem(storageKey(identity), JSON.stringify(next));
        } catch {
          setError('Observation is available, but this browser could not save it for reload.');
        }
      } catch (e) {
        if (!cancelled) {
          current = { ...current, truth: 'unknown', steps: [], blockNumber: null, blockHash: undefined };
          setObservation(current);
          setError(e instanceof Error ? e.message : 'The transaction status could not be read');
        }
      } finally {
        running = false;
        if (!cancelled) timer = setTimeout(tick, OBSERVATION_INTERVAL_MS);
      }
    };
    void tick();
    window.addEventListener('online', tick);
    window.addEventListener(RPC_CHANGED_EVENT, tick);
    return () => {
      cancelled = true;
      if (timer) clearTimeout(timer);
      window.removeEventListener('online', tick);
      window.removeEventListener(RPC_CHANGED_EVENT, tick);
    };
  }, [identity.hash, identity.chainId]);

  const matches = observation.identity.hash.toLowerCase() === identity.hash.toLowerCase() && observation.identity.chainId === identity.chainId;
  return { observation: matches ? observation : submittedTransfer(identity), error: matches ? error : '' };
}

export function TransferSuccess({
  fromLabel,
  fromAmount,
  fromSymbol,
  toLabel,
  toAmount,
  toSymbol,
  transfer,
  explorerUrl,
  submissionWarning,
  onExplorerView,
  onDone,
}: TransferSuccessProps) {
  const { observation, error } = useTransferObservation(transfer);
  const txHash = transfer.hash;
  const final = observation.truth === 'included' && observation.steps.some((step) => step.rung === 'final');
  const phase: 'processing' | 'completed' = final ? 'completed' : 'processing';

  const shortHash = txHash.length > 16
    ? `${txHash.slice(0, 10)}...${txHash.slice(-8)}`
    : txHash;

  return (
    <div data-transfer-hash={transfer.hash} data-transfer-chain={transfer.chainId} className="fixed inset-0 z-50 flex items-center justify-center bg-pax-bg/95 backdrop-blur-sm px-6">
      <motion.div
        initial={{ opacity: 0, y: 20 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.5, ease: [0.22, 1, 0.36, 1] }}
        className="w-full max-w-sm flex flex-col items-center"
      >
        {/* Animated icon */}
        <div className="relative flex items-center justify-center h-[100px] w-[100px] mb-5">
          <motion.div
            animate={{ opacity: [0, 0.8, 0.6] }}
            className={`absolute inset-0 rounded-full blur-2xl ${final ? 'bg-emerald-500/10' : 'bg-white/5'}`}
            initial={{ opacity: 0 }}
            transition={{ duration: 1.5, times: [0, 0.5, 1] }}
          />
          <AnimatePresence mode="wait">
            {phase === 'completed' ? (
              <motion.div
                key="check"
                initial={{ opacity: 0, scale: 0.5, rotate: -90 }}
                animate={{ opacity: 1, scale: 1, rotate: 0 }}
                transition={{ duration: 0.5, ease: 'easeOut' }}
              >
                <AnimatedCheckmark />
              </motion.div>
            ) : (
              <motion.div
                key="spinner"
                exit={{ opacity: 0, scale: 0.5, rotate: 180 }}
                transition={{ duration: 0.4 }}
                className="relative"
              >
                <motion.div
                  animate={{ rotate: 360 }}
                  className="absolute inset-0 rounded-full  "
                  style={{
                  }}
                  transition={{ rotate: { duration: 2, repeat: Infinity, ease: 'linear' } }}
                />
                <div className="rounded-full bg-pax-card p-4">
                  <span aria-hidden="true" className="text-2xl">{observation.truth === 'reverted' || observation.truth === 'replaced' ? '!' : '…'}</span>
                </div>
              </motion.div>
            )}
          </AnimatePresence>
        </div>

        {/* Title */}
        <AnimatePresence mode="wait">
          <motion.h2
            key={transfer.hash}
            data-truth={observation.truth}
            initial={{ opacity: 0, y: 10 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: -10 }}
            transition={{ duration: 0.4 }}
            className="text-lg font-bold mb-1"
          >
            {final ? 'Transfer Completed' : TRUTH_TITLES[observation.truth]}
          </motion.h2>
        </AnimatePresence>

        <AnimatePresence mode="wait">
          <motion.p
            key={phase}
            initial={{ opacity: 0, y: 5 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: -5 }}
            transition={{ duration: 0.3 }}
            className={`text-xs mb-5 ${observation.truth === 'reverted' ? 'text-red-400' : 'text-emerald-400'}`}
          >
            {shortHash}
          </motion.p>
        </AnimatePresence>

        <div className="w-full mb-4">
          <StatusLadder steps={observation.steps} />
          {observation.lastVerified && observation.steps.length === 0 && (
            <p data-retained-evidence className="mt-2 text-[11px] text-pax-muted">
              Saved evidence for block {observation.lastVerified.blockNumber}; current status requires revalidation.
            </p>
          )}
          {observation.replacementHash && <p className="mt-2 text-[11px] break-all">Replacement: {observation.replacementHash}</p>}
          {submissionWarning && <p role="alert" className="mt-2 text-[11px] text-pax-muted">{submissionWarning}</p>}
          {error && <p role="alert" className="mt-2 text-[11px] text-pax-muted">{error}</p>}
        </div>

        {/* Transfer card */}
        <motion.div
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          transition={{ delay: 0.2, duration: 0.5 }}
          className="w-full"
        >
          <motion.div
            animate={{ gap: phase === 'completed' ? '0px' : '8px' }}
            className="flex flex-col"
            transition={{ duration: 0.5, ease: [0.32, 0.72, 0, 1] }}
          >
            {/* From */}
            <div className={`glass-card p-3 transition-all duration-300 ${phase === 'completed' ? 'rounded-b-none ' : ''}`}>
              <span className="flex items-center gap-1 text-[10px] text-pax-muted mb-1">
                <SvgIcon name="arrow-up" className="w-3 h-3" /> From
              </span>
              <div className="flex items-center gap-2">
                <span className="inline-flex h-7 w-7 items-center justify-center rounded-lg bg-white/10 font-medium text-sm text-pax-accent">
                  {fromSymbol[0]}
                </span>
                <div className="flex flex-col">
                  <motion.span
                    animate={{ opacity: phase === 'completed' ? 1 : 0.5 }}
                    className="text-sm font-medium"
                  >
                    {fromAmount} {fromSymbol}
                  </motion.span>
                  <span className="text-[10px] text-pax-muted">{fromLabel}</span>
                </div>
              </div>
            </div>

            {/* To */}
            <div className={`glass-card p-3 transition-all duration-300 ${phase === 'completed' ? 'rounded-t-none ' : ''}`}>
              <span className="flex items-center gap-1 text-[10px] text-pax-muted mb-1">
                <SvgIcon name="arrow-down" className="w-3 h-3" /> To
              </span>
              <div className="flex items-center gap-2">
                <span className="inline-flex h-7 w-7 items-center justify-center rounded-lg bg-white/10 font-medium text-sm text-pax-accent">
                  {(toSymbol || fromSymbol)[0]}
                </span>
                <div className="flex flex-col">
                  <motion.span
                    animate={{ opacity: phase === 'completed' ? 1 : 0.5 }}
                    className="text-sm font-medium"
                  >
                    {toAmount || fromAmount} {toSymbol || fromSymbol}
                  </motion.span>
                  <span className="text-[10px] text-pax-muted">{toLabel}</span>
                </div>
              </div>
            </div>
          </motion.div>
        </motion.div>

        <AnimatePresence>
          {(
            <motion.div
              initial={{ opacity: 0, y: 10 }}
              animate={{ opacity: 1, y: 0 }}
              transition={{ delay: 0.3, duration: 0.4 }}
              className="flex gap-3 mt-6"
            >
              {(onExplorerView || explorerUrl) && (
                <button
                  onClick={() => onExplorerView ? onExplorerView() : explorerUrl ? openExternalUrl(explorerUrl) : undefined}
                  className="flex items-center gap-1.5 px-4 py-2.5 rounded-xl bg-white/5 text-xs font-medium press-scale"
                >
                  <SvgIcon name="external-link" className="w-4 h-4" style={{ filter: 'brightness(0) invert(1)' }} /> Explorer
                </button>
              )}
              <button
                onClick={() => onDone(final || observation.truth === 'reverted' || observation.truth === 'replaced')}
                className="px-6 py-2.5 rounded-xl bg-pax-accent text-black text-xs font-semibold press-scale"
              >
                {final ? 'Done' : 'Close'}
              </button>
            </motion.div>
          )}
        </AnimatePresence>
      </motion.div>
    </div>
  );
}
