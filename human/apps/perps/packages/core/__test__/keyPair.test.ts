import { BasePaxeerNetworkKeyPair } from "../src";
import * as ed from "@noble/ed25519";

beforeAll(() => {
  const crypto = require("crypto");
  global.crypto = crypto;
});

describe("keyPair", () => {
  test("get public", async () => {
    const keyPair = new BasePaxeerNetworkKeyPair(
      "AFmQSju4FhDwG93cMdKogcnKx7SWmViDtDv5PVzfvRDF"
    );

    const publicKey = await keyPair.getPublicKey();

    //

    expect(publicKey).toBe(
      "ed25519:FwPH3gH4G6NqFyYr1YD5njRC6FGNRPRXnZ5SLAxHie5J"
    );
  });

  // it("ramdom key", async () => {
  //   const keyPair = BasePaxeerNetworkKeyPair.generateKey();
  //
  // });

  // describe("generate paxeer-network key, secretKey len eq 44", () => {

  // const keys = new Array(100).map(() => [
  //   BasePaxeerNetworkKeyPair.generateKey().secretKey,
  //   44,
  // ]);

  //

  // test.each(keys)("secretKey len eq 44", (key, expected) => {
  //
  //   expect(key.length).toBe(expected);
  // });

  test("secretKey len eq 44", () => {
    let index = 100;
    while (index > 0) {
      index--;
      const keyPair = BasePaxeerNetworkKeyPair.generateKey();

      expect(keyPair.secretKey.length).toBe(44);
    }
  });

  //   // expect(keyPair).toBeTruthy();
  // });
});
