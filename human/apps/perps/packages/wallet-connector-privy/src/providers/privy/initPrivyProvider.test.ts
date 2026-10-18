import type { Chain } from "viem/chains";
import { describe, expect, it } from "vitest";
import { Network } from "../../types";
import { buildPrivyClientConfig } from "./initPrivyProvider";

const evmChain = { id: 42161, name: "Arbitrum" } as Chain;
const solanaChain = { id: 900900900, name: "Solana" } as Chain;

const privyConfig = {
  appid: "test-app",
  config: {},
};

describe("buildPrivyClientConfig", () => {
  it("passes EVM chains through with a default chain", () => {
    const config = buildPrivyClientConfig({
      initChains: [solanaChain, evmChain],
      privyConfig,
      network: Network.mainnet,
    });

    expect(config.supportedChains).toEqual([evmChain]);
    expect(config.defaultChain?.id).toBe(42161);
  });

  it("omits supportedChains and defaultChain for a Solana-only broker", () => {
    const config = buildPrivyClientConfig({
      initChains: [solanaChain],
      privyConfig,
      network: Network.mainnet,
    });

    // Privy throws on an empty supportedChains array; the fields must be
    // absent so the SDK falls back to its own defaults
    expect(config).not.toHaveProperty("supportedChains");
    expect(config).not.toHaveProperty("defaultChain");

    // login and embedded Solana wallets keep working
    expect(config.loginMethods).toEqual(["email", "google", "twitter"]);
    expect(config.embeddedWallets?.solana).toMatchObject({
      createOnLogin: "all-users",
    });
  });

  it("ignores user-provided supportedChains and defaultChain", () => {
    const config = buildPrivyClientConfig({
      initChains: [solanaChain, evmChain],
      privyConfig: {
        appid: "test-app",
        config: {
          // @ts-expect-error -- deliberately wrong shape; the SDK owns chains
          supportedChains: [{ id: 1 }],
          defaultChain: { id: 1 } as Chain,
        },
      },
      network: Network.mainnet,
    });

    expect(config.supportedChains).toEqual([evmChain]);
    expect(config.defaultChain?.id).toBe(42161);
  });
});
