import { addressToBytes32 } from "@layerzerolabs/lz-v2-utilities";
import { PublicKey } from "@solana/web3.js";

export const ENDPOINT_PROGRAM_ID = new PublicKey(
  "76y77prsiCMvXMjuoZ5VRrhG5qYBrUMYTE5WgHqgjEn6",
);
export const SEND_LIB_PROGRAM_ID = new PublicKey(
  "7a4WjyR8VZ7yZz5XJAKm39BUGn5iT9CKcv2pmG9tdXVH",
);
export const EXECUTOR_PROGRAM_ID = new PublicKey(
  "6doghB248px58JSSwG4qejQ46kFMW4AMj7vzJnWZHNZn",
);
export const PRICE_FEED_PROGRAM_ID = new PublicKey(
  "8ahPGPjEbpgGaZx2NV1iG5Shj7TDwvsjkEDcGWjt94TP",
);
export const RECEIVE_LIB_PROGRAM_ID = SEND_LIB_PROGRAM_ID;
export const TREASURY_PROGRAM_ID = SEND_LIB_PROGRAM_ID;
export const DVN_PROGRAM_ID = new PublicKey(
  "HtEYV4xB4wvsj5fgTkcfuChYpvGYzgzwvNhgDZQNh7wW",
);
/** Same program as `DVN_PROGRAM_ID`; aligns with solana-vault ULN 3-DVN scripts. */
export const LZ_DVN_PROGRAM_ID = DVN_PROGRAM_ID;
export const CANARY_DVN_PROGRAM_ID = new PublicKey(
  "5KAALa8AEEKnW6p6AacdnqNDmGMpfhwR7AEyWs1gUvsT",
);
export const NEVERMIND_DVN_PROGRAM_ID = new PublicKey(
  "4fs6aL12L18K5giDy9Dgxgrb3aNRYiuRV2a7JPPj3e7F",
);
/** LZ DVN config PDA; must match `getDvnConfigPda()` derivation in solana.util. */
export const LZ_DVN_PDA = new PublicKey(
  "4VDjp6XQaxoZf5RGwiPU9NR1EXSZn2TP4ATMmiSzLfhb",
);
export const CANARY_DVN_PDA = new PublicKey(
  "7jMeX5mzXnSSKYd8DxBDP4xMnkNFZZZm5W28FWUTbwU3",
);
export const NEVERMIND_DVN_PDA = new PublicKey(
  "GPjyWr8vCotGuFubDpTxDxy9Vj1ZeEN4F2dwRmFiaGab",
);
export const VAULT_AUTHORITY_SEED = "VaultAuthority";
export const BROKER_SEED = "Broker";
export const TOKEN_SEED = "Token";
export const SOL_VAULT_SEED = "SolVault";
// fro dev
// export const PEER_ADDRESS = addressToBytes32('0x9Dc724b24146BeDD2dA28b8C4B74126169B8f312');
// for qa
export const DEV_PEER_ADDRESS = addressToBytes32(
  "0x9Dc724b24146BeDD2dA28b8C4B74126169B8f312",
);
export const QA_PEER_ADDRESS = addressToBytes32(
  "0x45b6C6266A7A2170617d8A27A50C642fd68b91c4",
);
export const STAGING_PEER_ADDRESS = addressToBytes32(
  "0x5Bf771A65d057e778C5f0Ed52A0003316f94322D",
);
export const MAINNET_PEER_ADDRESS = addressToBytes32(
  "0xCecAe061aa078e13b5e70D5F9eCee90a3F2B6AeA",
);

export const DEV_DST_EID = 40200;
export const MAIN_DST_EID = 30213;

export const DEV_LOOKUP_TABLE_ADDRESS = new PublicKey(
  "BWp8HaYYhiNHekt3zgQhqoCrRftneGxxfgKmCZ6svHN",
);
export const QA_LOOKUP_TABLE_ADDRESS = new PublicKey(
  "BswrQQoPKAFojTuJutZcBMtigAgTghEH4M8ofn3EG2X2",
);
export const STAGING_LOOKUP_TABLE_ADDRESS = new PublicKey(
  "BbGKfxuPwDmu58BjPpd7PMG69TqnZjSpKaLDMgf9E9Dr",
);
export const MAINNET_LOOKUP_TABLE_ADDRESS = new PublicKey(
  "8iq7xCQt3bLdRRn4A46d5GuaXYinBoiAhbe2sUmZVzwg",
);

export const DEV_OAPP_PROGRAM_ID = new PublicKey(
  "EYJq9eU4GMRUriUJBgGoZ8YLQBXcWaciXuSsEXE7ieQS",
);
export const QA_OAPP_PROGRAM_ID = new PublicKey(
  "5zBjLor7vEraAt4zp2H82sy9MSqFoDnNa1Lx6EYKTYRZ",
);
export const STAGING_OAPP_PROGRAM_ID = new PublicKey(
  "9shwxWDUNhtwkHocsUAmrNAQfBH2DHh4njdAEdHZZkF2",
);
export const MAINNET_OAPP_PROGRAM_ID = new PublicKey(
  "ErBmAD61mGFKvrFNaTJuxoPwqrS8GgtwtqJTJVjFWx9Q",
);
