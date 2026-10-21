import { describe, it } from "node:test";
import { strict as assert } from "node:assert";
import { spawnSync } from "node:child_process";
import {
  createWalletClient,
  encodeFunctionData,
  erc20Abi as tokenAbi,
  getAddress,
  http,
  isHex,
  parseEventLogs,
  size,
  type Address,
  type Hex,
  type TransactionReceipt,
} from "viem";
import { privateKeyToAccount } from "viem/accounts";
import type { CreateConnectorFn } from "wagmi";

import type { ApproveCall } from "../src/core/network/approvals";
import type { PositionWrite } from "../src/core/perps/positionWrites";
import type { buildDeposit, buildWithdraw, buildCancelOrder } from "../src/core/perps/calls";

const ENV_NAMES = ["LOCAL_NODE_RPC", "LOCAL_NODE_KEY", "NEXT_PUBLIC_PERPS_GATEWAY_URL"] as const;
type EnvName = (typeof ENV_NAMES)[number];

/** Every variable the round trip needs; an unset one fails the test with its name. */
function requireEnv(): Record<EnvName, string> {
  const env = {} as Record<EnvName, string>;
  for (const name of ENV_NAMES) {
    const value = process.env[name];
    if (!value) throw new Error(`${name} is not set`);
    env[name] = value;
  }
  if (!isHex(env.LOCAL_NODE_KEY) || size(env.LOCAL_NODE_KEY) !== 32) {
    throw new Error("LOCAL_NODE_KEY is not a 32-byte hex private key");
  }
  return env;
}

const USDL_UNIT = 1_000_000n;
const USD_TO_USDL = 1_000_000_000_000n;
const E18 = 10n ** 18n;
const DEPOSIT_AMOUNT = 100n * USDL_UNIT;
const OPEN_COLLATERAL = 50n * USDL_UNIT;
const ADDED_COLLATERAL = 10n * USDL_UNIT;
const LIMIT_COLLATERAL = 10n * USDL_UNIT;
const SPOT_QUOTE_AMOUNT = 25n * USDL_UNIT;
const LEVERAGE = 20n * E18;
const MARKET_ID = 0n;
const FEE_TYPE_TRADING = 0;
const FEE_TYPE_MAKER_REBATE = 4;
const FUNDING_INTERVAL_SECONDS = 3600n;
const FUNDING_INDEX_GRACE_MS = 120_000;
const POLL_INTERVAL_MS = 2_000;

/** Fixed gas of contracts/perps.md section 21: 2000 + 30 per input byte + the method surcharge. */
const GAS_SURCHARGE = {
  deposit: 130_000n,
  withdraw: 130_000n,
  placeOrder: 250_000n,
  cancelOrder: 20_000n,
  addCollateral: 130_000n,
  closePositionWithParams: 250_000n,
} as const;
type GasMethod = keyof typeof GAS_SURCHARGE;

type PerpsStep =
  | PositionWrite
  | ReturnType<typeof buildDeposit>
  | ReturnType<typeof buildWithdraw>
  | ReturnType<typeof buildCancelOrder>;
type Step = ApproveCall | PerpsStep;

interface GasRecord {
  method: GasMethod;
  estimate: bigint;
  fixed: bigint;
}

