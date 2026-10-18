import test from "node:test";
import assert from "node:assert/strict";
import { nativeRequest } from "../packages/paxeer-backend/src/nativeClient";
import { updateNativeSnapshot } from "../packages/paxeer-backend/src/runtime";
import { COLLATERAL_TOKENS } from "../packages/paxeer-backend/src/core/perps/config";

const wallet = "0x00000000000000000000000000000000000000a1";
const usdc = COLLATERAL_TOKENS.usdc;
const vaultEvents = [
  {
    id: "deposit-1",
    eventType: "Deposit",
    tokenAddress: usdc.address,
    amount: (25n * 10n ** BigInt(usdc.decimals)).toString(),
    blockNumber: "10",
    txHash: "0xdeposit",
    blockTimestamp: "1000",
  },
  {
    id: "withdraw-1",
    eventType: "Withdraw",
    tokenAddress: usdc.address,
    amount: (7n * 10n ** BigInt(usdc.decimals)).toString(),
    blockNumber: "11",
    txHash: "0xwithdraw",
    blockTimestamp: "2000",
  },
];

const signed = (row: any) =>
  row.side === "WITHDRAW" ? -row.amount : row.amount;

test("asset history returns only the requested direction with the row's own sign", async () => {
  const fetchBefore = globalThis.fetch;
  globalThis.fetch = async (_url, init) => {
    const body = JSON.parse(String(init?.body));
    assert.match(body.query, /vaultEvents/);
    assert.equal(body.variables.userAddress, wallet);
    return Response.json({ data: { vaultEvents } });
  };
  updateNativeSnapshot({ address: wallet, connected: true });
  try {
    const deposits = await nativeRequest(
      "/v1/asset/history?page=1&size=10&side=DEPOSIT",
    );
    assert.deepEqual(
      deposits.data.rows.map((row: any) => row.id),
      ["deposit-1"],
    );
    assert.deepEqual(deposits.data.rows.map(signed), [25]);
    assert.equal(deposits.data.meta.total, 1);

    const withdrawals = await nativeRequest(
      "/v1/asset/history?page=1&size=10&side=WITHDRAW",
    );
    assert.deepEqual(
      withdrawals.data.rows.map((row: any) => row.id),
      ["withdraw-1"],
    );
    assert.deepEqual(withdrawals.data.rows.map(signed), [-7]);
    assert.equal(withdrawals.data.meta.total, 1);

    const all = await nativeRequest("/v1/asset/history?page=1&size=10");
    assert.deepEqual(
      all.data.rows.map((row: any) => row.side),
      ["DEPOSIT", "WITHDRAW"],
    );
  } finally {
    globalThis.fetch = fetchBefore;
    updateNativeSnapshot({ address: null, connected: false });
  }
});
