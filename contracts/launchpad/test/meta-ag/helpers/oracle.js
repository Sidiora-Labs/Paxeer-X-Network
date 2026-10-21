/**
 * Sidiora Meta-AG — Oracle test utilities.
 *
 * Light, implementation-agnostic helpers for PriceOracle / OracleHub tests.
 * Nothing in this file reaches the chain: callers pass the already-deployed
 * ethers v6 Contract handles produced by `helpers/fixtures.js`.
 *
 * Covered operations:
 *   - Push price (single / batch) with a relayer signer
 *   - Warp the EVM forward to simulate staleness / TWAP windows
 *   - Build a deterministic TWAP price series for accumulator tests
 *   - Generate canonical FeedPrice tuples matching IDataFeedAdapter.FeedPrice
 */

const { ethers, network } = require("hardhat");
const { CONFIDENCE, SOURCE_IDS, STALENESS } = require("./constants");

// --------------------------------------------------------------------------- //
// Time controls                                                               //
// --------------------------------------------------------------------------- //
async function increaseTime(seconds) {
  await network.provider.send("evm_increaseTime", [Number(seconds)]);
  await network.provider.send("evm_mine");
}
async function setNextBlockTimestamp(ts) {
  await network.provider.send("evm_setNextBlockTimestamp", [Number(ts)]);
  await network.provider.send("evm_mine");
}
async function latestTimestamp() {
  const block = await ethers.provider.getBlock("latest");
  return Number(block.timestamp);
}

// --------------------------------------------------------------------------- //
// PriceOracle push helpers (Phase 2.1)                                        //
// --------------------------------------------------------------------------- //
/**
 * Push a single token price through a relayer signer.
 * Expects the relayer to already hold RELAYER_ROLE on `priceOracle`.
 */
async function pushPrice({ priceOracle, relayer, token, price }) {
  return priceOracle.connect(relayer).updatePrice(token, price);
}

/**
 * Batch-push `tokens` ↔ `prices` via `batchUpdatePrices`. Arrays must match.
 */
async function batchPushPrices({ priceOracle, relayer, tokens, prices }) {
  if (tokens.length !== prices.length) {
    throw new Error(
      `[meta-ag/oracle] batchPushPrices length mismatch: tokens=${tokens.length} prices=${prices.length}`
    );
  }
  return priceOracle.connect(relayer).batchUpdatePrices(tokens, prices);
}

// --------------------------------------------------------------------------- //
// FeedPrice builders (IDataFeedAdapter.FeedPrice – spec §6.2)                  //
// --------------------------------------------------------------------------- //
/**
 * Build a FeedPrice struct usable by MockFeedAdapter.
 *
 * Default tuple order: (price, timestamp, confidence, sourceId, available)
 * Callers may override any field; `available` defaults to `true`.
 */
function buildFeedPrice({
  price,
  timestamp,
  confidence = CONFIDENCE.PRICE_ORACLE_FRESH,
  sourceId = SOURCE_IDS.PAXEER_PRICE_ORACLE,
  available = true,
} = {}) {
  if (price === undefined) {
    throw new Error("[meta-ag/oracle] buildFeedPrice requires `price`");
  }
  return {
    price: BigInt(price),
    timestamp: BigInt(timestamp ?? Math.floor(Date.now() / 1000)),
    confidence: BigInt(confidence),
    sourceId,
    available,
  };
}

// --------------------------------------------------------------------------- //
// TWAP simulators                                                             //
// --------------------------------------------------------------------------- //
/**
 * Produce a deterministic price/time series around `basePrice` following
 * a sinusoidal path. Used by PriceOracle TWAP accumulator tests so every
 * invocation yields the same expected moving average.
 */
function buildTwapSeries({ basePrice, samples = 8, periodSec = 60, amplitudeBps = 100 }) {
  const base = BigInt(basePrice);
  const denom = 10_000n;
  return Array.from({ length: samples }, (_, i) => {
    const angle = (2 * Math.PI * i) / samples;
    const amp = (base * BigInt(amplitudeBps)) / denom;
    const offset = BigInt(Math.round(Math.sin(angle) * Number(amp)));
    return {
      tSec: i * Number(periodSec),
      price: base + offset,
    };
  });
}

/**
 * Drive a price oracle through `buildTwapSeries` ticks, advancing EVM time
 * between each update. Returns the timestamps at which each sample was
 * pushed so tests can assert on the TWAP accumulator state.
 */
async function driveTwapSeries({ priceOracle, relayer, token, series }) {
  const pushedAt = [];
  for (const sample of series) {
    if (sample.tSec > 0) {
      await increaseTime(sample.tSec);
    }
    const tx = await pushPrice({ priceOracle, relayer, token, price: sample.price });
    await tx.wait();
    pushedAt.push(await latestTimestamp());
  }
  return pushedAt;
}

module.exports = {
  increaseTime,
  setNextBlockTimestamp,
  latestTimestamp,
  pushPrice,
  batchPushPrices,
  buildFeedPrice,
  buildTwapSeries,
  driveTwapSeries,
  CONFIDENCE,
  STALENESS,
  SOURCE_IDS,
};
