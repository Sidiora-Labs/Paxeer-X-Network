// Precompile custom errors classified into application codes (contracts/application.md#errors).

import { describe, it } from "node:test";
import { strict as assert } from "node:assert";
import {
  type Abi,
  type AbiParameter,
  ContractFunctionExecutionError,
  ContractFunctionRevertedError,
  type Hex,
  encodeErrorResult,
  toFunctionSelector,
  zeroAddress,
} from "viem";

import { classifyError, PRECOMPILE_ERROR_CODES } from "../src/core/errors/classify";
import { ERROR_CATALOG } from "../src/core/errors/catalog";
import { PerpsPrecompileABI } from "../src/core/network/abi/PerpsPrecompile";
import { pecorAbi } from "../src/core/network/spot_abi/PECOR";
import { pecorOrdersAbi } from "../src/core/network/spot_abi/PECOROrders";
import { pecorStopOrdersAbi } from "../src/core/network/spot_abi/PECORStopOrders";
import { pecorQuoterAbi } from "../src/core/network/spot_abi/PECORQuoter";
import { pecorVaultAbi } from "../src/core/network/spot_abi/PECORVault";

type AbiError = Extract<Abi[number], { type: "error" }>;

const ABIS: Record<string, Abi> = {
  PerpsPrecompileABI,
  pecorAbi,
  pecorOrdersAbi,
  pecorStopOrdersAbi,
  pecorQuoterAbi,
  pecorVaultAbi,
};

const DEADLINE_MESSAGE = "The transaction deadline passed before it was executed.";
const STALE_PRICE_MESSAGE = "The price feed for this market is stale, so the contract refused the trade.";
const INPUT_MESSAGE = "The contract rejected the token or amount given for this transaction.";

/** The error viem's writeContract/simulateContract throw for a reverted call. */
function reverted(abi: Abi, data: Hex, functionName: string): ContractFunctionExecutionError {
  return new ContractFunctionExecutionError(new ContractFunctionRevertedError({ abi, data, functionName }), {
    abi,
    functionName,
    args: [],
    contractAddress: "0x0000000000000000000000000000000000001021",
  });
}

function argFor(param: AbiParameter): unknown {
  if (param.type === "address") return zeroAddress;
  if (param.type === "uint256") return 1n;
  throw new Error(`no test argument for ${param.type}`);
}

function encodeError(abi: Abi, item: AbiError): Hex {
  return encodeErrorResult({ abi: [item], errorName: item.name, args: item.inputs.map(argFor) });
}

function classify(abi: Abi, errorName: string, functionName = "marketBuy") {
  const item = abi.find((i): i is AbiError => i.type === "error" && i.name === errorName);
  assert.ok(item, `${errorName} is not in the ABI`);
  return classifyError(reverted(abi, encodeError(abi, item), functionName), "trade/test");
}

