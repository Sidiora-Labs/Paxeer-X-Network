/**
 * Sidiora Meta-AG — Canonical Test Constants
 * Derived from `docs/architecture/pecor-sidiora-aggregator-spec.md` (FROZEN 2026-04-24).
 * Any change here REQUIRES a spec-amendment PR. Do not hand-edit role hashes or source IDs
 * without cross-checking the spec sections listed beside each block.
 */

const { ethers } = require("hardhat");
const { keccak256, toUtf8Bytes, ZeroAddress } = ethers;

// --------------------------------------------------------------------------- //
// Access-control role identifiers (spec §7.1 – §7.12)                         //
// --------------------------------------------------------------------------- //
const PECOR_ROLES = Object.freeze({
  DEFAULT_ADMIN_ROLE: "0x0000000000000000000000000000000000000000000000000000000000000000",
  RELAYER_ROLE:        keccak256(toUtf8Bytes("RELAYER_ROLE")),
  OPERATOR_ROLE:       keccak256(toUtf8Bytes("OPERATOR_ROLE")),
  KEEPER_ROLE:         keccak256(toUtf8Bytes("KEEPER_ROLE")),
  FEE_COLLECTOR_ROLE:  keccak256(toUtf8Bytes("FEE_COLLECTOR_ROLE")),
  EMITTER_ROLE:        keccak256(toUtf8Bytes("EMITTER_ROLE")),
});

// --------------------------------------------------------------------------- //
// IDataFeedAdapter sourceIds (spec §7.3 – §7.4)                               //
// --------------------------------------------------------------------------- //
const SOURCE_IDS = Object.freeze({
  PAXEER_PRICE_ORACLE: keccak256(toUtf8Bytes("PaxeerPriceOracle.v1")),
  SIDIORA_AMM:         keccak256(toUtf8Bytes("SidioraAMM.v1")),
});

// --------------------------------------------------------------------------- //
// IProtocolAdapter adapterIds (spec §7.8 – §7.9)                              //
// --------------------------------------------------------------------------- //
const ADAPTER_IDS = Object.freeze({
  VAULT:   keccak256(toUtf8Bytes("PECORVault.v1")),
  SIDIORA: keccak256(toUtf8Bytes("SidioraAMM.v1")),
});

// --------------------------------------------------------------------------- //
// Numeric bounds (spec §7.1 – §7.10 and §8)                                   //
// --------------------------------------------------------------------------- //
const BPS = Object.freeze({
  DENOMINATOR:            10_000n,
  // PECOR engine (spec §7.6)
  MAX_FEE_BPS:            200n,
  MAX_IMPACT_BPS:         500n,
  // VaultAdapter (spec §7.8)
  VAULT_ADAPTER_MAX_FEE:  200n,
  // MetaAGRouter (spec §7.10)
  DEFAULT_ORACLE_DEVIATION: 500n, // 5%
});

const LIMITS = Object.freeze({
  MAX_ADAPTERS: 20,
  MAX_HOPS:     5,
  TIER1_THRESHOLD_USD: ethers.parseUnits("10000", 18),
  TIER2_THRESHOLD_USD: ethers.parseUnits("100000", 18),
});

// Confidence bands surfaced by adapters (spec §7.3 – §7.4)
const CONFIDENCE = Object.freeze({
  PRICE_ORACLE_FRESH:      9000n,
  PRICE_ORACLE_AGING:      6000n,
  PRICE_ORACLE_NEAR_STALE: 3000n,
  SIDIORA_HIGH:            7000n,
  SIDIORA_MEDIUM:          4000n,
  SIDIORA_LOW:             1500n,
});

// Adapter staleness (spec §7.4)
const STALENESS = Object.freeze({
  SIDIORA_SECONDS: 120,
});

// --------------------------------------------------------------------------- //
// Paxeer live inventory (deployments/legacy-pecor-deployment.json)            //
// Used only by forked-mainnet integration tests. Unit tests must NOT hit RPC. //
// --------------------------------------------------------------------------- //
const LIVE_ADDRESSES = Object.freeze({
  chainId: 125,
  tokens: Object.freeze({
    USDC:  "0xf8850b62AE017c55be7f571BBad840b4f3DA7D49",
    USDT:  "0x5dfE06Ae465a39c442c45ed273c523BaC2d1f6a8",
    USDL:  "0x7c69c84daAEe90B21eeCABDb8f0387897E9B7B37",
    USID:  "0x6C32c255EeBD6A72B56ee82454d7140020919652",
    WPAX9: "0xe5ccf339d1c89c7e6c6768b28507f78b861fc1de",
    SID:   "0x86949e4CdB89496490890B67C9cfF63eD8efB4b1",
  }),
  stablecoins: Object.freeze({
    // spec Q2 locks USDL as stablecoin=true in v2 vault registration
    USDC:  true,
    USDT:  true,
    USDL:  true,
    USID:  true,
    WPAX9: false,
    SID:   false,
  }),
  legacyPecor: Object.freeze({
    PriceOracle:        "0x921A37182339b1618CB55937448c66B6538BF225",
    TransactionTracker: "0xf656612D8F305E4867d8203176f5656bB69be958",
    PECORVault:         "0x6500B1B3F8067772041C68b2c51D8E7A84e20C31",
    PECOR:              "0xae894b953ec1dD9b305346dEc1484Fe0ffF5eaD4",
    PECOROrders:        "0x39DCa28a022fED90Bc7964E84330b3871D02692D",
    PECORQuoter:        "0x4e643931fbb2df1B5965739B46CF70BCe622BD0a",
    OracleHub:          "0xED7620DC28759d55D89fF802E307Dd246d61D409",
    PriceOracleAdapter: "0x02D04a000E09c47d6BCFc1D6afb43Cac5d62d1c9",
    SidioraFeedAdapter: "0x2531dFa65CB370771ca695cf9620140022ab360B",
    VaultAdapter:       "0x8C362D903ad5ce3E42b2E7a00686aE5E3aF0B0F6",
    SidioraAdapter:     "0x5882D31D5E5E22395863DAe0fe977B2C978C9f33",
    PECORRouter:        "0x5925FA311707C406D83FC76317a69bb1Ba263F32",
  }),
  sidioraLaunchpad: Object.freeze({
    PoolRegistry: "0x1F22f11325197fae71937598F6935cc4e9231970",
    Quoter:       "0xeDb3B45E320A8ab2306Fa1C303742f2478fd3E0a",
    Router:       "0xB2D63300FE8b3508A83728e8f36B98e845eBD980",
  }),
  governance: Object.freeze({
    Timelock: "0xEc2B7b640469607A45615385e713e656B7e667b9",
    timelockDelay: 172800,
  }),
});

// Convenience sentinel
const ZERO_ADDRESS = ZeroAddress;

module.exports = {
  PECOR_ROLES,
  SOURCE_IDS,
  ADAPTER_IDS,
  BPS,
  LIMITS,
  CONFIDENCE,
  STALENESS,
  LIVE_ADDRESSES,
  ZERO_ADDRESS,
};
