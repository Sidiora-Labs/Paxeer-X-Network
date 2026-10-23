import { ExternalLinkIcon, LockIcon, PenLineIcon, ShieldCheckIcon } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Separator } from "@/components/ui/separator";
import { BRIDGE_CHAINS } from "@/lib/bridge/chains";
import { LAYERX_BRIDGE_ADDRESS } from "@/lib/bridge/contracts";
import { foreignAddressUrl, paxeerAddressUrl } from "@/lib/bridge/explorer";
import { shortenAddress } from "@/lib/trading";

const STEPS = [
  {
    icon: LockIcon,
    title: "Lock or burn",
    body: "A deposit locks the asset in PaxeerXVault on the source chain. A withdrawal burns the bridged denom on Paxeer. Nothing moves on the other side yet.",
  },
  {
    icon: PenLineIcon,
    title: "Attestors sign",
    body: "After the chain's finality depth, the attestor set signs the exact deposit or burn — vault, transaction, recipient, asset and amount, byte for byte.",
  },
  {
    icon: ShieldCheckIcon,
    title: "Anyone settles",
    body: "With a threshold of signatures, bridgeIn mints on Paxeer (or the vault releases back). You submit it from here; this app re-verifies every signature before your wallet opens.",
  },
] as const;

export function HowItWorks() {
  return (
    <section id="how-it-works" aria-label="How the bridge works" className="flex w-full flex-col gap-4">
      <h2 className="text-lg font-semibold tracking-tight">How it works</h2>
      <div className="grid gap-3 sm:grid-cols-3">
        {STEPS.map((step, index) => (
          <Card key={step.title} variant="tonal" className="gap-3 py-5">
            <CardHeader className="px-5">
              <div className="flex items-center gap-2">
                <span className="grid size-8 place-items-center rounded-lg bg-container-high">
                  <step.icon className="size-4" aria-hidden="true" />
                </span>
                <Badge variant="outline">Step {index + 1}</Badge>
              </div>
              <CardTitle className="text-[15px]">{step.title}</CardTitle>
            </CardHeader>
            <CardContent className="px-5">
              <CardDescription className="leading-relaxed">{step.body}</CardDescription>
            </CardContent>
          </Card>
        ))}
      </div>

      <Card variant="outline" className="gap-4 py-5">
        <CardHeader className="px-5">
          <CardTitle className="text-[15px]">Routes & contracts</CardTitle>
          <CardDescription>
            The Paxeer side is the layerxbridge precompile. Every foreign chain runs the same PaxeerXVault bytecode;
            a route opens when its vault is deployed and governance registers the chain.
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-3 px-5">
          <div className="flex flex-wrap items-center justify-between gap-2 text-sm">
            <span className="flex items-center gap-2">
              Paxeer bridge precompile
              <Badge variant="tonal" className="font-mono text-[11px]">
                {shortenAddress(LAYERX_BRIDGE_ADDRESS, 10, 6)}
              </Badge>
            </span>
            <Button
              variant="link"
              size="sm"
              render={<a href={paxeerAddressUrl(LAYERX_BRIDGE_ADDRESS)} target="_blank" rel="noreferrer" />}
            >
              Paxscan <ExternalLinkIcon />
            </Button>
          </div>
          <Separator />
          <ul className="flex flex-col divide-y divide-border/60">
            {BRIDGE_CHAINS.map((chain) => (
              <li key={chain.key} className="flex flex-wrap items-center justify-between gap-2 py-2.5 text-sm">
                <span className="flex items-center gap-2">
                  <span className="font-medium">{chain.label}</span>
                  <span className="text-xs text-muted-foreground">
                    {chain.assets.map((a) => a.symbol).join(", ")} · {chain.finalityDepth.toLocaleString()}{" "}
                    confirmations
                  </span>
                </span>
                {chain.vault ? (
                  <Button
                    variant="link"
                    size="sm"
                    render={<a href={foreignAddressUrl(chain, chain.vault)} target="_blank" rel="noreferrer" />}
                  >
                    Vault {shortenAddress(chain.vault)} <ExternalLinkIcon />
                  </Button>
                ) : (
                  <Badge variant="warning">
                    {chain.kind === "solana" ? "Program pending" : "Vault not deployed"}
                  </Badge>
                )}
              </li>
            ))}
          </ul>
          <p className="text-xs leading-relaxed text-muted-foreground">
            A note on custody: this is an attested bridge, not a light-client bridge. Funds are held by the vaults
            and move only against a threshold of attestor signatures, with per-asset caps bounding what can be in
            flight. Caps and pause state above are read from chain, live.
          </p>
        </CardContent>
      </Card>
    </section>
  );
}