async function loadHarness(env: Record<EnvName, string>) {
  // The chain definition, the wagmi transports and the public client read the RPC list at module load.
  process.env.NEXT_PUBLIC_PAXEER_RPC_URLS = env.LOCAL_NODE_RPC;

  const contract = await import("../src/hooks/perps/useContract");
  const clients = await import("../src/core/viem/clients");
  const { paxeerChain } = await import("../src/core/chain");
  const { wagmiConfig } = await import("../src/core/wagmi");
  const { connect } = await import("wagmi/actions");
  const { createConnector } = await import("wagmi");
  const calls = await import("../src/core/perps/calls");
  const writes = await import("../src/core/perps/positionWrites");
  const { usePosition, perpsAllowanceSource } = await import("../src/hooks/perps/usePosition");
  const { usePerpsOrders } = await import("../src/hooks/perps/usePerpsOrders");
  const { useQuoter } = await import("../src/hooks/perps/useQuoter");
  const account = await import("../src/hooks/perps/useAccount");
  const { SPOT_NATIVE_TOKENS } = await import("../src/hooks/spot/useSpotApprovals");
  const { PERPS_SPENDER } = await import("../src/core/network/approvals");
  const { PRECOMPILES } = await import("../src/core/network/precompiles");
  const { PerpsPrecompileABI, TimeInForce } = await import("../src/core/network/abi/PerpsPrecompile");
  const { SPOT_TOKENS } = await import("../src/core/spot/addresses");
  const { buildQuoteMarketBuy } = await import("../src/core/spot/quoterCalls");
  const { pecorQuoterAbi } = await import("../src/core/network/spot_abi/PECORQuoter");
  const { gatewayUrls } = await import("../src/core/network/gateway");
  const { gqlEndpoint, gqlQuery } = await import("../src/core/graphql/client");
  const { FUNDING_PAYMENTS_QUERY } = await import("../src/hooks/perps/useSubgraph");
  const { COLLATERAL_TOKENS, erc20Abi } = await import("../src/core/perps/config");

  const publicClient = clients.getPublicClient();
  const nodeChainId = await publicClient.getChainId();
  assert.equal(nodeChainId, paxeerChain.id, `LOCAL_NODE_RPC serves chain ${nodeChainId}, not ${paxeerChain.id}`);

  const signer = privateKeyToAccount(env.LOCAL_NODE_KEY as Hex);
  const walletClient = createWalletClient({
    account: signer,
    chain: paxeerChain,
    transport: http(env.LOCAL_NODE_RPC),
  });
  const localKey: CreateConnectorFn = createConnector(() => ({
    id: "localNodeKey",
    name: "Local node key",
    type: "localNodeKey",
    async connect() {
      return { accounts: [signer.address], chainId: paxeerChain.id } as never;
    },
    async disconnect() {},
    async getAccounts() {
      return [signer.address];
    },
    async getChainId() {
      return paxeerChain.id;
    },
    async getProvider() {
      return walletClient;
    },
    async getClient() {
      return walletClient as never;
    },
    async isAuthorized() {
      return true;
    },
    onAccountsChanged() {},
    onChainChanged() {},
    onDisconnect() {},
  }));
  await connect(wagmiConfig, { connector: localKey });
  assert.equal(clients.getConnectedAddress(), signer.address);

  return {
    env,
    contract,
    calls,
    writes,
    position: usePosition(),
    orders: usePerpsOrders(),
    quoter: useQuoter(),
    perpsAllowanceSource,
    account,
    SPOT_NATIVE_TOKENS,
    PERPS_SPENDER,
    PRECOMPILES,
    PerpsPrecompileABI,
    TimeInForce,
    SPOT_TOKENS,
    buildQuoteMarketBuy,
    pecorQuoterAbi,
    gatewayUrls,
    gqlEndpoint,
    gqlQuery,
    FUNDING_PAYMENTS_QUERY,
    USDL: COLLATERAL_TOKENS.usdl.address,
    erc20Abi,
    publicClient,
    owner: signer.address,
  };
}

type Harness = Awaited<ReturnType<typeof loadHarness>>;
let harnessPromise: Promise<Harness> | undefined;

function harness(): Promise<Harness> {
  harnessPromise ??= loadHarness(requireEnv());
  return harnessPromise;
}

const state: {
  startBalance?: bigint;
  startBlock?: bigint;
  positionId?: bigint;
  takeProfitPrice?: bigint;
  stopLossPrice?: bigint;
  receipts: TransactionReceipt[];
  gas: GasRecord[];
} = { receipts: [], gas: [] };

