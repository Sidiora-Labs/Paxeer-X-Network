// The ABI barrels hold only the precompile ABIs and the token ABI (contracts/application.md#abi-modules rule 3).

import { describe, it } from "node:test";
import { strict as assert } from "node:assert";
import { readFileSync, readdirSync } from "node:fs";
import { createRequire } from "node:module";
import path from "node:path";

import { PerpsPrecompileABI, OrderType, TimeInForce } from "../src/core/network/abi/PerpsPrecompile";
import { IERC20ABI } from "../src/core/network/abi/IERC20";

const ROOT = path.resolve(__dirname, "..");
const SRC = path.join(ROOT, "src");
const ABI_DIR = path.join(SRC, "core/network/abi");

const BARREL_EXPORTS = ["IERC20ABI", "OrderType", "PerpsPrecompileABI", "TimeInForce"];

const ABI_FILES = ["IERC20.js", "IERC20.json", "IERC20.ts", "PerpsPrecompile.ts", "index.js", "index.ts"];

const REMOVED_ABIS = [
  "PositionFacetABI",
  "OrderBookFacetABI",
  "QuoterFacetABI",
  "TradingAccountABI",
  "VaultFactoryFacetABI",
  "OracleFacetABI",
];

const SOURCE_EXTENSIONS = new Set([".ts", ".tsx", ".js", ".jsx", ".cjs", ".mjs"]);

function sourceFiles(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) return sourceFiles(full);
    return SOURCE_EXTENSIONS.has(path.extname(entry.name)) ? [full] : [];
  });
}

describe("ABI barrel", () => {
  it("barrelHoldsPrecompileAbisOnly", () => {
    const load = createRequire(__filename);

    const barrel = load(path.join(ABI_DIR, "index.ts")) as Record<string, unknown>;
    assert.deepEqual(Object.keys(barrel).sort(), BARREL_EXPORTS);
    assert.equal(barrel.PerpsPrecompileABI, PerpsPrecompileABI);
    assert.equal(barrel.OrderType, OrderType);
    assert.equal(barrel.TimeInForce, TimeInForce);
    assert.equal(barrel.IERC20ABI, IERC20ABI);

    const cjs = load(path.join(ABI_DIR, "index.js")) as Record<string, unknown>;
    assert.deepEqual(Object.keys(cjs).sort(), BARREL_EXPORTS);
    assert.deepEqual(cjs.PerpsPrecompileABI, PerpsPrecompileABI);
    assert.deepEqual(cjs.OrderType, OrderType);
    assert.deepEqual(cjs.TimeInForce, TimeInForce);
    assert.deepEqual(cjs.IERC20ABI, IERC20ABI);

    assert.deepEqual(readdirSync(ABI_DIR).sort(), ABI_FILES);

    const facetPath = /core\/network\/abi\/[A-Za-z0-9]*(Facet|TradingAccount)\b/;
    const removedName = new RegExp(`\\b(${REMOVED_ABIS.join("|")})\\b`);
    const offenders = sourceFiles(SRC).flatMap((file) =>
      readFileSync(file, "utf8")
        .split("\n")
        .flatMap((line, i) =>
          facetPath.test(line) || removedName.test(line)
            ? [`${path.relative(ROOT, file)}:${i + 1}: ${line.trim()}`]
            : [],
        ),
    );
    assert.deepEqual(offenders, []);
  });
});
