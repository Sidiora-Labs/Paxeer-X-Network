import { BN, Program } from "@coral-xyz/anchor";
import type { WalletAdapterProps } from "@solana/wallet-adapter-base";
import {
  ComputeBudgetProgram,
  Connection,
  PublicKey,
  SystemProgram,
  TransactionMessage,
  VersionedTransaction,
} from "@solana/web3.js";
import { decode as bs58Decode } from "bs58";
import { Hash } from "crypto";
import { keccak256 } from "ethereum-cryptography/keccak";
import { bytesToHex, hexToBytes } from "ethereum-cryptography/utils";
import { AbiCoder, solidityPackedKeccak256 } from "ethers";
import {
  utils as CoreUtils,
  AddPaxeerNetworkKeyInputs,
  RegisterAccountInputs,
  type SettleInputs,
  type SignatureDomain,
  type WithdrawInputs,
  InternalTransferInputs,
} from "@paxeer-network/core";
import { DexRequestInputs } from "@paxeer-network/core";
import {
  DEFAUL_PAXEER_NETWORK_KEY_SCOPE,
  LedgerWalletKey,
} from "@paxeer-network/types";
import {
  ENDPOINT_PROGRAM_ID,
  EXECUTOR_PROGRAM_ID,
  PRICE_FEED_PROGRAM_ID,
  SEND_LIB_PROGRAM_ID,
  TREASURY_PROGRAM_ID,
} from "./constant";
import { IDL as VaultIDL, SolanaVault } from "./idl/solana_vault";
import {
  appendDvnDepositRemainingAccounts,
  appendDvnQuoteRemainingAccounts,
  getBrokerPDA,
  getDefaultSendConfigPda,
  getDefaultSendLibConfigPda,
  getDstEID,
  getEndorcedOptionsPda,
  getEndpointSettingPda,
  getEventAuthorityPda,
  getExecutorConfigPda,
  getLookupTableAccount,
  getLookupTableAddress,
  getMessageLibInfoPda,
  getMessageLibPda,
  getNoncePda,
  getOAppConfigPda,
  getPeerAddress,
  getPeerPda,
  getPriceFeedPda,
  getSendConfigPda,
  getSendLibConfigPda,
  getSendLibInfoPda,
  getSendLibPda,
  getTokenPDA,
  getUlnEventAuthorityPda,
  getUlnSettingPda,
  getTokenAccounts,
  getVaultAuthorityPda,
  getSolVaultPda,
} from "./solana.util";

export function addPaxeerNetworkKeyMessage(
  inputs: AddPaxeerNetworkKeyInputs & { chainId: number },
) {
  const {
    publicKey,
    brokerId,
    expiration = 365,
    timestamp = Date.now(),
    scope,
    tag,
    chainId,
    subAccountId,
  } = inputs;
  const message = {
    brokerId: brokerId,
    chainType: "SOL",
    paxeerNetworkKey: publicKey,
    scope: scope || DEFAUL_PAXEER_NETWORK_KEY_SCOPE,
    chainId,
    timestamp,
    expiration: timestamp + 1000 * 60 * 60 * 24 * expiration,
    ...(typeof tag !== "undefined" ? { tag } : {}),
    ...(typeof subAccountId !== "undefined" ? { subAccountId } : {}),
  };

  const brokerIdHash = solidityPackedKeccak256(["string"], [message.brokerId]);

  const paxeerNetworkKeyHash = solidityPackedKeccak256(
    ["string"],
    [message.paxeerNetworkKey],
  );
  const scopeHash = solidityPackedKeccak256(["string"], [message.scope]);
  const abicoder = AbiCoder.defaultAbiCoder();
  const msgToSign = keccak256(
    hexToBytes(
      abicoder.encode(
        ["bytes32", "bytes32", "bytes32", "uint256", "uint256", "uint256"],
        [
          brokerIdHash,
          paxeerNetworkKeyHash,
          scopeHash,
          message.chainId,
          message.timestamp,
          message.expiration,
        ],
      ),
    ),
  );
  const msgToSignHex = bytesToHex(msgToSign);
  const msgToSignTextEncoded: Uint8Array = new TextEncoder().encode(
    msgToSignHex,
  );
  return [message, msgToSignTextEncoded];
}