function required<T>(value: T | undefined, what: string): T {
  if (value === undefined) throw new Error(`${what} is missing: an earlier step of the round trip failed`);
  return value;
}

function isApprove(step: Step): step is ApproveCall {
  return step.functionName === "approve";
}

/** Send one step through writeContract, recording the fixed-gas estimate of a perpetuals write first. */
async function send(h: Harness, step: Step): Promise<TransactionReceipt> {
  if (!isApprove(step)) {
    const method = step.functionName as GasMethod;
    const surcharge = GAS_SURCHARGE[method];
    assert.ok(surcharge !== undefined, `no fixed gas row for ${step.functionName}`);
    const data = encodeFunctionData({ abi: step.abi, functionName: step.functionName, args: step.args } as never);
    const estimate = await h.publicClient.estimateGas({ account: h.owner, to: step.address, data });
    state.gas.push({ method, estimate, fixed: 2_000n + 30n * BigInt(size(data) - 4) + surcharge });
  }
  const hash = isApprove(step)
    ? await h.contract.writeContract({ ...step, abi: h.erc20Abi })
    : await h.contract.writeContract(step);
  const result = await h.contract.waitForTx(hash);
  assert.equal(result.status, "success");
  const receipt = await h.publicClient.getTransactionReceipt({ hash });
  assert.equal(receipt.status, "success", `${step.functionName} receipt status`);
  state.receipts.push(receipt);
  return receipt;
}

async function sendAll(h: Harness, plan: readonly Step[]): Promise<TransactionReceipt[]> {
  const receipts: TransactionReceipt[] = [];
  for (const step of plan) receipts.push(await send(h, step));
  return receipts;
}

function roundToTick(price: bigint, tick: bigint): bigint {
  return (price / tick) * tick;
}

/** 18-decimal USD to USDL base units: a credit rounds down, a debit rounds up. */
function usdToUsdl(usd: bigint): bigint {
  return usd >= 0n ? usd / USD_TO_USDL : -((-usd + USD_TO_USDL - 1n) / USD_TO_USDL);
}

async function latestBlockTime(h: Harness): Promise<bigint> {
  return (await h.publicClient.getBlock({ blockTag: "latest" })).timestamp;
}

