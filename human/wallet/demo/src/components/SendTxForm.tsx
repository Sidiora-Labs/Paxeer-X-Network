'use client';

import { useState } from 'react';
import type { PaxeerWallet } from '@paxeer/wallet';
import { isAddress, nativeToWei } from '@/lib/format';

interface SendTxFormProps {
  paxeer: PaxeerWallet;
  explorerUrl?: string | null;
  onBack: () => void;
}

type Status =
  | { kind: 'idle' }
  | { kind: 'sending' }
  | { kind: 'sent'; hash: string }
  | { kind: 'error'; message: string };

/**
 * Send-transaction form, rendered inside the wallet modal. JetBrains Mono on
 * the address & amount inputs because they're data, not prose.
 */
export function SendTxForm({ paxeer, explorerUrl, onBack }: SendTxFormProps) {
  const [to, setTo] = useState('');
  const [amount, setAmount] = useState('');
  const [status, setStatus] = useState<Status>({ kind: 'idle' });

  const valid = isAddress(to) && /^\d+(\.\d+)?$/.test(amount.trim()) && amount.trim() !== '0';

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    if (!valid) return;
    setStatus({ kind: 'sending' });
    try {
      const value = nativeToWei(amount);
      const res = await paxeer.sendTransaction({ to: to as `0x${string}`, value });
      setStatus({ kind: 'sent', hash: res.tx_hash });
    } catch (err) {
      setStatus({
        kind: 'error',
        message: err instanceof Error ? err.message : 'transaction failed',
      });
    }
  }

  if (status.kind === 'sent') {
    const explorer = explorerUrl ? `${explorerUrl.replace(/\/$/, '')}/tx/${status.hash}` : null;
    return (
      <div className="flex flex-col items-center gap-4 px-6 py-8 text-center">
        <div className="flex h-12 w-12 items-center justify-center rounded-full bg-[#05c168]/10 text-[#05c168]">
          <svg width="24" height="24" viewBox="0 0 24 24" fill="none" aria-hidden="true">
            <path
              d="M5 12l5 5L20 7"
              stroke="currentColor"
              strokeWidth="2.5"
              strokeLinecap="round"
              strokeLinejoin="round"
            />
          </svg>
        </div>
        <div>
          <h3 className="text-[18px] text-neutral-100">Transaction submitted</h3>
          <p className="mt-1 text-[14px] text-neutral-400">
            Broadcast to HyperPaxeer. Confirmation in seconds.
          </p>
        </div>
        <code className="break-all rounded-lg bg-neutral-800 px-3 py-2 font-mono text-[12px] text-neutral-300">
          {status.hash}
        </code>
        <div className="flex w-full flex-col gap-2">
          {explorer && (
            <a
              href={explorer}
              target="_blank"
              rel="noreferrer"
              className="
                rounded-xl border border-neutral-700 bg-neutral-800
                px-4 py-3 text-center text-[15px] text-neutral-100
                transition-colors duration-[var(--duration-snappy)]
                ease-[var(--ease-standard)]
                hover:border-neutral-600 hover:bg-neutral-700
              "
            >
              View on explorer
            </a>
          )}
          <button
            type="button"
            onClick={onBack}
            className="
              rounded-xl bg-[#004ced] px-4 py-3 text-[15px] text-white
              transition-colors duration-[var(--duration-snappy)]
              ease-[var(--ease-standard)]
              hover:bg-[#0040c9]
            "
          >
            Done
          </button>
        </div>
      </div>
    );
  }

  return (
    <form onSubmit={submit} className="flex flex-col gap-4 px-6 py-6">
      <button
        type="button"
        onClick={onBack}
        className="
          -ml-1 flex w-fit items-center gap-1 rounded px-1 py-0.5
          text-[13px] text-neutral-400
          transition-colors duration-[var(--duration-snappy)]
          ease-[var(--ease-standard)]
          hover:text-neutral-100
        "
      >
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" aria-hidden="true">
          <path
            d="M15 18l-6-6 6-6"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
            strokeLinejoin="round"
          />
        </svg>
        Back
      </button>

      <div>
        <label htmlFor="to" className="mb-1.5 block text-[13px] text-neutral-400">
          Send to
        </label>
        <input
          id="to"
          type="text"
          autoComplete="off"
          spellCheck={false}
          value={to}
          onChange={(e) => setTo(e.target.value.trim())}
          placeholder="0x…"
          className="
            w-full rounded-xl border border-neutral-700 bg-neutral-800
            px-3.5 py-3 font-mono text-[14px] text-neutral-100
            placeholder:text-neutral-600
            transition-colors duration-[var(--duration-snappy)]
            ease-[var(--ease-standard)]
            focus:border-[#004ced] focus:outline-none
          "
        />
        {to && !isAddress(to) && (
          <p className="mt-1.5 text-[12px] text-[#ff5a65]">Not a valid 0x address.</p>
        )}
      </div>

      <div>
        <label htmlFor="amount" className="mb-1.5 block text-[13px] text-neutral-400">
          Amount (PAX)
        </label>
        <input
          id="amount"
          type="text"
          inputMode="decimal"
          autoComplete="off"
          value={amount}
          onChange={(e) => setAmount(e.target.value)}
          placeholder="0.0"
          className="
            w-full rounded-xl border border-neutral-700 bg-neutral-800
            px-3.5 py-3 font-mono text-[14px] text-neutral-100
            placeholder:text-neutral-600
            transition-colors duration-[var(--duration-snappy)]
            ease-[var(--ease-standard)]
            focus:border-[#004ced] focus:outline-none
          "
        />
      </div>

      {status.kind === 'error' && (
        <div className="rounded-lg border border-[#ff5a65]/30 bg-[#ff5a65]/5 px-3 py-2 text-[13px] text-[#ff5a65]">
          {status.message}
        </div>
      )}

      <button
        type="submit"
        disabled={!valid || status.kind === 'sending'}
        className="
          mt-2 flex h-12 items-center justify-center rounded-xl
          bg-[#004ced] text-[15px] text-white
          transition-[background,opacity] duration-[var(--duration-snappy)]
          ease-[var(--ease-standard)]
          hover:bg-[#0040c9]
          disabled:cursor-not-allowed disabled:opacity-40
        "
      >
        {status.kind === 'sending' ? 'Signing & broadcasting…' : 'Send Transaction'}
      </button>
    </form>
  );
}
