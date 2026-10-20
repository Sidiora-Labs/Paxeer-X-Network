// Golden selectors of the spot precompile set (0x1021 to 0x1025), pinned to contracts/spot.md.

import { describe, it } from "node:test";
import { strict as assert } from "node:assert";
import { existsSync } from "node:fs";
import { join } from "node:path";
import { toFunctionSelector, toFunctionSignature, type Abi } from "viem";

import * as spotAbis from "../src/core/network/spot_abi/index";
import { pecorAbi } from "../src/core/network/spot_abi/PECOR";
import { pecorOrdersAbi } from "../src/core/network/spot_abi/PECOROrders";
import { pecorStopOrdersAbi } from "../src/core/network/spot_abi/PECORStopOrders";
import { pecorQuoterAbi } from "../src/core/network/spot_abi/PECORQuoter";
import { pecorVaultAbi } from "../src/core/network/spot_abi/PECORVault";

type Rail = {
  abi: Abi;
  methods: Record<string, string>;
  errors: Record<string, string>;
};

const ORDER_RAIL_ERRORS: Record<string, string> = {
  "CannotCancel()": "0xfbc03578",
  "EnforcedPause()": "0xd93c0665",
  "ExpectedPause()": "0x8dfc202b",
  "FeeExceedsMax()": "0x5ff85e3f",
  "InsufficientLiquidity()": "0xbb55fd27",
  "InsufficientOutput()": "0xbb2875c3",
  "InvalidExpiry()": "0xd36c8500",
  "InvalidOracle()": "0x9589a27d",
  "InvalidPrice()": "0x00bfc921",
  "InvalidTWAPPeriod()": "0x8bd931d1",
  "InvalidVault()": "0xd03a6320",
  "NoFeesToCollect()": "0x6a4ea9e4",
  "NotAStablecoin()": "0x777a9be8",
  "NotAuthorized()": "0xea8e4eb5",
  "NotFeeCollector()": "0x73fcd3fe",
  "NotOrderOwner()": "0xf6412b5a",
  "OrderBelowMinimumNotional()": "0x9b2bea5b",
  "OrderExpired()": "0xc56873ba",
  "OrderNotPending()": "0xba254946",
  "OwnableInvalidOwner(address)": "0x1e4fbdf7",
  "OwnableUnauthorizedAccount(address)": "0x118cdaa7",
  "ReentrancyGuardReentrantCall()": "0x3ee5aeb5",
  "SafeERC20FailedOperation(address)": "0x5274afe7",
  "StalePrice()": "0x19abf40e",
  "TokenIsStablecoin()": "0xc1343a37",
  "TokenPaused()": "0xe7792495",
  "TooManyActiveOrders()": "0x28d7a641",
  "UnsupportedInputTransfer()": "0x1c42e5ce",
  "UnsupportedTransfer()": "0xce2d28e1",
  "ZeroAmount()": "0x1f2a2005",
};