describe("classifyError over precompile custom errors", () => {
  it("insufficientOutputIsSlippage", () => {
    const data = encodeErrorResult({ abi: pecorAbi, errorName: "InsufficientOutput" });
    assert.equal(data, "0xbb2875c3");
    const err = classifyError(reverted(pecorAbi, data, "marketBuy"), "spot/market-buy");
    assert.equal(err.code, "TRADE_SLIPPAGE_EXCEEDED");
    assert.equal(err.title, ERROR_CATALOG.TRADE_SLIPPAGE_EXCEEDED.title);
    assert.equal(err.message, ERROR_CATALOG.TRADE_SLIPPAGE_EXCEEDED.message);
    assert.equal(err.context, "spot/market-buy");
    assert.deepEqual(err.fingerprint, ["TRADE_SLIPPAGE_EXCEEDED", "spot/market-buy", "InsufficientOutput"]);
  });

  it("spotOrderErrorsMapped", () => {
    assert.equal(classify(pecorOrdersAbi, "PriceAboveTarget", "executeLimitOrder").code, "TRADE_SLIPPAGE_EXCEEDED");
    assert.equal(classify(pecorOrdersAbi, "PriceBelowTarget", "executeLimitOrder").code, "TRADE_SLIPPAGE_EXCEEDED");
    assert.equal(classify(pecorOrdersAbi, "InsufficientOutput", "executeLimitOrder").code, "TRADE_SLIPPAGE_EXCEEDED");

    const liquidity = classify(pecorOrdersAbi, "InsufficientLiquidity", "executeLimitOrder");
    assert.equal(liquidity.code, "TRADE_INSUFFICIENT_LIQUIDITY");
    assert.equal(liquidity.message, ERROR_CATALOG.TRADE_INSUFFICIENT_LIQUIDITY.message);

    const expired = classify(pecorAbi, "Expired");
    assert.equal(expired.code, "RPC_REVERT");
    assert.equal(expired.message, DEADLINE_MESSAGE);

    const stale = classify(pecorAbi, "StalePrice");
    assert.equal(stale.code, "RPC_REVERT");
    assert.equal(stale.message, STALE_PRICE_MESSAGE);

    for (const name of ["SameToken", "ZeroAmount", "UnsupportedTransfer", "UnsupportedInputTransfer"]) {
      const input = classify(pecorAbi, name);
      assert.equal(input.code, "RPC_REVERT", name);
      assert.equal(input.message, INPUT_MESSAGE, name);
    }

    const native = classify(pecorAbi, "NativeTransferFailed");
    assert.equal(native.code, "RPC_REVERT");
    assert.equal(native.message, "The contract rejected this transaction with NativeTransferFailed.");

    const multicall = classify(pecorAbi, "MulticallFailed", "multicall");
    assert.equal(multicall.code, "RPC_REVERT");
    assert.equal(multicall.message, "The contract rejected this transaction with MulticallFailed.");
  });

  it("stopOrderErrorsMapped", () => {
    const above = classify(pecorStopOrdersAbi, "LimitMustBeAboveStopBuy", "placeStopLimitBuy");
    assert.equal(above.code, "TRADE_TP_SL_INVALID");
    assert.equal(above.message, ERROR_CATALOG.TRADE_TP_SL_INVALID.message);
    assert.equal(
      encodeErrorResult({ abi: pecorStopOrdersAbi, errorName: "LimitMustBeAboveStopBuy" }),
      "0x896a5f19",
    );

    const below = classify(pecorStopOrdersAbi, "LimitMustBeBelowStopSell", "placeStopLimitSell");
    assert.equal(below.code, "TRADE_TP_SL_INVALID");
    assert.equal(
      encodeErrorResult({ abi: pecorStopOrdersAbi, errorName: "LimitMustBeBelowStopSell" }),
      "0xba470259",
    );
  });

  it("unknownRevertKeepsRpcRevert", () => {
    const unknown = classifyError(reverted(pecorAbi, "0x12345678", "marketBuy"), "spot/market-buy");
    assert.equal(unknown.code, "RPC_REVERT");
    assert.equal(unknown.message, ERROR_CATALOG.RPC_REVERT.message);
    assert.deepEqual(unknown.fingerprint, ["RPC_REVERT", "spot/market-buy", "ContractFunctionExecutionError"]);

    const margin = classify(PerpsPrecompileABI, "PerpsInsufficientMargin", "placeOrder");
    assert.equal(margin.code, "TRADE_POSITION_LIQUIDATABLE");
    assert.equal(classify(PerpsPrecompileABI, "PerpsSlippageExceeded", "placeOrder").code, "TRADE_SLIPPAGE_EXCEEDED");
    assert.equal(classify(PerpsPrecompileABI, "PerpsDeadlineExpired", "placeOrder").code, "TRADE_SLIPPAGE_EXCEEDED");
    assert.equal(classify(PerpsPrecompileABI, "PerpsInvalidTriggerPrice", "placeTakeProfitOrder").code, "TRADE_TP_SL_INVALID");

    const paused = classify(PerpsPrecompileABI, "PerpsPaused", "placeOrder");
    assert.equal(paused.code, "RPC_REVERT");
    assert.equal(paused.message, "The contract rejected this transaction with PerpsPaused.");

    // A JSON-RPC eth_call error object carrying the revert data directly.
    const rpc = classifyError({
      code: 3,
      message: "execution reverted",
      data: encodeErrorResult({ abi: PerpsPrecompileABI, errorName: "PerpsPriceStale" }),
    });
    assert.equal(rpc.code, "RPC_REVERT");
    assert.equal(rpc.message, STALE_PRICE_MESSAGE);

    // An Error(string) revert is not a precompile error: it keeps the RPC_REVERT path.
    const errorString = [
      { type: "error", name: "Error", inputs: [{ name: "message", type: "string" }] },
    ] as const satisfies Abi;
    const reason = classifyError(
      reverted(pecorAbi, encodeErrorResult({ abi: errorString, errorName: "Error", args: ["paused"] }), "marketBuy"),
    );
    assert.equal(reason.code, "RPC_REVERT");
    assert.equal(reason.message, ERROR_CATALOG.RPC_REVERT.message);
  });

  it("everyErrorHasACode", () => {
    const abiErrorNames = new Set<string>();
    let perpsErrors = 0;
    for (const [abiName, abi] of Object.entries(ABIS)) {
      for (const item of abi) {
        if (item.type !== "error") continue;
        if (abiName === "PerpsPrecompileABI") perpsErrors++;
        abiErrorNames.add(item.name);
        assert.ok(Object.hasOwn(PRECOMPILE_ERROR_CODES, item.name), `${abiName}.${item.name} has no entry`);
        const expected = PRECOMPILE_ERROR_CODES[item.name as keyof typeof PRECOMPILE_ERROR_CODES];
        const data = encodeError(abi, item);
        assert.equal(data.slice(0, 10), toFunctionSelector(`function ${item.name}(${item.inputs.map((p) => p.type).join(",")})`));
        const err = classifyError(reverted(abi, data, "call"));
        assert.notEqual(err.code, "UNKNOWN", `${abiName}.${item.name}`);
        assert.equal(err.code, expected.code, `${abiName}.${item.name}`);
        assert.equal(
          err.message,
          expected.message ??
            (expected.code === "RPC_REVERT"
              ? `The contract rejected this transaction with ${item.name}.`
              : ERROR_CATALOG[expected.code].message),
          `${abiName}.${item.name}`,
        );
      }
    }
    assert.equal(perpsErrors, 44);
    assert.deepEqual(Object.keys(PRECOMPILE_ERROR_CODES).sort(), [...abiErrorNames].sort());
  });
});
