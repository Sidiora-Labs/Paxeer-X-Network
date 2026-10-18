const { expect } = require("chai");
const { compareLayouts, readRequiredBaseline } = require("../../scripts/lib/storage-layout");
describe("OPS recursive storage gate", function () {
  const base = () => ({ storage: [{ label: "items", slot: "0", offset: 0, type: "map" }], types: {
    map: { encoding: "mapping", label: "mapping(uint256 => struct Item)", numberOfBytes: "32", key: "u", value: "s" },
    u: { encoding: "inplace", label: "uint256", numberOfBytes: "32" },
    s: { encoding: "inplace", label: "struct Item", numberOfBytes: "64", members: [{ label: "a", slot: "0", offset: 0, type: "u" }, { label: "b", slot: "1", offset: 0, type: "u" }] }
  } });
  it("rejects absent pinned baseline", () => expect(() => readRequiredBaseline("/tmp/ops-intentionally-absent-baseline.json")).to.throw("baseline missing"));
  it("accepts equivalent graphs", () => expect(compareLayouts(base(), base())).to.deep.equal([]));
  it("rejects nested member reordering", () => { const c = base(); c.types.s.members.reverse(); expect(compareLayouts(base(), c)).not.to.be.empty; });
  it("rejects missing type metadata", () => { const c = base(); delete c.types.u; expect(compareLayouts(base(), c)).not.to.be.empty; });
  it("rejects encoding and width changes", () => { for (const key of ["encoding", "numberOfBytes"]) { const c = base(); c.types.u[key] = "changed"; expect(compareLayouts(base(), c)).not.to.be.empty; } });
  it("rejects missing layout", () => expect(compareLayouts({}, base())).not.to.be.empty);
});