export function registerAccountMessage(
  inputs: RegisterAccountInputs & {
    chainId: number;
  },
) {
  const { chainId, registrationNonce, brokerId, timestamp } = inputs;

  const message = {
    brokerId,
    chainId,
    timestamp,
    registrationNonce,
  };
  const brokerIdHash = solidityPackedKeccak256(["string"], [message.brokerId]);
  const abicoder = AbiCoder.defaultAbiCoder();
  const msgToSign = keccak256(
    hexToBytes(
      abicoder.encode(
        ["bytes32", "uint256", "uint256", "uint256"],
        [
          brokerIdHash,
          message.chainId,
          message.timestamp,
          message.registrationNonce,
        ],
      ),
    ),
  );
  const msgToSignHex = bytesToHex(msgToSign);
  const msgToSignTextEncoded: Uint8Array = new TextEncoder().encode(
    msgToSignHex,
  );
  return [message, msgToSignTextEncoded];
}

export function internalTransferMessage(
  inputs: InternalTransferInputs & {
    chainId: number;
  },
) {
  const { chainId, receiver, token, amount, nonce } = inputs;
  const message = {
    chainId,
    receiver,
    token,
    amount,
    transferNonce: nonce,
    chainType: "SOL",
  };

  const tokenSymbolHash = solidityPackedKeccak256(["string"], [message.token]);

  const abicoder = AbiCoder.defaultAbiCoder();
  // StaticStruct staticStruct = new StaticStruct(
  //   new Bytes32(Numeric.hexStringToByteArray(receiver)),
  //   new Bytes32(Numeric.hexStringToByteArray(Hash.sha3(TypeEncoder.encodePacked(new Utf8String(token))))),
  //   new Uint256(amount),
  //   new Uint64(transferNonce),
  //   new Uint256(Long.parseLong(chainId))
  // );
  const msgToSign = keccak256(
    hexToBytes(
      abicoder.encode(
        ["bytes32", "bytes32", "uint256", "uint64", "uint256"],
        [
          message.receiver,
          tokenSymbolHash,
          message.amount,
          message.transferNonce,
          chainId,
        ],
      ),
    ),
  );
  const msgToSignHex = bytesToHex(msgToSign);
  const msgToSignTextEncoded: Uint8Array = new TextEncoder().encode(
    msgToSignHex,
  );
  return [message, msgToSignTextEncoded];
}

export async function dexRequestMessage(
  inputs: DexRequestInputs & {
    domain: SignatureDomain;
    chainId: number;
  },
) {
  const {
    payloadType,
    nonce,
    receiver,
    amount,
    vaultId,
    token,
    dexBrokerId,
    chainId,
  } = inputs;

  const message = {
    payloadType,
    nonce,
    receiver,
    amount,
    vaultId,
    token,
    dexBrokerId,
    chainId,
  };

  let receiverBytes: Uint8Array;
  receiverBytes = bs58Decode(receiver);
  const receiverBytes32 = new Uint8Array(32);
  receiverBytes32.set(receiverBytes);

  const vaultIdHex = vaultId;
  const vaultIdBytes = hexToBytes(vaultIdHex);

  const tokenHash = keccak256(new TextEncoder().encode(token));
  const dexBrokerIdHash = keccak256(new TextEncoder().encode(dexBrokerId));
  const abicoder = AbiCoder.defaultAbiCoder();
  const msgToSign = keccak256(
    hexToBytes(
      abicoder.encode(
        [
          "uint8", // payloadType
          "uint256", // nonce
          "bytes32", // receiver (Base58 decoded)
          "uint256", // amount
          "bytes32", // vaultId
          "bytes32", // token hash
          "bytes32", // dexBrokerId hash
          "uint256", // chainId
        ],
        [
          payloadType,
          nonce,
          receiverBytes,
          amount,
          vaultIdBytes,
          tokenHash,
          dexBrokerIdHash,
          chainId,
        ],
      ),
    ),
  );

  const msgToSignHex = bytesToHex(msgToSign);
  const msgToSignTextEncoded: Uint8Array = new TextEncoder().encode(
    msgToSignHex,
  );

  return [message, msgToSignTextEncoded];
}

