/**
 * WalletControlPanel — signed-in wallet popover.
 *
 * Shows when the user clicks their address chip in the Header. Replaces the
 * old behaviour where clicking the chip re-opened the connect modal.
 *
 * What lives in here:
 *   - Account header (auth provider + email when available)
 *   - Address card with copy + explorer link
 *   - Native PAX balance, fetched live via wagmi's public client
 *   - Quick actions (Deposit / Withdraw / Receive)
 *   - Network status row
 *   - Sign out (which both wagmi-disconnects and Supabase-signs-out)
 */
"use client";

import { useEffect, useMemo, useState, type ReactNode } from "react";
import * as Popover from "@radix-ui/react-popover";
import {
  ArrowDownToLine,
  ArrowUpToLine,
  Check,
  Copy,
  ExternalLink,
  LogOut,
  Mail,
  Loader2,
  type LucideIcon,
} from "lucide-react";
import { useAccount, useBalance, useChainId, useDisconnect } from "wagmi";
import { formatUnits } from "viem";
import { paxeerChain } from "@/core/chain";
import { usePaxeer, PAXEER_CONNECTOR_ID } from "@/core/wallet/embedded";
import { cn } from "@/ui/utils/cn";

interface WalletControlPanelProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** The element the popover anchors to (the address chip in the header). */
  trigger: ReactNode;
  onDeposit?: () => void;
  onWithdraw?: () => void;
}

export function WalletControlPanel({
  open,
  onOpenChange,
  trigger,
  onDeposit,
  onWithdraw,
}: WalletControlPanelProps) {
  return (
    <Popover.Root open={open} onOpenChange={onOpenChange}>
      <Popover.Trigger asChild>{trigger}</Popover.Trigger>
      <Popover.Portal>
        <Popover.Content
          align="end"
          sideOffset={8}
          className={cn(
            "z-50 w-[340px] overflow-hidden rounded-2xl",
            "border border-border bg-surface-1",
            "shadow-[0_24px_48px_rgba(0,0,0,0.4)] focus:outline-none",
            "data-[state=open]:animate-in data-[state=open]:zoom-in-95",
          )}
        >
          <PanelBody
            close={() => onOpenChange(false)}
            onDeposit={onDeposit}
            onWithdraw={onWithdraw}
          />
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  );
}

// ─── Body ─────────────────────────────────────────────────────────────────────

