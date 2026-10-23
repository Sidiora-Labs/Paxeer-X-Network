/**
 * Typed ABIs and precompile addresses for the PaxeerX bridge.
 *
 * - ILayerXBridge at 0x…1016 — repository: precompiles/layerxbridge/LayerXBridge.sol
 *   (JSON kept verbatim in ./layerx-bridge-abi.json, copied from
 *   precompiles/layerxbridge/abi.json and narrowed below to a typed `Abi`).
 * - PaxeerXVault — repository: contracts/bridge/evm/src/PaxeerXVault.sol
 *   (no compiled artifact is committed upstream; this ABI is transcribed
 *   from that source, user-facing surface only).
 * - IBank at 0x…1001 — repository: precompiles/bank/Bank.sol.
 */
import { parseAbi, type Abi } from "viem";

import layerxBridgeAbiJson from "./layerx-bridge-abi.json";

export const LAYERX_BRIDGE_ADDRESS = "0x0000000000000000000000000000000000001016" as const;
export const BANK_ADDRESS = "0x0000000000000000000000000000000000001001" as const;

export const layerxBridgeAbi = layerxBridgeAbiJson as unknown as Abi;

/** PaxeerXVault, user-facing surface (writes, views, events, custom errors). */
export const paxeerXVaultAbi = parseAbi([
  "function deposit(address asset, uint256 amount, bytes32 paxeerRecipient)",
  "function depositNative(bytes32 paxeerRecipient) payable",
  "function release(address asset, uint256 amount, address recipient, bytes32 paxeerTxHash, uint64 paxeerNonce, bytes[] signatures)",
  "function depositDigest(bytes32 txHash, uint64 logIndex, bytes32 paxeerRecipient, address asset, uint256 amount) view returns (bytes32)",
  "function releaseDigest(bytes32 paxeerTxHash, uint64 paxeerNonce, address recipient, address asset, uint256 amount) view returns (bytes32)",
  "function nullifierOf(bytes32 paxeerTxHash, uint64 paxeerNonce) pure returns (bytes32)",
  "function nullified(bytes32) view returns (bool)",
  "function caps(address) view returns (uint256 perTx, uint256 total)",
  "function outstanding(address) view returns (uint256)",
  "function registered(address) view returns (bool)",
  "function paused() view returns (bool)",
  "function threshold() view returns (uint256)",
  "function attestors() view returns (address[])",
  "function isAttestor(address) view returns (bool)",
  "function depositNonce() view returns (uint64)",
  "function owner() view returns (address)",
  "event BridgeDeposit(address indexed asset, uint256 amount, address indexed sender, bytes32 indexed paxeerRecipient, uint64 nonce)",
  "event BridgeRelease(address indexed asset, uint256 amount, address indexed recipient, bytes32 indexed paxeerTxHash, uint64 paxeerNonce)",
  "error WhenPaused()",
  "error NotPaused()",
  "error ZeroAmount()",
  "error InvalidRecipient()",
  "error InvalidAttestor(address attestor)",
  "error InvalidThreshold(uint256 threshold, uint256 attestors)",
  "error AssetNotEnabled(address asset)",
  "error PerTxCapExceeded(uint256 amount, uint256 perTx)",
  "error TotalCapExceeded(uint256 outstandingAfter, uint256 total)",
  "error InsufficientOutstanding(uint256 amount, uint256 outstanding)",
  "error NullifierUsed(bytes32 nullifier)",
  "error BelowThreshold(uint256 signatures, uint256 threshold)",
  "error InvalidSignature()",
  "error SignersNotAscending(address signer)",
  "error UnknownSigner(address signer)",
  "error UseDepositNative()",
  "error TransferAmountMismatch(uint256 received, uint256 amount)",
  "error NativeTransferFailed()",
  "error NativeAssetNotRescuable()",
  "error OutstandingNotZero(address token, uint256 outstanding)",
  "error AssetRegisteredForBridging(address token)",
  "error NothingToRescue(address token)",
]);

/** Bank precompile, the reads the bridge UI needs. */
export const bankAbi = parseAbi([
  "function balance(address acc, string denom) view returns (uint256)",
  "function supply(string denom) view returns (uint256)",
  "function name(string denom) view returns (string)",
  "function symbol(string denom) view returns (string)",
  "function decimals(string denom) view returns (uint8)",
]);

/** ERC-20 surface used on foreign chains (allowance + exact approve + balance). */
export const erc20Abi = parseAbi([
  "function balanceOf(address account) view returns (uint256)",
  "function allowance(address owner, address spender) view returns (uint256)",
  "function approve(address spender, uint256 amount) returns (bool)",
  "function decimals() view returns (uint8)",
  "function symbol() view returns (string)",
]);
