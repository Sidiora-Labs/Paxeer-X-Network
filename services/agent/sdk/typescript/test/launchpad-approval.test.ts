import assert from "node:assert/strict";

import {
  LAUNCHPAD_PRECOMPILE,
  PrecompileAbiError,
  abiSelector,
  launchpadApproval,
  launchpadBuyCall,
  launchpadCreateMarketCall,
  launchpadSellCall,
  sendLaunchpadBuy,
  sendLaunchpadCreateMarket,
  sendLaunchpadSell,
  type Eip1193Requester,
} from "../src/index.js";

const word = (value: bigint | string): string =>
  (typeof value === "bigint" ? value.toString(16) : value.slice(2)).padStart(64, "0");
const address = (byte: string): string => `0x${byte.repeat(20)}`;

const owner = address("12");
const usdl = address("a1");
const token = address("b2");
const pool = address("c3");
const creationFee = 5_000_000n;
const approveSelector = abiSelector("approve(address,uint256)");
assert.equal(approveSelector, "0x095ea7b3");

class Chain implements Eip1193Requester {
  readonly sent: { to: string; data: string }[] = [];
  readonly allowances = new Map<string, bigint>();
  approveStatus = "0x1";
  private readonly receipts = new Map<string, string>();

  async request({ method, params = [] }: Readonly<{ method: string; params?: readonly unknown[] }>): Promise<unknown> {
    const call = params[0] as { from?: string; to: string; data: string };
    switch (method) {
      case "eth_call":
        if (call.to === LAUNCHPAD_PRECOMPILE && call.data === abiSelector("usdlAddress()")) return `0x${word(usdl)}`;
        if (call.to === LAUNCHPAD_PRECOMPILE && call.data === abiSelector("creationFee()")) return `0x${word(creationFee)}`;
        if (call.to === pool && call.data === abiSelector("tokenAddress()")) return `0x${word(token)}`;
        if (call.data === `${abiSelector("allowance(address,address)")}${word(owner)}${word(LAUNCHPAD_PRECOMPILE)}`) {
          return `0x${word(this.allowances.get(call.to) ?? 0n)}`;
        }
        throw new Error(`no answer for ${call.data.slice(0, 10)} at ${call.to}`);
      case "eth_sendTransaction": {
        assert.equal(call.from, owner);
        this.sent.push({ to: call.to, data: call.data });
        const hash = `0x${word(BigInt(this.sent.length))}`;
        const approving = call.data.startsWith(approveSelector);
        if (approving && this.approveStatus === "0x1") {
          this.allowances.set(call.to, BigInt(`0x${call.data.slice(74)}`));
        }
        this.receipts.set(hash, approving ? this.approveStatus : "0x1");
        return hash;
      }
      case "eth_getTransactionReceipt": {
        const status = this.receipts.get(String(params[0]));
        return status === undefined ? null : { status };
      }
      default:
        throw new Error(`unsupported ${method}`);
    }
  }
}

const approveData = (amount: bigint): string => `${approveSelector}${word(LAUNCHPAD_PRECOMPILE)}${word(amount)}`;
const order = { pool, amountIn: 250n, minOut: 1n, deadline: 1_700_000_000n };

{
  const chain = new Chain();
  await sendLaunchpadBuy(chain, owner, order);
  assert.deepEqual(chain.sent, [
    { to: usdl, data: approveData(order.amountIn) },
    { to: LAUNCHPAD_PRECOMPILE, data: launchpadBuyCall(order).data },
  ]);
}

{
  const chain = new Chain();
  await sendLaunchpadSell(chain, owner, order);
  assert.deepEqual(chain.sent, [
    { to: token, data: approveData(order.amountIn) },
    { to: LAUNCHPAD_PRECOMPILE, data: launchpadSellCall(order).data },
  ]);
}

{
  const chain = new Chain();
  await sendLaunchpadCreateMarket(chain, owner, "Paxeer Dog", "PDOG", 2);
  assert.deepEqual(chain.sent, [
    { to: usdl, data: approveData(creationFee) },
    { to: LAUNCHPAD_PRECOMPILE, data: launchpadCreateMarketCall("Paxeer Dog", "PDOG", 2).data },
  ]);
}

{
  const chain = new Chain();
  chain.allowances.set(usdl, order.amountIn);
  assert.equal(await launchpadApproval(chain, owner, { kind: "buy", order }), undefined);
  await sendLaunchpadBuy(chain, owner, order);
  assert.deepEqual(chain.sent, [{ to: LAUNCHPAD_PRECOMPILE, data: launchpadBuyCall(order).data }]);
}

{
  const chain = new Chain();
  chain.allowances.set(token, order.amountIn - 1n);
  assert.deepEqual(await launchpadApproval(chain, owner, { kind: "sell", order }), { to: token, data: approveData(order.amountIn), value: 0n });
}

{
  const chain = new Chain();
  chain.approveStatus = "0x0";
  await assert.rejects(sendLaunchpadBuy(chain, owner, order), PrecompileAbiError);
  assert.deepEqual(chain.sent, [{ to: usdl, data: approveData(order.amountIn) }]);
}
