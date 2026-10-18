import assert from "node:assert/strict";
import { test } from "vitest";
import { ROBINHOOD_MAINNET_CHAINID } from "@paxeer-network/types";
import { Assets } from "../src/assets";

function fixture() {
  const requests: any[] = [];
  const assets = new Assets(
    {} as any,
    {} as any,
    {
      walletAdapter: { chainId: ROBINHOOD_MAINNET_CHAINID },
      stateValue: { address: "test", accountId: "test" },
      signer: {
        signText: async () => ({ publicKey: "test", signature: "test" }),
      },
    } as any,
  );
  (assets as any)._simpleFetch = async (url, init) => {
    requests.push({ url, body: JSON.parse(init.body) });
    return { success: true, data: { converted_id: 123 } };
  };
  return { assets, requests };
}
test("reverse request sends normalized symbols and explicit target", async () => {
  const { assets, requests } = fixture();
  const result = await assets.convert({
    converted_asset: " usdc ",
    received_asset: " usdg ",
    amount: 100,
    slippage: 0.005,
  });
  assert.equal(result.data.converted_id, 123);
  assert.deepEqual(requests[0], {
    url: "/v1/asset/manual_convert",
    body: {
      converted_asset: "USDC",
      received_asset: "USDG",
      amount: 100,
      slippage: 0.005,
    },
  });
});
test("legacy collateral to USDC remains supported without a target", async () => {
  const { assets, requests } = fixture();
  await assets.convert({
    converted_asset: "USDT",
    amount: 100,
    slippage: 0.005,
  });
  assert.equal(requests[0].body.converted_asset, "USDT");
  assert.equal("received_asset" in requests[0].body, false);
});
test("invalid pairs never reach the network", async () => {
  const { assets, requests } = fixture();
  for (const [from, to] of [
    ["USDC", undefined],
    ["USDG", "USDT"],
    ["USDG", " "],
    ["", "USDC"],
  ]) {
    await assert.rejects(
      assets.convert({
        converted_asset: from,
        received_asset: to,
        amount: 100,
        slippage: 0.005,
      }),
      /Invalid conversion pair/,
    );
  }
  assert.equal(requests.length, 0);
});
