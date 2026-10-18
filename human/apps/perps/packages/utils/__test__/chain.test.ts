import {
  ADI_TESTNET_CHAINID,
  XLAYER_TESTNET_CHAINID,
} from "@paxeer-network/types";
import { isTestnet } from "../src/chain";

describe("chain", () => {
  test("identifies ADI Testnet as a testnet chain", () => {
    expect(isTestnet(ADI_TESTNET_CHAINID)).toBe(true);
  });

  test("identifies X Layer Testnet as a testnet chain", () => {
    expect(isTestnet(XLAYER_TESTNET_CHAINID)).toBe(true);
  });
});
