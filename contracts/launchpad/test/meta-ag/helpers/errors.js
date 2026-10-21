/**
 * Sidiora Meta-AG — Canonical custom-error registry for test assertions.
 *
 * Source of truth: the Solidity `error Foo(...)` declarations inside each
 * contract under `contracts/meta-ag/`. The catalogue below is organised by
 * layer to match the build order; when a new custom error ships in Phase N,
 * add it under the matching block and reference the file it lives in so the
 * registry stays grep-able.
 *
 * Usage:
 *
 *   const { ERRORS } = require("../helpers/errors");
 *   await expect(call).to.be.revertedWithCustomError(contract, ERRORS.common.ZeroAddress);
 *
 * For raw selector comparisons:
 *
 *   const { selectorOf } = require("../helpers/errors");
 *   expect(selectorOf("Unauthorized()")).to.equal("0x82b42900");
 */

const { ethers } = require("hardhat");
const { keccak256, toUtf8Bytes } = ethers;

/**
 * Compute the 4-byte selector of any Solidity error/function signature.
 * Accepts a full canonical signature such as "SlippageTooHigh(uint256,uint256)".
 */
function selectorOf(signature) {
  return keccak256(toUtf8Bytes(signature)).slice(0, 10);
}

// --------------------------------------------------------------------------- //
// Plan Phase 1 — Interfaces (pure, no custom errors yet)                      //
// --------------------------------------------------------------------------- //
// Interfaces do not declare custom errors; implementations do. Names below    //
// are the canonical strings consuming tests pass to                           //
// `revertedWithCustomError(contract, name)` — the mapping to a specific       //
// Solidity signature is in the matching contract.                             //
// --------------------------------------------------------------------------- //

const ERRORS = Object.freeze({
  // Shared across every contract in contracts/meta-ag/**
  // These map to errors declared by contracts/base/*, NOT OpenZeppelin upstream names.
  common: Object.freeze({
    ZeroAddress:        "ZeroAddress",
    ZeroAmount:         "ZeroAmount",
    InvalidArrayLength: "InvalidArrayLength",
    Deadline:           "DeadlineExpired",
    Reentrancy:         "ReentrancyGuardReentrantCall",
    // base/Pausable.sol -> error Paused()
    Paused:             "Paused",
    // base/AccessControl.sol -> error MissingRole(address account, bytes32 role)
    Unauthorized:       "MissingRole",
    // base/Initializable.sol -> error AlreadyInitialized()
    AlreadyInitialized: "AlreadyInitialized",
    // base/UUPSUpgradeable.sol -> error UnauthorizedUpgrade()
    UnauthorizedUpgrade: "UnauthorizedUpgrade",
    InvalidImplementation: "InvalidImplementation",
  }),
  // Oracle layer (Phase 2)
  oracle: Object.freeze({
    StalePrice:                 "StalePrice",
    DeviationExceeded:          "DeviationExceeded",
    ConfidenceTooLow:           "ConfidenceTooLow",
    NoActiveAdapters:           "NoActiveAdapters",
    AdapterAlreadyRegistered:   "AdapterAlreadyRegistered",
    AdapterNotFound:            "AdapterNotFound",
    MaxAdaptersReached:         "MaxAdaptersReached",
    PriceOutOfBounds:           "PriceOutOfBounds",
    TokenNotConfigured:         "TokenNotConfigured",
    TwapWindowInvalid:          "TwapWindowInvalid",
  }),
  // Vault v2 (Phase 3)
  vault: Object.freeze({
    TokenNotRegistered:   "TokenNotRegistered",
    TokenAlreadyRegistered: "TokenAlreadyRegistered",
    ReservesMismatch:     "ReservesMismatch",
    OperatorNotAuthorized:"OperatorNotAuthorized",
    NativeTransferFailed: "NativeTransferFailed",
    WethOnly:             "WethOnly",
  }),
  // Engine (Phase 4)
  engine: Object.freeze({
    InsufficientLiquidity: "InsufficientLiquidity",
    SlippageTooHigh:       "SlippageTooHigh",
    FeeOutOfBounds:        "FeeOutOfBounds",
    ImpactOutOfBounds:     "ImpactOutOfBounds",
    SameToken:             "SameToken",
    OrderNotFound:         "OrderNotFound",
    OrderExpired:          "OrderExpired",
    OrderNotExecutable:    "OrderNotExecutable",
    OrderAlreadyActive:    "OrderAlreadyActive",
  }),
  // Router adapters (Phase 5)
  adapter: Object.freeze({
    UnsupportedSwap:      "UnsupportedSwap",
    InvalidAdapterData:   "InvalidAdapterData",
    QuoteUnavailable:     "QuoteUnavailable",
  }),
  // MetaAGRouter (Phase 6)
  router: Object.freeze({
    NoAdaptersAvailable:   "NoAdaptersAvailable",
    MaxHopsExceeded:       "MaxHopsExceeded",
    OracleSanityFailed:    "OracleSanityFailed",
    AdapterInactive:       "AdapterInactive",
    BestQuoteUnavailable:  "BestQuoteUnavailable",
  }),
  // Analytics (Phase 7)
  analytics: Object.freeze({
    EmitterNotAuthorized: "EmitterNotAuthorized",
    DayAlreadySnapshot:   "DayAlreadySnapshot",
  }),
});

module.exports = {
  ERRORS,
  selectorOf,
};