function delay(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

describe("e2eLocalNode", () => {
  it("neverSkips", () => {
    requireEnv();
    const childEnv: NodeJS.ProcessEnv = { ...process.env };
    delete childEnv.LOCAL_NODE_RPC;
    delete childEnv.NODE_TEST_CONTEXT;
    const child = spawnSync(
      process.execPath,
      ["--test", "--test-reporter=tap", "--import", "tsx", "tests/e2e-local-node.test.ts"],
      { cwd: process.cwd(), env: childEnv, encoding: "utf8" },
    );
    const output = `${child.stdout}${child.stderr}`;
    assert.notEqual(child.status, 0, "the round trip without LOCAL_NODE_RPC must fail");
    assert.match(output, /LOCAL_NODE_RPC is not set/);
    assert.match(output, /^# skipped 0$/m);
    assert.match(output, /^# todo 0$/m);
  });

  it("pointersMatchSpotTokens", async () => {
    const h = await harness();
    const pax = h.SPOT_TOKENS.PAX.address;
    const sid = h.SPOT_TOKENS.SID.address;
    const weth = await h.publicClient.readContract({
      address: h.PRECOMPILES.spot.PECORQuoter,
      abi: h.pecorQuoterAbi,
      functionName: "WETH",
    });
    assert.equal(getAddress(weth), getAddress(pax));
    assert.equal(await h.publicClient.readContract({ address: pax, abi: tokenAbi, functionName: "decimals" }), 6);
    assert.equal(await h.publicClient.readContract({ address: sid, abi: tokenAbi, functionName: "decimals" }), 6);
    const quote = await h.publicClient.readContract(h.buildQuoteMarketBuy(h.USDL, pax, SPOT_QUOTE_AMOUNT));
    assert.ok(quote.amountOut > 0n, "a 25 USDL market buy of PAX quotes zero");
  });

  it("depositRoundTrip", async () => {
    const h = await harness();
    state.startBalance = await h.account.getWalletBalance(h.owner);
    state.startBlock = await h.publicClient.getBlockNumber();
    const plan = await h.calls.buildDepositPlan(
      DEPOSIT_AMOUNT,
      () => h.account.getAllowance(h.owner),
      h.SPOT_NATIVE_TOKENS,
    );
    assert.equal(plan.length, 2);
    const [approve, deposit] = plan;
    assert.ok(isApprove(approve), "the deposit plan starts with the exact approval");
    assert.equal(getAddress(approve.address), getAddress(h.USDL));
    assert.deepEqual(approve.args, [h.PERPS_SPENDER, DEPOSIT_AMOUNT]);
    assert.equal(getAddress(h.PERPS_SPENDER), getAddress("0x0000000000000000000000000000000000001015"));
    await send(h, approve);
    assert.equal(await h.account.getAllowance(h.owner), DEPOSIT_AMOUNT);
    await send(h, deposit);
    assert.equal((await h.account.getAccount(h.owner)).collateral, DEPOSIT_AMOUNT);
  });

  it("openWithTpSlAttaches", async () => {
    const h = await harness();
    required(state.startBalance, "the deposit");
    const config = await h.publicClient.readContract({
      address: h.PRECOMPILES.perps,
      abi: h.PerpsPrecompileABI,
      functionName: "getMarketConfig",
      args: [MARKET_ID],
    });
    const quote = await h.quoter.quoteOpenPosition({
      marketId: MARKET_ID,
      collateralToken: h.USDL,
      collateralAmount: OPEN_COLLATERAL,
      leverage: LEVERAGE,
      isLong: true,
    });
    const takeProfitPrice = roundToTick((quote.entryPrice * 110n) / 100n, config.tickSize);
    const stopLossPrice = roundToTick((quote.entryPrice * 95n) / 100n, config.tickSize);
    const plan = await h.writes.planOpen(
      {
        marketId: MARKET_ID,
        isLong: true,
        collateralAmount: OPEN_COLLATERAL,
        leverage: LEVERAGE,
        quote: { entryPrice: quote.entryPrice, sizeUsd: quote.sizeUsd },
        takeProfitPrice,
        stopLossPrice,
      },
      h.perpsAllowanceSource(),
    );
    const receipts = await sendAll(h, plan);
    const opened = parseEventLogs({
      abi: h.PerpsPrecompileABI,
      logs: receipts[receipts.length - 1].logs,
      eventName: "PositionOpened",
    });
    assert.equal(opened.length, 1);
    const positionId = opened[0].args.positionId;
    assert.equal(getAddress(opened[0].args.user), h.owner);
    assert.deepEqual(await h.position.getUserPositionIds(h.owner), [positionId]);
    const details = await h.publicClient.readContract(h.calls.buildGetPositionDetails(positionId));
    assert.equal(details.takeProfitPrice, takeProfitPrice);
    assert.equal(details.stopLossPrice, stopLossPrice);
    state.positionId = positionId;
    state.takeProfitPrice = takeProfitPrice;
    state.stopLossPrice = stopLossPrice;
  });

  it("positionManagementRoundTrip", async () => {
    const h = await harness();
    const positionId = required(state.positionId, "the open position");
    const config = await h.publicClient.readContract({
      address: h.PRECOMPILES.perps,
      abi: h.PerpsPrecompileABI,
      functionName: "getMarketConfig",
      args: [MARKET_ID],
    });
    const record = await h.position.getPositionRecord(positionId);
    const limit = h.calls.buildPlaceLimitOrder({
      marketId: MARKET_ID,
      isLong: true,
      limitPrice: roundToTick(record.entryPrice / 2n, config.tickSize),
      sizeUsd: 200n * E18,
      leverage: LEVERAGE,
      collateralAmount: LIMIT_COLLATERAL,
      timeInForce: h.TimeInForce.GTC,
      reduceOnly: false,
      cloid: h.calls.ladderCloid(await latestBlockTime(h), 0),
    });
    const placed = await sendAll(h, await h.writes.planOrder([limit], h.perpsAllowanceSource()));
    const orderLogs = parseEventLogs({
      abi: h.PerpsPrecompileABI,
      logs: placed[placed.length - 1].logs,
      eventName: "OrderPlaced",
    });
    assert.equal(orderLogs.length, 1);
    const orderId = orderLogs[0].args.orderId;
    assert.ok((await h.orders.getUserOrderIds(h.owner)).includes(orderId));
    await send(h, h.calls.buildCancelOrder(orderId));
    assert.ok(!(await h.orders.getUserOrderIds(h.owner)).includes(orderId));

    const allowanceBefore = await h.account.getAllowance(h.owner);
    const plan = await h.writes.planCollateral(
      { action: "add", positionId, amount: ADDED_COLLATERAL },
      h.perpsAllowanceSource(),
    );
    const approvals = plan.filter(isApprove);
    assert.ok(approvals.length <= 1);
    for (const approve of approvals) assert.deepEqual(approve.args, [h.PERPS_SPENDER, ADDED_COLLATERAL]);
    const approved = approvals.length === 1 ? ADDED_COLLATERAL : allowanceBefore;
    await sendAll(h, plan);
    assert.equal(await h.account.getAllowance(h.owner), approved - ADDED_COLLATERAL);
    const details = await h.publicClient.readContract(h.calls.buildGetPositionDetails(positionId));
    assert.equal(details.takeProfitPrice, required(state.takeProfitPrice, "the take-profit"));
    assert.equal(details.stopLossPrice, required(state.stopLossPrice, "the stop-loss"));
  });

  it("fundingVisibleThroughGateway", async () => {
    const h = await harness();
    const positionId = required(state.positionId, "the open position");
    const settlesAt = ((await latestBlockTime(h)) / FUNDING_INTERVAL_SECONDS + 1n) * FUNDING_INTERVAL_SECONDS;
    while ((await latestBlockTime(h)) <= settlesAt) await delay(POLL_INTERVAL_MS);
    const endpoint = h.gqlEndpoint(h.gatewayUrls().graphql.url);
    const deadline = Date.now() + FUNDING_INDEX_GRACE_MS;
    type FundingRow = { positionId: string; userAddress: string };
    let rows: FundingRow[] = [];
    while (rows.length === 0 && Date.now() < deadline) {
      const data = await h.gqlQuery<{ fundingPayments: FundingRow[] }>(endpoint, h.FUNDING_PAYMENTS_QUERY, {
        userAddress: h.owner.toLowerCase(),
        positionId: positionId.toString(),
      });
      rows = data.fundingPayments;
      if (rows.length === 0) await delay(POLL_INTERVAL_MS);
    }
    assert.ok(rows.length >= 1, `GetFundingPayments returned no row for position ${positionId}`);
    for (const row of rows) {
      assert.equal(row.positionId, positionId.toString());
      assert.equal(getAddress(row.userAddress), h.owner);
    }
  });

  it("closeRoundTrip", async () => {
    const h = await harness();
    const positionId = required(state.positionId, "the open position");
    const before = await h.position.getPositionRecord(positionId);
    const half = before.sizeUsd / 2n;
    const partial = await h.quoter.quotePartialClose(positionId, half);
    const halfPlan = h.writes.planPartialClose({
      positionId,
      isLong: before.isLong,
      closeSizeUsd: half,
      exitPrice: partial.exitPrice,
    });
    const halfReceipts = await sendAll(h, halfPlan);
    const halfClosed = parseEventLogs({
      abi: h.PerpsPrecompileABI,
      logs: halfReceipts[0].logs,
      eventName: "PositionClosed",
    });
    assert.equal(halfClosed.length, 1);
    assert.equal(halfClosed[0].args.isFullClose, false);

    const rest = await h.position.getPositionRecord(positionId);
    const { exitPrice } = await h.quoter.quoteClosePosition(positionId);
    const restReceipts = await sendAll(
      h,
      h.writes.planClose({ positionId, isLong: rest.isLong, sizeUsd: rest.sizeUsd, exitPrice }),
    );
    const restClosed = parseEventLogs({
      abi: h.PerpsPrecompileABI,
      logs: restReceipts[0].logs,
      eventName: "PositionClosed",
    });
    assert.equal(restClosed.length, 1);
    assert.equal(restClosed[0].args.isFullClose, true);
    assert.deepEqual(await h.position.getUserPositionIds(h.owner), []);
  });

  it("withdrawalBalances", async () => {
    const h = await harness();
    const startBalance = required(state.startBalance, "the starting balance");
    const startBlock = required(state.startBlock, "the starting block");
    required(state.positionId, "the closed position");
    const view = await h.account.getAccount(h.owner);
    assert.equal(view.withdrawable, view.collateral);
    await send(h, h.calls.buildWithdrawAll(view));
    assert.equal((await h.account.getAccount(h.owner)).collateral, 0n);
    const finalBalance = await h.account.getWalletBalance(h.owner);

    const transfers = parseEventLogs({
      abi: tokenAbi,
      eventName: "Transfer",
      logs: state.receipts.flatMap((r) => r.logs).filter((l) => getAddress(l.address) === getAddress(h.USDL)),
    });
    let moved = 0n;
    for (const t of transfers) {
      if (getAddress(t.args.from) === h.owner) moved -= t.args.value;
      if (getAddress(t.args.to) === h.owner) moved += t.args.value;
    }
    assert.equal(finalBalance, startBalance + moved);

    const range = {
      address: h.PRECOMPILES.perps,
      args: { user: h.owner },
      fromBlock: startBlock,
      toBlock: "latest",
    } as const;
    const abi = h.PerpsPrecompileABI;
    const fees = await h.publicClient.getContractEvents({ ...range, abi, eventName: "FeeCollected" });
    const closes = await h.publicClient.getContractEvents({ ...range, abi, eventName: "PositionClosed" });
    const funding = await h.publicClient.getContractEvents({ ...range, abi, eventName: "PositionFundingApplied" });
    let feeTokens = 0n;
    for (const fee of fees) {
      if (fee.args.feeType === FEE_TYPE_TRADING) feeTokens += required(fee.args.feeTokens, "FeeCollected.feeTokens");
      else if (fee.args.feeType === FEE_TYPE_MAKER_REBATE) {
        feeTokens -= required(fee.args.feeTokens, "FeeCollected.feeTokens");
      } else assert.fail(`FeeCollected carries feeType ${fee.args.feeType}`);
    }
    let realised = 0n;
    for (const close of closes) realised += usdToUsdl(required(close.args.realizedPnl, "PositionClosed.realizedPnl"));
    for (const f of funding) {
      realised += usdToUsdl(required(f.args.fundingPaymentUsd, "PositionFundingApplied.fundingPaymentUsd"));
    }
    assert.equal(finalBalance, startBalance - feeTokens + realised);
  });

  it("estimatesCoverFixedGas", () => {
    assert.ok(state.gas.length > 0, "no perpetuals write recorded its gas estimate");
    for (const method of Object.keys(GAS_SURCHARGE) as GasMethod[]) {
      assert.ok(
        state.gas.some((g) => g.method === method),
        `no ${method} write recorded its gas estimate`,
      );
    }
    for (const record of state.gas) {
      assert.ok(
        record.estimate >= record.fixed,
        `${record.method} estimate ${record.estimate} is below its fixed gas ${record.fixed}`,
      );
    }
  });
});