export function withdrawMessage(
  inputs: WithdrawInputs & {
    chainId: number;
  },
) {
  const { chainId, receiver, token, amount, nonce, brokerId } = inputs;
  const timestamp = Date.now();

  const message = {
    brokerId,
    chainId,
    receiver,
    token: token,
    amount: amount,
    withdrawNonce: nonce,
    timestamp,
    chainType: "SOL",
  };

  const brokerIdHash = solidityPackedKeccak256(["string"], [message.brokerId]);
  const tokenSymbolHash = solidityPackedKeccak256(["string"], [message.token]);
  const salt = keccak256(Buffer.from("paxeer-network Network"));
  const abicoder = AbiCoder.defaultAbiCoder();

  const msgToSign = keccak256(
    hexToBytes(
      abicoder.encode(
        [
          "bytes32",
          "bytes32",
          "uint256",
          "bytes32",
          "uint256",
          "uint64",
          "uint64",
          "bytes32",
        ],
        [
          brokerIdHash,
          tokenSymbolHash,
          chainId,
          bs58Decode(message.receiver),
          message.amount,
          message.withdrawNonce,
          timestamp,
          salt,
        ],
      ),
    ),
  );
  const msgToSignHex = bytesToHex(msgToSign);
  const msgToSignTextEncoded: Uint8Array = new TextEncoder().encode(
    msgToSignHex,
  );
  return [message, msgToSignTextEncoded];
}

export function settleMessage(
  inputs: SettleInputs & {
    chainId: number;
  },
) {
  const { settlePnlNonce, brokerId, chainId, timestamp } = inputs;

  const message = {
    brokerId: brokerId,
    chainId: chainId,
    timestamp: timestamp,
    chainType: "SOL",
    settleNonce: settlePnlNonce,
  };
  const brokerIdHash = solidityPackedKeccak256(["string"], [brokerId]);

  const abicoder = AbiCoder.defaultAbiCoder();
  const msgToSign = keccak256(
    hexToBytes(
      abicoder.encode(
        ["bytes32", "uint256", "uint64", "uint64"],
        [brokerIdHash, message.chainId, message.settleNonce, message.timestamp],
      ),
    ),
  );
  const msgToSignHex = bytesToHex(msgToSign);
  const msgToSignTextEncoded: Uint8Array = new TextEncoder().encode(
    msgToSignHex,
  );
  return [message, msgToSignTextEncoded];
}

export enum MsgType {
  Deposit = 0,
  // Add other message types if needed
}

export interface LzMessage {
  msgType: MsgType;
  payload: Buffer;
}

export function encodeLzMessage(message: LzMessage): Buffer {
  const msgTypeBuffer = Buffer.alloc(1);
  msgTypeBuffer.writeUInt8(message.msgType);
  return Buffer.concat([msgTypeBuffer, message.payload]);
}