function PanelBody({
  close,
  onDeposit,
  onWithdraw,
}: {
  close: () => void;
  onDeposit?: () => void;
  onWithdraw?: () => void;
}) {
  const paxeer = usePaxeer();
  const { address, connector } = useAccount();
  const { disconnect } = useDisconnect();
  const chainId = useChainId();
  const isEmbedded = connector?.id === PAXEER_CONNECTOR_ID;

  const balance = useBalance({
    address,
    chainId: paxeerChain.id,
    query: { enabled: Boolean(address), refetchInterval: 15_000 },
  });

  const [copied, setCopied] = useState(false);
  useEffect(() => {
    if (!copied) return;
    const t = setTimeout(() => setCopied(false), 1500);
    return () => clearTimeout(t);
  }, [copied]);

  const explorerUrl = useMemo(() => {
    if (!address) return null;
    const base = paxeerChain.blockExplorers?.default?.url;
    return base ? `${base}/address/${address}` : null;
  }, [address]);

  const onCorrectChain = chainId === paxeerChain.id;

  async function copy() {
    if (!address) return;
    try {
      await navigator.clipboard.writeText(address);
      setCopied(true);
    } catch {
      /* ignore */
    }
  }

  async function handleSignOut() {
    try {
      // wagmi.disconnect first so the UI flips immediately; the Paxeer
      // connector's disconnect handler also calls paxeer.signOut(), so this is
      // idempotent for the embedded path. For injected wallets we still want
      // to clear any Supabase session that's lingering.
      disconnect();
      if (paxeer.session) await paxeer.signOut();
    } finally {
      close();
    }
  }

  if (!address) return null;

  const balanceLabel = balance.data
    ? `${Number(formatUnits(balance.data.value, balance.data.decimals)).toLocaleString(undefined, { maximumFractionDigits: 4 })} ${balance.data.symbol}`
    : balance.isLoading
      ? "…"
      : "—";

  return (
    <div className="flex flex-col">
      {/* ── Account header ── */}
      <div className="flex items-center gap-3 border-b border-border px-4 py-3.5">
        <div className="flex h-9 w-9 items-center justify-center rounded-full bg-blue-500/10 text-blue-400">
          <Mail size={16} />
        </div>
        <div className="min-w-0 flex-1">
          <p className="truncate text-[13px] font-medium text-foreground">
            {paxeer.user?.email ?? (isEmbedded ? "Paxeer Wallet" : connector?.name ?? "Connected")}
          </p>
          <p className="truncate text-[11px] text-muted-foreground">
            {isEmbedded ? "Embedded · server-signed" : `${connector?.name ?? "Injected"} · browser`}
          </p>
        </div>
      </div>

      {/* ── Address card ── */}
      <div className="px-4 pt-4">
        <p className="text-[10px] uppercase tracking-wider text-muted-foreground">Address</p>
        <button
          type="button"
          onClick={copy}
          className={cn(
            "mt-1 flex w-full items-center justify-between gap-3 rounded-xl",
            "border border-border bg-surface-2 px-3 py-2.5",
            "font-mono text-[12px] text-foreground",
            "transition-colors hover:border-muted hover:bg-muted/30",
          )}
        >
          <span className="truncate">{address}</span>
          {copied ? (
            <Check size={14} className="shrink-0 text-emerald-500" />
          ) : (
            <Copy size={14} className="shrink-0 text-muted-foreground" />
          )}
        </button>

        <div className="mt-2 flex items-center justify-between text-[11px]">
          <span className="text-muted-foreground">Balance</span>
          <span className="font-mono font-medium text-foreground">{balanceLabel}</span>
        </div>
      </div>

      {/* ── Network status ── */}
      <div className="mx-4 mt-3 flex items-center justify-between rounded-lg bg-surface-2 px-3 py-2 text-[11px]">
        <span className="flex items-center gap-2 text-muted-foreground">
          <span
            className={cn(
              "h-1.5 w-1.5 rounded-full",
              onCorrectChain ? "bg-emerald-500" : "bg-amber-500",
            )}
          />
          {onCorrectChain ? paxeerChain.name : `Wrong network · chain ${chainId}`}
        </span>
        <span className="font-mono text-muted-foreground">id {paxeerChain.id}</span>
      </div>

      {/* ── Quick actions ── */}
      <div className="grid grid-cols-2 gap-2 px-4 pt-3">
        <ActionButton
          icon={ArrowDownToLine}
          label="Deposit"
          onClick={() => {
            onDeposit?.();
            close();
          }}
          disabled={!onDeposit}
        />
        <ActionButton
          icon={ArrowUpToLine}
          label="Withdraw"
          onClick={() => {
            onWithdraw?.();
            close();
          }}
          disabled={!onWithdraw}
        />
      </div>

      {/* ── Footer links ── */}
      <div className="mt-3 flex flex-col border-t border-border">
        {explorerUrl && (
          <a
            href={explorerUrl}
            target="_blank"
            rel="noreferrer"
            className="flex items-center justify-between px-4 py-3 text-[12px] text-muted-foreground transition-colors hover:bg-muted/20 hover:text-foreground"
          >
            <span>View on PaxScan</span>
            <ExternalLink size={13} />
          </a>
        )}
        <button
          type="button"
          onClick={handleSignOut}
          className="flex items-center justify-between px-4 py-3 text-[12px] text-muted-foreground transition-colors hover:bg-muted/20 hover:text-red-400"
        >
          <span>{isEmbedded ? "Sign out" : "Disconnect"}</span>
          <LogOut size={13} />
        </button>
      </div>

      {paxeer.isLoading && (
        <p className="flex items-center justify-center gap-1.5 border-t border-border bg-surface-2/50 px-4 py-2 text-[11px] text-muted-foreground">
          <Loader2 className="h-3 w-3 animate-spin" />
          Refreshing wallet…
        </p>
      )}
    </div>
  );
}

// ─── Pieces ───────────────────────────────────────────────────────────────────

function ActionButton({
  icon: Icon,
  label,
  onClick,
  disabled,
}: {
  icon: LucideIcon;
  label: string;
  onClick: () => void;
  disabled?: boolean;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      className={cn(
        "flex h-9 items-center justify-center gap-1.5 rounded-lg",
        "border border-border bg-surface-2 text-[12px] font-medium text-foreground",
        "transition-colors hover:border-muted hover:bg-muted/30",
        "disabled:cursor-not-allowed disabled:opacity-40",
      )}
    >
      <Icon size={13} />
      {label}
    </button>
  );
}
