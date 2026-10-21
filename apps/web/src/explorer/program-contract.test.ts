import assert from "node:assert/strict";
import test from "node:test";

import { decodeProgramForIdentifier } from "./model.ts";

function projection() {
  return {
    program: "11".repeat(32),
    upgrade_policy: { kind: "immutable" },
    lifecycle: "active",
    versions: [{
      version: "1", code_hash: "22".repeat(32), abi_version: "4",
      interface_digest: "33".repeat(32),
      source: { status: "verified", source_digest: "44".repeat(32), environment_digest: "55".repeat(32) },
    }],
    value_accounts: [], observed_sequence: "7", observed_at: "1700000000000",
    receipt_digest: "66".repeat(32), state_root: "77".repeat(32),
  };
}

test("the program consumer retains the producer's interface and verified source metadata", () => {
  const program = decodeProgramForIdentifier(projection(), "11".repeat(32));
  assert.equal(program.versions[0]?.interfaceDigest, "33".repeat(32));
  assert.deepEqual(program.versions[0]?.source, {
    status: "verified", sourceDigest: "44".repeat(32), environmentDigest: "55".repeat(32),
  });
});

test("a projection for another program never satisfies the requested program", () => {
  assert.throws(() => decodeProgramForIdentifier(projection(), "99".repeat(32)), TypeError);
  assert.throws(() => decodeProgramForIdentifier(projection(), "invalid"), TypeError);
});

test("malformed interface and incomplete verified source metadata remain refused", () => {
  const badInterface = projection();
  badInterface.versions[0]!.interface_digest = "not-a-digest";
  assert.throws(() => decodeProgramForIdentifier(badInterface, "11".repeat(32)), TypeError);
  const badSource = projection();
  badSource.versions[0]!.source.environment_digest = "";
  assert.throws(() => decodeProgramForIdentifier(badSource, "11".repeat(32)), TypeError);
});