export async function getDepositQuoteFee({
  vaultAddress,
  userAddress,
  connection,
  depositData,
}: {
  vaultAddress: string;
  userAddress: string;
  connection: Connection;
  depositData: {
    tokenHash: string;
    brokerHash: string;
    accountId: string;
    tokenAddress: string;
    tokenAmount: string;
  };
}) {
  console.log("-- vaultAddress", vaultAddress);
  const appProgramId = new PublicKey(vaultAddress);
  const DST_EID = getDstEID(appProgramId);

  const program = new Program<SolanaVault>(VaultIDL, appProgramId, {
    connection,
  });
  const userPublicKey = new PublicKey(userAddress);

  const oappConfigPDA = getOAppConfigPda(appProgramId);
  const peerPDA = getPeerPda(appProgramId, oappConfigPDA, DST_EID);
  const endorcedPDA = getEndorcedOptionsPda(
    appProgramId,
    oappConfigPDA,
    DST_EID,
  );
  const sendLibConfigPDA = getSendLibConfigPda(oappConfigPDA, DST_EID);
  const defaultSendLibPDA = getDefaultSendLibConfigPda(DST_EID);

  const endpointSettingPDA = getEndpointSettingPda();
  const noncePDA = getNoncePda(appProgramId, oappConfigPDA, DST_EID);
  const sendConfigPDA = getSendConfigPda(oappConfigPDA, DST_EID);
  const defaultSendConfigPDA = getDefaultSendConfigPda(DST_EID);
  const executorConfigPDA = getExecutorConfigPda();
  const priceFeedPDA = getPriceFeedPda();

  const messageLibPDA = getMessageLibPda(SEND_LIB_PROGRAM_ID);
  const messageLibInfoPDA = getMessageLibInfoPda(messageLibPDA);
  const vaultAuthorityPDA = getVaultAuthorityPda(appProgramId);

  const depositParams = getDepositParams(userAddress, depositData);

  // deposit fee
  const quoteFee = await program.methods
    .oappQuote(depositParams)
    .accounts({
      oappConfig: oappConfigPDA,
      peer: peerPDA,
      enforcedOptions: endorcedPDA,
      vaultAuthority: vaultAuthorityPDA,
    })
    .remainingAccounts([
      // ENDPOINT solana/programs/programs/uln/src/instructions/endpoint/send.rs
      {
        pubkey: ENDPOINT_PROGRAM_ID,
        isWritable: false,
        isSigner: false,
      },
      {
        pubkey: SEND_LIB_PROGRAM_ID,
        isWritable: false,
        isSigner: false,
      },
      {
        pubkey: sendLibConfigPDA, // send_library_config
        isWritable: false,
        isSigner: false,
      },
      {
        pubkey: defaultSendLibPDA, // default_send_library_config
        isWritable: false,
        isSigner: false,
      },
      {
        pubkey: messageLibInfoPDA, // send_library_info
        isWritable: false,
        isSigner: false,
      },
      {
        pubkey: endpointSettingPDA, // endpoint settings
        isWritable: false,
        isSigner: false,
      },
      {
        pubkey: noncePDA, // nonce
        isWritable: false,
        isSigner: false,
      },
      {
        pubkey: messageLibPDA,
        isWritable: false,
        isSigner: false,
      },
      {
        pubkey: sendConfigPDA,
        isWritable: false,
        isSigner: false,
      },
      {
        pubkey: defaultSendConfigPDA,
        isWritable: false,
        isSigner: false,
      },
      {
        pubkey: EXECUTOR_PROGRAM_ID,
        isWritable: false,
        isSigner: false,
      },
      {
        pubkey: executorConfigPDA,
        isWritable: false,
        isSigner: false,
      },
      {
        pubkey: PRICE_FEED_PROGRAM_ID,
        isWritable: false,
        isSigner: false,
      },
      {
        pubkey: priceFeedPDA,
        isWritable: false,
        isSigner: false,
      },
      ...appendDvnQuoteRemainingAccounts(appProgramId, priceFeedPDA),
    ])
    .instruction();

  const lastBlockHash = await connection.getLatestBlockhash();

  const lookupTableAddress = getLookupTableAddress(appProgramId);
  const lookupTableAccount = await getLookupTableAccount(
    connection,
    lookupTableAddress,
  );
  if (!lookupTableAccount) {
    console.log("-- lookup table account error");
    throw new Error("-- lookup table account error");
  }
  const ixQuoteComputeBudget = ComputeBudgetProgram.setComputeUnitLimit({
    units: 600_000,
  });
  const feeMsg = new TransactionMessage({
    payerKey: userPublicKey,
    recentBlockhash: lastBlockHash.blockhash,
    instructions: [ixQuoteComputeBudget, quoteFee],
  }).compileToV0Message([lookupTableAccount]);

  const feeTx = new VersionedTransaction(feeMsg);

  const feeRes = await connection.simulateTransaction(feeTx);

  console.log("-- feeRes", feeRes);
  if (feeRes.value.err) {
    const errorInfo =
      typeof feeRes.value.err === "object"
        ? JSON.stringify(feeRes.value.err)
        : feeRes.value.err;

    if (errorInfo.toString().includes("AccountNotFound")) {
      throw new Error("Error: Account gas is insufficient.");
    }

    throw new Error(`Error: ${errorInfo}`);
  }
  const returnPrefix = `Program return: ${program.programId} `;
  const returnLogEntry = feeRes.value.logs!.find((log) =>
    log.startsWith(returnPrefix),
  );
  if (!returnLogEntry) {
    throw new Error("Error: get deposit fee error");
  }

  // Slice out the prefix to get the base64 return data
  const encodedReturnData = returnLogEntry.slice(returnPrefix.length);

  // Convert the Base64 return data
  const decodedBuffer = Buffer.from(encodedReturnData, "base64");

  return decodedBuffer.readBigUInt64LE(0);
}
const getDepositParams = (
  userAddress: string,
  depositData: {
    tokenHash: string;
    brokerHash: string;
    accountId: string;
    tokenAddress: string;
    tokenAmount: string;
  },
) => {
  const brokerHash = depositData.brokerHash;
  const codedBrokerHash = Array.from(Buffer.from(brokerHash.slice(2), "hex"));

  const tokenHash = depositData.tokenHash;
  const codedTokenHash = Array.from(Buffer.from(tokenHash.slice(2), "hex"));

  const solAccountId = depositData.accountId;
  const codedAccountId = Array.from(Buffer.from(solAccountId.slice(2), "hex"));
  const userPublicKey = new PublicKey(userAddress);

  return {
    accountId: codedAccountId,
    brokerHash: codedBrokerHash,
    tokenHash: codedTokenHash,
    userAddress: Array.from(userPublicKey.toBuffer()),
    tokenAmount: new BN(depositData.tokenAmount),
  };
};