const RAILS: Record<string, Rail> = {
  pecorAbi: {
    abi: pecorAbi,
    methods: {
      "marketBuy(address,address,uint256,uint256,uint256)": "0x77a61952",
      "marketSell(address,address,uint256,uint256,uint256)": "0x57f24201",
      "multicall(bytes[])": "0xac9650d8",
    },
    errors: {
      "EnforcedPause()": "0xd93c0665",
      "ExcessiveInput()": "0xbd750b82",
      "ExpectedPause()": "0x8dfc202b",
      "Expired()": "0x203d82d8",
      "InsufficientLiquidity()": "0xbb55fd27",
      "InsufficientOutput()": "0xbb2875c3",
      "InvalidVault()": "0xd03a6320",
      "MulticallFailed(uint256)": "0x750ebb7d",
      "NativeTransferFailed()": "0xf4b3b1bc",
      "NoFeesToCollect()": "0x6a4ea9e4",
      "NotAStablecoin()": "0x777a9be8",
      "OwnableInvalidOwner(address)": "0x1e4fbdf7",
      "OwnableUnauthorizedAccount(address)": "0x118cdaa7",
      "ReentrancyGuardReentrantCall()": "0x3ee5aeb5",
      "SafeERC20FailedOperation(address)": "0x5274afe7",
      "SameToken()": "0x201b580a",
      "StalePrice()": "0x19abf40e",
      "TokenIsStablecoin()": "0xc1343a37",
      "TokenPaused()": "0xe7792495",
      "UnsupportedInputTransfer()": "0x1c42e5ce",
      "UnsupportedTransfer()": "0xce2d28e1",
      "UseWethDeposit()": "0xb1da5593",
      "UseWethWithdraw()": "0x74ee4e2a",
      "ZeroAmount()": "0x1f2a2005",
    },
  },
  pecorOrdersAbi: {
    abi: pecorOrdersAbi,
    methods: {
      "placeLimitBuy(address,address,uint256,uint256,uint256)": "0x947de5b1",
      "placeLimitSell(address,address,uint256,uint256,uint256)": "0x31880832",
      "cancelLimitOrder(uint256)": "0xa5cdc8fc",
      "modifyLimitOrder(uint256,uint256,uint256)": "0x3f08abf2",
      "getUserLimitOrders(address)": "0xdd5d02e6",
      "getLimitOrder(uint256)": "0x53061026",
    },
    errors: {
      ...ORDER_RAIL_ERRORS,
      "PriceAboveTarget()": "0xbcfcc626",
      "PriceBelowTarget()": "0xb8673d0c",
      "SameToken()": "0x201b580a",
    },
  },
  pecorStopOrdersAbi: {
    abi: pecorStopOrdersAbi,
    methods: {
      "placeStopLimitBuy(address,address,uint256,uint256,uint256,uint256)": "0xafbf4a9b",
      "placeStopLimitSell(address,address,uint256,uint256,uint256,uint256)": "0x5b377378",
      "cancelStopLimitOrder(uint256)": "0x1f407810",
      "placeStopLoss(address,address,uint256,uint256,uint256)": "0x695970ef",
      "cancelStopLossOrder(uint256)": "0xc62bf4dc",
      "getUserStopLimitOrders(address)": "0xe93c4673",
      "getUserStopLossOrders(address)": "0x21297b41",
      "getStopLimitOrder(uint256)": "0x307ff324",
      "getStopLossOrder(uint256)": "0x4c34cf02",
    },
    errors: {
      ...ORDER_RAIL_ERRORS,
      "InvalidPrices()": "0x2084e5f6",
      "LimitMustBeAboveStopBuy()": "0x896a5f19",
      "LimitMustBeBelowStopSell()": "0xba470259",
      "NotActivated()": "0x037c597f",
      "NotPending()": "0x7dc6505a",
      "PriceAboveLimit()": "0x6d27d70f",
      "PriceAboveTrigger()": "0x7df2e7d4",
      "PriceBelowLimit()": "0xb4d66e0f",
    },
  },
  pecorQuoterAbi: {
    abi: pecorQuoterAbi,
    methods: {
      "quoteMarketBuy(address,address,uint256)": "0x6f6ac6a2",
      "quoteMarketSell(address,address,uint256)": "0x5323412e",
      "quoteExactIn(address,address,uint256)": "0x12be4fe0",
      "quoteExactOut(address,address,uint256)": "0x7020df95",
      "batchQuote((address,address,uint256,bool)[])": "0x3425eb94",
      "getTokenPrice(address)": "0xd02641a0",
      "getLiquidityInfo(address)": "0x7c0f8456",
      "getAllLiquidityInfo()": "0xe9505c98",
      "getTWAP(address,uint256)": "0x6f8c10a8",
    },
    errors: {
      "OwnableUnauthorizedAccount(address)": "0x118cdaa7",
      "StalePrice()": "0x19abf40e",
      "TwapWindowInvalid()": "0xb307c2d2",
    },
  },
  pecorVaultAbi: {
    abi: pecorVaultAbi,
    methods: {
      "deposit(address,uint256)": "0x47e7ef24",
      "depositNative()": "0xdb6b5246",
      "withdraw(address,uint256)": "0xf3fef3a3",
      "withdrawNative(uint256,address)": "0xb8ca8dd8",
      "shareTokens(address)": "0x96153f07",
    },
    errors: {
      "InsufficientLiquidity()": "0xbb55fd27",
      "InvalidArrayLength()": "0x9d89020a",
      "OwnableInvalidOwner(address)": "0x1e4fbdf7",
      "OwnableUnauthorizedAccount(address)": "0x118cdaa7",
      "ReentrancyGuardReentrantCall()": "0x3ee5aeb5",
      "SafeERC20FailedOperation(address)": "0x5274afe7",
      "TokenNotRegistered()": "0x259ba1ad",
      "TokenPaused()": "0xe7792495",
      "UnsupportedInputTransfer()": "0x1c42e5ce",
      "UnsupportedTransfer()": "0xce2d28e1",
      "WethOnly()": "0xcfab6517",
      "ZeroAmount()": "0x1f2a2005",
    },
  },
};

