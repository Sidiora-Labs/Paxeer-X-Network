const { expect } = require("chai");
const fs = require("fs");
const os = require("os");
const path = require("path");
const { verifyRelease, sha256 } = require("../../scripts/lib/release-check");

describe("Production: release evidence rejects drift", function () {
  let root, info, artifact;
  function write(relative, value) {
    const file = path.join(root, relative);
    fs.mkdirSync(path.dirname(file), { recursive: true });
    fs.writeFileSync(file, typeof value === "string" ? value : JSON.stringify(value));
  }
  function persist() {
    write("artifacts/build-info/fixture.json", info);
    write("artifacts/contracts/Example.sol/Example.json", artifact);
  }
  function verify(tracked = []) { return verifyRelease({ root, tracked }); }
  beforeEach(function () {
    root = fs.mkdtempSync(path.join(os.tmpdir(), "falcon-release-check-"));
    const source = 'pragma solidity ^0.8.27; import "./Library.sol"; contract Example {}';
    const dependency = 'pragma solidity ^0.8.27; library Library {}';
    const layout = { storage: [{ label: "balance", slot: "0", offset: 0, type: "u256" }], types: { u256: { encoding: "inplace", label: "uint256", numberOfBytes: "32" } } };
    const output = { abi: [], evm: { bytecode: { object: "6000" }, deployedBytecode: { object: "6000" } }, storageLayout: layout };
    info = { solcVersion: "0.8.27", input: { sources: { "contracts/Example.sol": { content: source }, "contracts/Library.sol": { content: dependency } } },
      output: { contracts: { "contracts/Example.sol": { Example: output } }, sources: {
        "contracts/Example.sol": { ast: { nodes: [{ nodeType: "ImportDirective", absolutePath: "contracts/Library.sol" }] } },
        "contracts/Library.sol": { ast: { nodes: [] } },
      } } };
    artifact = { sourceName: "contracts/Example.sol", contractName: "Example", abi: [], bytecode: "0x6000", deployedBytecode: "0x6000" };
    write("contracts/Example.sol", source); write("contracts/Library.sol", dependency);
    write("artifacts/contracts/Example.sol/Example.dbg.json", { buildInfo: "../../build-info/fixture.json" });
    write("storage-layout/production/core/Example.json", layout);
    write("storage-layout/production/manifest.json", { files: { "core/Example.json": sha256(fs.readFileSync(path.join(root, "storage-layout/production/core/Example.json"))) } });
    persist();
  });
  afterEach(function () { fs.rmSync(root, { recursive: true, force: true }); });
  it("checks matching artifact, transitive inputs and frozen storage", function () {
    const result = verify(["contracts/Example.sol:Example"]);
    expect(result.status).to.equal("passed");
    expect(Object.keys(result.source_sha256)).to.have.length(2);
    expect(result.storage_layouts[0].passed).to.equal(true);
  });
  it("rejects imported source drift even when main source is unchanged", function () {
    write("contracts/Library.sol", "pragma solidity ^0.8.27; library Library { uint256 constant BAD = 1; }");
    expect(verify().errors.join(" ")).to.include("stale compiler input contracts/Library.sol");
  });
  it("rejects altered executable artifact and missing compilation inputs", function () {
    artifact.deployedBytecode = "0x6001"; persist();
    expect(verify().errors.join(" ")).to.include("deployedBytecode differs");
    artifact.deployedBytecode = "0x6000"; persist();
    write("contracts/Uncompiled.sol", "contract Uncompiled {}");
    expect(verify().errors.join(" ")).to.include("Source not covered");
  });
  it("rejects oversized runtime even if artifact matches compiler output", function () {
    artifact.deployedBytecode = "0x" + "00".repeat(24577);
    info.output.contracts["contracts/Example.sol"].Example.evm.deployedBytecode.object = artifact.deployedBytecode.slice(2); persist();
    expect(verify().errors.join(" ")).to.include("exceeds EIP-170");
  });
  it("rejects missing baseline and incompatible existing storage", function () {
    fs.unlinkSync(path.join(root, "storage-layout/production/core/Example.json"));
    expect(verify(["contracts/Example.sol:Example"]).errors.join(" ")).to.include("Required storage baseline missing");
    const layout = info.output.contracts["contracts/Example.sol"].Example.storageLayout;
    write("storage-layout/production/core/Example.json", layout);
    layout.storage[0].slot = "1"; persist();
    expect(verify(["contracts/Example.sol:Example"]).errors.join(" ")).to.include("moved or renamed balance");
  });
  it("rejects empty artifacts and an unexpected compiler", function () {
    info.solcVersion = "0.8.26"; persist();
    expect(verify().errors.join(" ")).to.include("Unexpected compiler");
    fs.rmSync(path.join(root, "artifacts"), { recursive: true });
    expect(verify().errors.join(" ")).to.include("No compiled artifacts");
  });
});