export async function deposit({
  vaultAddress,
  userAddress,
  connection,
  sendTransaction,
  depositData,
}: {
  vaultAddress: string;
  userAddress: string;
  connection: Connection;
  sendTransaction: WalletAdapterProps["sendTransaction"];
  depositData: {
    tokenHash: string;
    brokerHash: string;
    accountId: string;
    tokenAddress: string;
    tokenAmount: string;
  };
}) {
  const brokerHash = depositData.brokerHash;
  const tokenHash = depositData.tokenHash;

  const SOL_HASH = CoreUtils.parseTokenHash("SOL");
  const isSolDeposit = tokenHash.toLowerCase() === SOL_HASH.toLowerCase();

  console.log("-- vault address", vaultAddress);
  const appProgramId = new PublicKey(vaultAddress);
  const program = new Program<SolanaVault>(VaultIDL, appProgramId, {
    connection,
  });
  // If is not SOL deposit, tokenAddress is the token address
  // else, tokenAddress is the USDC address
  const token = new PublicKey(depositData.tokenAddress);
  const userPublicKey = new PublicKey(userAddress);
  const userTokenAccount = getTokenAccounts(token, userPublicKey);
  const vaultAuthorityPda = getVaultAuthorityPda(appProgramId);
  const vaultTokenAccount = getTokenAccounts(token, vaultAuthorityPda);
  const allowedBrokerPDA = getBrokerPDA(appProgramId, brokerHash);
  const allowedTokenPDA = getTokenPDA(appProgramId, tokenHash);
  const oappConfigPDA = getOAppConfigPda(appProgramId);
  console.log("-- oappconfig pda", oappConfigPDA.toBase58());
  const DST_EID = getDstEID(appProgramId);
  // const lzPDA = getLzReceiveTypesPda(appProgramId, oappConfigPDA);
  const peerPDA = getPeerPda(appProgramId, oappConfigPDA, DST_EID);
  const endorcedPDA = getEndorcedOptionsPda(
    appProgramId,
    oappConfigPDA,
    DST_EID,
  );
  const sendLibPDA = getSendLibPda();
  const sendLibConfigPDA = getSendLibConfigPda(oappConfigPDA, DST_EID);
  const defaultSendLibPDA = getDefaultSendLibConfigPda(DST_EID);
  const sendLibInfoPDA = getSendLibInfoPda(sendLibPDA);

  const endpointSettingPDA = getEndpointSettingPda();
  const noncePDA = getNoncePda(appProgramId, oappConfigPDA, DST_EID);
  const eventAuthorityPDA = getEventAuthorityPda();
  const ulnSettingPDA = getUlnSettingPda();
  const sendConfigPDA = getSendConfigPda(oappConfigPDA, DST_EID);
  const defaultSendConfigPDA = getDefaultSendConfigPda(DST_EID);
  const ulnEventAuthorityPDA = getUlnEventAuthorityPda();
  const executorConfigPDA = getExecutorConfigPda();
  const priceFeedPDA = getPriceFeedPda();

  const vaultDepositParams = getDepositParams(userAddress, depositData);

  const buildSendRemainingAccounts = () => [
    // ENDPOINT solana/programs/programs/uln/src/instructions/endpoint/send.rs
    {
      isSigner: false,
      isWritable: false,
      pubkey: ENDPOINT_PROGRAM_ID,
    },
    {
      isSigner: false,
      isWritable: false,
      // 0
      pubkey: oappConfigPDA,
    },
    {
      isSigner: false,
      isWritable: false,
      pubkey: SEND_LIB_PROGRAM_ID,
    },
    {
      isSigner: false,
      isWritable: false,
      // 7
      pubkey: sendLibConfigPDA,
    },
    {
      isSigner: false,
      isWritable: false,
      // 9
      pubkey: defaultSendLibPDA,
    },
    {
      isSigner: false,
      isWritable: false,
      // 8
      pubkey: sendLibInfoPDA,
    },
    {
      isSigner: false,
      isWritable: false,
      // 14
      pubkey: endpointSettingPDA,
    },
    {
      isSigner: false,
      isWritable: true,
      // 15
      pubkey: noncePDA,
    },
    {
      isSigner: false,
      isWritable: false,
      // 3
      pubkey: eventAuthorityPDA,
    },
    // ULN solana/programs/programs/uln/src/instructions/endpoint/send.rs
    {
      isSigner: false,
      isWritable: false,
      pubkey: ENDPOINT_PROGRAM_ID,
    },
    {
      isSigner: false,
      isWritable: false,
      // 13
      pubkey: ulnSettingPDA,
    },
    {
      isSigner: false,
      isWritable: false,
      // 10
      pubkey: sendConfigPDA,
    },
    {
      isSigner: false,
      isWritable: false,
      // 11
      pubkey: defaultSendConfigPDA,
    },
    {
      isSigner: true,
      isWritable: false,
      pubkey: userPublicKey,
    },
    {
      isSigner: false,
      isWritable: false,
      pubkey: TREASURY_PROGRAM_ID,
    },
    {
      isSigner: false,
      isWritable: false,
      pubkey: SystemProgram.programId,
    },
    {
      isSigner: false,
      isWritable: false,
      // 12
      pubkey: ulnEventAuthorityPDA,
    },
    {
      isSigner: false,
      isWritable: false,
      pubkey: SEND_LIB_PROGRAM_ID,
    },
    {
      isSigner: false,
      isWritable: false,
      pubkey: EXECUTOR_PROGRAM_ID,
    },
    {
      isSigner: false,
      isWritable: true,
      // 16
      pubkey: executorConfigPDA,
    },
    {
      isSigner: false,
      isWritable: false,
      pubkey: PRICE_FEED_PROGRAM_ID,
    },
    {
      isSigner: false,
      isWritable: false,
      // 17
      pubkey: priceFeedPDA,
    },
    ...appendDvnDepositRemainingAccounts(appProgramId, priceFeedPDA),
  ];

  const fee = await getDepositQuoteFee({
    vaultAddress,
    userAddress,
    connection,
    depositData,
  });

  const sendParam = {
    nativeFee: new BN(fee.toString()),
    lzTokenFee: new BN(0),
  };

  // const sendParam = {
  //     nativeFee: new BN(1_000_000_000),
  //     lzTokenFee:new BN(0),
  //
  // }
  console.log("--- value params", {
    vaultDepositParams,
    sendParam,
  });
  const ixDepositEntry = isSolDeposit
    ? await program.methods
        .depositSol(vaultDepositParams, sendParam)
        .accounts({
          solVault: getSolVaultPda(appProgramId),
          vaultAuthority: vaultAuthorityPda,
          user: userPublicKey,
          peer: peerPDA,
          enforcedOptions: endorcedPDA,
          oappConfig: oappConfigPDA,
          allowedBroker: allowedBrokerPDA,
          allowedToken: allowedTokenPDA,
        })
        .remainingAccounts(buildSendRemainingAccounts())
        .instruction()
    : await program.methods
        .deposit(vaultDepositParams, sendParam)
        .accounts({
          userTokenAccount: userTokenAccount,
          vaultAuthority: vaultAuthorityPda,
          vaultTokenAccount: vaultTokenAccount,
          depositToken: token,
          user: userPublicKey,
          peer: peerPDA,
          enforcedOptions: endorcedPDA,
          oappConfig: oappConfigPDA,
          allowedBroker: allowedBrokerPDA,
          allowedToken: allowedTokenPDA,
        })
        .remainingAccounts(buildSendRemainingAccounts())
        .instruction();

  const lookupTableAddress = getLookupTableAddress(appProgramId);
  const lookupTableAccount = await getLookupTableAccount(
    connection,
    lookupTableAddress,
  );
  if (!lookupTableAccount) {
    console.log("-- lookup table account error");
    return;
  }

  const ixAddComputeBudget = ComputeBudgetProgram.setComputeUnitLimit({
    units: 800_000,
  });

  const lastBlockHash = await connection.getLatestBlockhash();
  const msg = new TransactionMessage({
    payerKey: userPublicKey,
    recentBlockhash: lastBlockHash.blockhash,
    instructions: [ixDepositEntry, ixAddComputeBudget],
  }).compileToV0Message([lookupTableAccount]);

  const tx = new VersionedTransaction(msg);

  const res = await sendTransaction(tx, connection);
  console.log("res", res);
  return res;
}

export function checkIsLedgerWallet(userAddress: string): boolean {
  const info = window.localStorage.getItem(LedgerWalletKey);
  if (!info) {
    return false;
  }
  const addressArr = JSON.parse(info ?? "[]");
  console.log("-- addressArr", addressArr);
  if (addressArr.includes(userAddress)) {
    return true;
  }
  return false;
}