const DELETED_FILES = [
  "PECORRouter",
  "PECORVaultShare",
  "VaultAdapter",
  "OracleHub",
  "PriceOracle",
  "PriceOracleAdapter",
  "SidioraFeedAdapter",
  "SidioraAdapter",
  "TransactionTracker",
];

const DELETED_EXPORTS = [
  "pecorRouterAbi",
  "pecorVaultShareAbi",
  "vaultAdapterAbi",
  "oracleHubAbi",
  "priceOracleAbi",
  "priceOracleAdapterAbi",
  "sidioraFeedAdapterAbi",
  "sidioraAdapterAbi",
  "transactionTrackerAbi",
];

function functionSelectors(abi: Abi): Map<string, string> {
  const out = new Map<string, string>();
  for (const item of abi) {
    if (item.type === "function") out.set(toFunctionSignature(item), toFunctionSelector(item));
  }
  return out;
}

function errorSelectors(abi: Abi): Record<string, string> {
  const out: Record<string, string> = {};
  for (const item of abi) {
    if (item.type !== "error") continue;
    const signature = `${item.name}(${item.inputs.map((input) => input.type).join(",")})`;
    out[signature] = toFunctionSelector(signature);
  }
  return out;
}

describe("selectorsMatchSpotContract", () => {
  it("computes the marketBuy and InsufficientOutput selectors of PECOR as contracts/spot.md lists them", () => {
    assert.equal(toFunctionSelector("marketBuy(address,address,uint256,uint256,uint256)"), "0x77a61952");
    assert.equal(functionSelectors(pecorAbi).get("marketBuy(address,address,uint256,uint256,uint256)"), "0x77a61952");
    assert.equal(errorSelectors(pecorAbi)["InsufficientOutput()"], "0xbb2875c3");
  });

  for (const [name, rail] of Object.entries(RAILS)) {
    it(`${name} carries every application-called method at its contract selector`, () => {
      const actual = functionSelectors(rail.abi);
      for (const [signature, selector] of Object.entries(rail.methods)) {
        assert.equal(toFunctionSelector(signature), selector, signature);
        assert.equal(actual.get(signature), selector, signature);
      }
    });

    it(`${name} carries exactly the errors contracts/spot.md lists for its rail`, () => {
      const actual = errorSelectors(rail.abi);
      for (const [signature, selector] of Object.entries(rail.errors)) {
        assert.equal(toFunctionSelector(signature), selector, signature);
      }
      assert.deepEqual(actual, rail.errors);
    });
  }

  it("serves multicall as nonpayable", () => {
    const multicall = pecorAbi.find((item) => item.type === "function" && item.name === "multicall");
    assert.ok(multicall && multicall.type === "function");
    assert.equal(multicall.stateMutability, "nonpayable");
  });

  it("exports only the five served spot ABIs and none without a precompile", () => {
    assert.deepEqual(Object.keys(spotAbis).sort(), [
      "pecorAbi",
      "pecorOrdersAbi",
      "pecorQuoterAbi",
      "pecorStopOrdersAbi",
      "pecorVaultAbi",
    ]);
    for (const name of DELETED_EXPORTS) {
      assert.equal(Object.prototype.hasOwnProperty.call(spotAbis, name), false, name);
    }
    const dir = join(__dirname, "..", "src", "core", "network", "spot_abi");
    for (const file of DELETED_FILES) {
      assert.equal(existsSync(join(dir, `${file}.ts`)), false, file);
    }
  });
});
