const { ethers } = require("hardhat");

// Token decimals (both USDL and SidioraERC20 use 6 decimals)
const TOKEN_DECIMALS = 6n;
function p6(n) { return ethers.parseUnits(String(n), 6); }

// Virtual reserve defaults
const VIRTUAL_USDL_DEFAULT = p6("10000"); // 10,000 USDL (6 dec)
const VIRTUAL_TOKEN_DEFAULT = p6("1000000000"); // 1B tokens (6 dec)

// Fee bounds (basis points)
const MIN_FEE_BPS = 10n; // 0.10%
const MAX_FEE_BPS = 300n; // 3.00%
const BASE_FEE_BPS = 30n; // 0.30%
const PROTOCOL_FEE_BPS = 1000n; // 10% of pool fees go to protocol

// Fee weights
const FEE_DECAY_RATE = 500n; // age factor decay
const VOLATILITY_WEIGHT = 100n;
const CONCENTRATION_WEIGHT = 100n;

// Creation fee
const CREATION_FEE = p6("1"); // 1 USDL (6 dec)

// Token defaults
const TOKEN_TOTAL_SUPPLY = p6("1000000000"); // 1B tokens (6 dec)

// Addresses
const DEAD_ADDRESS = "0x000000000000000000000000000000000000dEaD";
const ZERO_ADDRESS = ethers.ZeroAddress;

// Fee strategies
const FeeStrategy = {
  CLAIM: 0n,
  BURN: 1n,
  AIRDROP: 2n,
  LP_REWARDS: 3n,
};

// Optical hook flags (bitmask)
const HookFlags = {
  BEFORE_SWAP: 1n,
  AFTER_SWAP: 2n,
  BEFORE_FEE_DISTRIBUTION: 4n,
  AFTER_FEE_DISTRIBUTION: 8n,
};

// Time constants
const ONE_HOUR = 3600n;
const ONE_DAY = 86400n;
const TWO_DAYS = 172800n; // Timelock delay

// Basis points denominator
const BPS_DENOMINATOR = 10000n;

module.exports = {
  VIRTUAL_USDL_DEFAULT,
  VIRTUAL_TOKEN_DEFAULT,
  MIN_FEE_BPS,
  MAX_FEE_BPS,
  BASE_FEE_BPS,
  PROTOCOL_FEE_BPS,
  FEE_DECAY_RATE,
  VOLATILITY_WEIGHT,
  CONCENTRATION_WEIGHT,
  CREATION_FEE,
  TOKEN_DECIMALS,
  TOKEN_TOTAL_SUPPLY,
  DEAD_ADDRESS,
  ZERO_ADDRESS,
  FeeStrategy,
  HookFlags,
  ONE_HOUR,
  ONE_DAY,
  TWO_DAYS,
  BPS_DENOMINATOR,
  p6,
};
