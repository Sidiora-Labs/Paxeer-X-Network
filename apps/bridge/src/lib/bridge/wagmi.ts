/**
 * Wagmi config: Paxeer + every EVM bridge chain, injected (EIP-6963)
 * discovery only — no project ids, no remote wallet services. The Paxeer
 * transport comes from deploy config; foreign transports fall back to the
 * viem chain defaults unless a deploy RPC override is set.
 */
import { createConfig, http } from "wagmi";
import { injected } from "wagmi/connectors";

import { BRIDGE_CHAINS, paxeer } from "./chains";
import { bridgeConfig } from "./config";

const evmViemChains = BRIDGE_CHAINS.flatMap((c) => (c.viemChain ? [c.viemChain] : []));
const evmBridgeChains = BRIDGE_CHAINS.filter((c) => c.viemChain);

export const wagmiConfig = createConfig({
  chains: [paxeer, ...evmViemChains],
  connectors: [injected()],
  transports: {
    ...Object.fromEntries(
      evmBridgeChains.map((c) => [
        c.viemChain!.id,
        http(bridgeConfig.foreignRpcUrls[c.key as keyof typeof bridgeConfig.foreignRpcUrls]),
      ]),
    ),
    [paxeer.id]: http(bridgeConfig.paxeerRpcUrl),
  },
  ssr: true,
});

declare module "wagmi" {
  interface Register {
    config: typeof wagmiConfig;
  }
}
