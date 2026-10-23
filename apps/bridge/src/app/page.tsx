"use client";

import { useState } from "react";

import { BridgeCard } from "@/components/bridge/bridge-card";
import { HowItWorks } from "@/components/bridge/how-it-works";
import { SiteHeader } from "@/components/bridge/site-header";
import { TransfersPanel } from "@/components/bridge/transfers-panel";
import { Badge } from "@/components/ui/badge";
import { BRIDGE_CHAINS } from "@/lib/bridge/chains";

export default function Home() {
  const [refreshKey, setRefreshKey] = useState(0);
  const liveRoutes = BRIDGE_CHAINS.filter((c) => c.state === "configured").length;

  return (
    <>
      <SiteHeader />
      <main className="mx-auto flex w-full max-w-5xl flex-1 flex-col gap-10 px-4 py-8 sm:px-6 sm:py-10">
        <div className="flex flex-col items-start gap-3">
          <Badge variant={liveRoutes > 0 ? "success" : "warning"}>
            {liveRoutes > 0
              ? `${liveRoutes} of ${BRIDGE_CHAINS.length} routes live`
              : "Routes open as vaults deploy"}
          </Badge>
          <h1 className="max-w-xl text-3xl font-semibold tracking-tight text-balance sm:text-4xl">
            Move assets to Paxeer — and back
          </h1>
          <p className="max-w-xl leading-relaxed text-muted-foreground">
            One bridge for Ethereum, Base, Arbitrum, Optimism, BNB Chain, Polygon, Avalanche, HyperEVM and Solana.
            Deposits lock in the chain's vault, attestors sign, and the bridged asset lands on Paxeer at 1:1.
          </p>
        </div>

        <div id="bridge" className="mx-auto w-full max-w-xl scroll-mt-20">
          <BridgeCard onTransferSubmitted={() => setRefreshKey((k) => k + 1)} />
        </div>

        <TransfersPanel refreshKey={refreshKey} />
        <HowItWorks />
      </main>
      <footer className="border-t border-border">
        <div className="mx-auto flex w-full max-w-5xl flex-col gap-1 px-4 py-6 text-xs text-muted-foreground sm:px-6">
          <p>
            PaxeerX bridge · precompile 0x…1016 · every crossing needs a threshold of attestor signatures. Verify the
            vault address on the source chain's explorer before depositing.
          </p>
          <p>Bridge only what you can afford to wait for — arrival is finality-bound, and caps are enforced on-chain.</p>
        </div>
      </footer>
    </>
  );
}
