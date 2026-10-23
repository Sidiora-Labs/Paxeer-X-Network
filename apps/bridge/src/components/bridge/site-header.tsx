"use client";

import { ArrowLeftRightIcon, CheckIcon, CopyIcon, TriangleAlertIcon, WalletIcon } from "lucide-react";
import { useState } from "react";
import { useAccount, useChainId, useConnect, useDisconnect, useSwitchChain } from "wagmi";

import { ThemeToggle } from "@/components/dx/theme-toggle";
import { ChainList } from "@/components/trading/chain-selector";
import { ChainIcon } from "@/components/trading/token-icon";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Spinner } from "@/components/ui/spinner";
import { toast } from "@/components/ui/sonner";
import { BRIDGE_CHAINS, paxeer, PAXEER_ROUTE_KEY } from "@/lib/bridge/chains";
import { describeWriteError } from "@/lib/bridge/errors";
import { shortenAddress } from "@/lib/trading";

function WalletButton() {
  const { address, isConnected, connector } = useAccount();
  const { connectors, connect, isPending } = useConnect();
  const { disconnect } = useDisconnect();
  const [copied, setCopied] = useState(false);

  if (!isConnected || !address) {
    if (connectors.length === 0) {
      return (
        <Button variant="tonal" size="sm" disabled>
          <WalletIcon /> No wallet found
        </Button>
      );
    }
    if (connectors.length === 1) {
      return (
        <Button
          size="sm"
          disabled={isPending}
          onClick={() =>
            connect(
              { connector: connectors[0] },
              { onError: (error) => toast.error("Could not connect", { description: describeWriteError(error) }) },
            )
          }
        >
          {isPending ? <Spinner /> : <WalletIcon />} Connect wallet
        </Button>
      );
    }
    return (
      <Popover>
        <PopoverTrigger
          render={
            <Button size="sm" disabled={isPending}>
              {isPending ? <Spinner /> : <WalletIcon />} Connect wallet
            </Button>
          }
        />
        <PopoverContent align="end" className="w-60 p-2">
          <div className="flex flex-col gap-1" role="menu" aria-label="Choose a wallet">
            {connectors.map((c) => (
              <Button
                key={c.uid}
                variant="ghost"
                size="sm"
                className="justify-start"
                onClick={() =>
                  connect(
                    { connector: c },
                    { onError: (error) => toast.error("Could not connect", { description: describeWriteError(error) }) },
                  )
                }
              >
                <WalletIcon /> {c.name}
              </Button>
            ))}
          </div>
        </PopoverContent>
      </Popover>
    );
  }

  return (
    <Popover>
      <PopoverTrigger
        render={
          <Button variant="tonal" size="sm" aria-label={`Wallet ${address}`}>
            <span aria-hidden="true" className="size-1.5 rounded-full bg-emerald-500" />
            <span className="font-mono">{shortenAddress(address)}</span>
          </Button>
        }
      />
      <PopoverContent align="end" className="w-64 p-3">
        <div className="flex flex-col gap-2">
          <span className="text-xs text-muted-foreground">Connected{connector ? ` via ${connector.name}` : ""}</span>
          <span className="font-mono text-xs break-all">{address}</span>
          <div className="flex gap-2 pt-1">
            <Button
              variant="outline"
              size="xs"
              onClick={() => {
                void navigator.clipboard?.writeText(address).then(() => setCopied(true)).catch(() => setCopied(false));
              }}
            >
              {copied ? <CheckIcon /> : <CopyIcon />} {copied ? "Copied" : "Copy"}
            </Button>
            <Button variant="outline" size="xs" onClick={() => disconnect()}>
              Disconnect
            </Button>
          </div>
        </div>
      </PopoverContent>
    </Popover>
  );
}

function NetworkSwitcher() {
  const chainId = useChainId();
  const { switchChain, isPending } = useSwitchChain();
  const [open, setOpen] = useState(false);

  const known: { id: string; name: string; chainId: number }[] = [
    { id: PAXEER_ROUTE_KEY, name: "Paxeer", chainId: paxeer.id },
    ...BRIDGE_CHAINS.flatMap((c) => (c.viemChain ? [{ id: c.key, name: c.label, chainId: c.viemChain.id }] : [])),
  ];
  const current = known.find((k) => k.chainId === chainId);

  return (
    <>
      <Button
        variant={current ? "tonal" : "outline"}
        size="sm"
        onClick={() => setOpen(true)}
        aria-label={current ? `Network: ${current.name}. Switch network` : "Unsupported network. Switch network"}
        className={current ? undefined : "border-amber-500/40 text-warning"}
      >
        {isPending ? (
          <Spinner />
        ) : current ? (
          <ChainIcon name={current.name} size="xs" />
        ) : (
          <TriangleAlertIcon />
        )}
        <span className="hidden sm:inline">{current ? current.name : "Wrong network"}</span>
      </Button>
      <Dialog open={open} onOpenChange={(o) => setOpen(o)}>
        <DialogContent className="gap-4 p-5 sm:max-w-md">
          <DialogHeader>
            <DialogTitle className="text-base">Wallet network</DialogTitle>
            <DialogDescription>
              Deposits sign on the source chain; withdrawals burn on Paxeer and release on the foreign chain.
            </DialogDescription>
          </DialogHeader>
          <ChainList
            chains={known.map((k) => ({ id: k.id, name: k.name }))}
            value={current?.id}
            wrongNetwork={!current}
            variant="compact"
            onSelect={(chain) => {
              const target = known.find((k) => k.id === chain.id);
              if (!target) return;
              setOpen(false);
              switchChain(
                { chainId: target.chainId },
                { onError: (error) => toast.error("Could not switch network", { description: describeWriteError(error) }) },
              );
            }}
          />
        </DialogContent>
      </Dialog>
    </>
  );
}

export function SiteHeader({ historyBadge }: { historyBadge?: number }) {
  const { isConnected } = useAccount();
  return (
    <header className="sticky top-0 z-40 border-b border-border bg-background/85 backdrop-blur-md">
      <div className="mx-auto flex h-14 w-full max-w-5xl items-center gap-3 px-4 sm:px-6">
        <a href="#" className="focus-ring flex items-center gap-2 rounded-md" aria-label="Paxeer Bridge home">
          <span className="grid size-8 place-items-center rounded-lg bg-primary text-primary-foreground">
            <ArrowLeftRightIcon className="size-4" aria-hidden="true" />
          </span>
          <span className="text-[15px] font-semibold tracking-tight">
            Paxeer <span className="text-muted-foreground font-medium">Bridge</span>
          </span>
        </a>
        <Badge variant="warning" className="hidden md:inline-flex">
          Attested bridge
        </Badge>
        <nav aria-label="Bridge" className="ml-2 hidden items-center gap-1 md:flex">
          <Button variant="ghost" size="sm" render={<a href="#bridge" />}>
            Bridge
          </Button>
          <Button variant="ghost" size="sm" render={<a href="#transfers" />}>
            Transfers
            {historyBadge !== undefined && historyBadge > 0 ? (
              <Badge variant="tonal" className="px-1.5">
                {historyBadge}
              </Badge>
            ) : null}
          </Button>
          <Button variant="ghost" size="sm" render={<a href="#how-it-works" />}>
            How it works
          </Button>
        </nav>
        <div className="ml-auto flex items-center gap-1.5">
          {isConnected ? <NetworkSwitcher /> : null}
          <WalletButton />
          <ThemeToggle icon="sun" appearance="ghost" variant="circle" start="pointer" />
        </div>
      </div>
    </header>
  );
}
