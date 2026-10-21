import { describe, it } from "node:test";
import { strict as assert } from "node:assert";
import { existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import path from "node:path";

import { NATIVE_PAX_ADDRESS, SPOT_STABLECOIN_LIST, SPOT_TOKENS } from "../src/core/spot/addresses";
import { DEFAULT_SPOT_SYMBOL, SPOT_MARKETS, getSpotMarket } from "../src/core/spot/config";
import { parseTokenAmount } from "../src/core/spot/slippage";
import { selectStableForBuy, selectStableForSell } from "../src/core/spot/stableSelector";

const ROOT = path.resolve(__dirname, "..");
const USDL = "0x85fcd13735f4309833a503ee804ea32395851479";
const LEGACY_USDC = "0xf8850b62ae017c55be7f571bbad840b4f3da7d49";
const LEGACY_USDT = "0x5dfe06ae465a39c442c45ed273c523bac2d1f6a8";
const LEGACY_WPAX9 = "0xe5ccf339d1c89c7e6c6768b28507f78b861fc1de";
const WRAP_NAMES = ["WPAX9_ADDRESS", "computeWrapShortfall", "computeUnwrapAmount", "shouldWrapBeforeSell"];

function sourceFiles(dir: string): string[] {
  const out: string[] = [];
  for (const name of readdirSync(dir)) {
    const full = path.join(dir, name);
    if (statSync(full).isDirectory()) out.push(...sourceFiles(full));
    else if (/\.(ts|tsx)$/.test(name)) out.push(full);
  }
  return out;
}

describe("spot tokens and markets", () => {
  it("spotTokensAreUsdlPaxSid", () => {
    assert.deepEqual(Object.keys(SPOT_TOKENS), ["USDL", "PAX", "SID"]);
    assert.equal(SPOT_TOKENS.USDL.address.toLowerCase(), USDL);
    assert.equal(SPOT_TOKENS.USDL.decimals, 6);
    assert.equal(SPOT_TOKENS.PAX.decimals, 6);
    assert.equal(SPOT_TOKENS.SID.decimals, 6);
    assert.equal(SPOT_TOKENS.PAX.isNative, true);
    assert.equal("wrappedAddress" in SPOT_TOKENS.PAX, false);
    for (const token of [SPOT_TOKENS.PAX, SPOT_TOKENS.SID]) {
      assert.match(token.address, /^0x[0-9a-fA-F]{40}$/);
      assert.notEqual(token.address.toLowerCase(), USDL);
      assert.notEqual(token.address.toLowerCase(), LEGACY_WPAX9);
    }
    // The PAX entry is the uhpx pointer of contracts/address-table.md, not the native-value placeholder.
    assert.notEqual(SPOT_TOKENS.PAX.address.toLowerCase(), NATIVE_PAX_ADDRESS);
  });

  it("stableIsUsdlOnly", () => {
    assert.equal(SPOT_STABLECOIN_LIST.length, 1);
    assert.equal(SPOT_STABLECOIN_LIST[0], SPOT_TOKENS.USDL);

    const balances = { [USDL]: 5_000_000n, [LEGACY_USDC]: 900_000_000n, [LEGACY_USDT]: 700_000_000n };
    assert.deepEqual(selectStableForBuy({ amountUsd: 4_000_000n, balances }), {
      stable: SPOT_TOKENS.USDL,
      sufficient: true,
      available: 5_000_000n,
    });
    assert.deepEqual(selectStableForBuy({ amountUsd: 6_000_000n, balances }), {
      stable: SPOT_TOKENS.USDL,
      sufficient: false,
      available: 5_000_000n,
    });
    assert.deepEqual(selectStableForBuy({ amountUsd: 0n, balances: {} }), {
      stable: SPOT_TOKENS.USDL,
      sufficient: true,
      available: 0n,
    });

    const vaultLiquidity = { [USDL]: 2_000_000n, [LEGACY_USDC]: 900_000_000n, [LEGACY_USDT]: 700_000_000n };
    assert.deepEqual(selectStableForSell({ expectedUsd: 2_000_000n, vaultLiquidity }), {
      stable: SPOT_TOKENS.USDL,
      sufficient: true,
      available: 2_000_000n,
    });
    assert.deepEqual(selectStableForSell({ expectedUsd: 3_000_000n, vaultLiquidity }), {
      stable: SPOT_TOKENS.USDL,
      sufficient: false,
      available: 2_000_000n,
    });
  });

  it("marketsAreOpeningPairs", () => {
    assert.deepEqual(
      SPOT_MARKETS.map((m) => [m.symbol, m.pair, m.baseToken, m.quoteToken]),
      [
        ["PAX", "PAX/USDL", "PAX", "USDL"],
        ["SID", "SID/USDL", "SID", "USDL"],
      ],
    );
    assert.equal(DEFAULT_SPOT_SYMBOL, "SID");
    assert.equal(getSpotMarket(DEFAULT_SPOT_SYMBOL)?.baseTokenMeta, SPOT_TOKENS.SID);
    for (const symbol of ["BNB", "ETH", "WPAX9"]) assert.equal(getSpotMarket(symbol), undefined);
    for (const [market, token] of [
      [SPOT_MARKETS[0]!, SPOT_TOKENS.PAX],
      [SPOT_MARKETS[1]!, SPOT_TOKENS.SID],
    ] as const) {
      assert.equal(market.baseTokenMeta, token);
      assert.equal(market.baseAddress, token.address);
      assert.equal(market.baseDecimals, token.decimals);
    }
  });

  it("wrapModuleRemoved", () => {
    assert.equal(existsSync(path.join(ROOT, "src/core/spot/native.ts")), false);
    const hits: string[] = [];
    for (const file of sourceFiles(path.join(ROOT, "src"))) {
      const text = readFileSync(file, "utf8");
      for (const name of [...WRAP_NAMES, "core/spot/native", "wpaxBalance"]) {
        if (text.includes(name)) hits.push(`${path.relative(ROOT, file)}: ${name}`);
      }
    }
    assert.deepEqual(hits, []);

    const form = readFileSync(path.join(ROOT, "src/widgets/spot/SpotOrderForm.tsx"), "utf8");
    assert.match(form, /const pointer = await getPaxPointer\(\);/);
    assert.match(form, /address: pointer,\s*abi: erc20Abi,\s*functionName: "balanceOf",/);
    assert.match(form, /formatUnits\(paxPointerBalance, market\.baseDecimals\)/);
    assert.doesNotMatch(form, /native\.balance, 18\)/);
  });

  it("onePaxIsOneMillionBaseUnits", () => {
    const pax = getSpotMarket("PAX");
    assert.ok(pax);
    assert.equal(pax.baseDecimals, 6);
    assert.equal(parseTokenAmount("1", pax.baseDecimals), 1_000_000n);
    assert.equal(pax.baseAddress, SPOT_TOKENS.PAX.address);
  });
});
