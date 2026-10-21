const { expect } = require("chai");
const { ethers, network } = require("hardhat");

describe("Production optical identity review", function () {
  let admin, outsider, registry, optical;
  const digest = ethers.id("independently-reviewed-optical-release");
  async function deployRegistry(artifact) {
    const Factory = artifact ? new ethers.ContractFactory(artifact.abi, artifact.bytecode, admin)
      : await ethers.getContractFactory("OpticalRegistry");
    const impl = await Factory.deploy();
    const Proxy = await ethers.getContractFactory("UUPSProxy");
    const proxy = await Proxy.deploy(await impl.getAddress(), Factory.interface.encodeFunctionData("initialize", [ethers.ZeroAddress, admin.address]));
    return Factory.attach(await proxy.getAddress());
  }
  async function register() {
    return registry.registerOptical(await optical.getAddress(), "Plugin", "Initial review", 2, "Reviewer");
  }
  async function renew(flags = 1, evidence = digest, hash) {
    const address = await optical.getAddress();
    return registry.renewReview(address, hash || ethers.keccak256(await ethers.provider.getCode(address)), flags, evidence);
  }
  beforeEach(async function () {
    [admin, outsider] = await ethers.getSigners();
    registry = await deployRegistry();
    optical = await (await ethers.getContractFactory("MockOpticalWithFlags")).deploy(ethers.ZeroAddress, admin.address, 1);
  });
  it("records explicit initial review and separately renews evidence", async function () {
    await expect(register()).to.emit(registry, "OpticalIdentityReviewed");
    expect(await registry.isApproved(await optical.getAddress())).to.equal(true);
    expect((await registry.identityReviews(await optical.getAddress())).evidenceDigest).to.equal(ethers.ZeroHash);
    await expect(renew()).to.emit(registry, "OpticalIdentityReviewed");
    expect((await registry.identityReviews(await optical.getAddress())).evidenceDigest).to.equal(digest);
  });
  it("metadata editing cannot approve replaced executable code", async function () {
    await register();
    const address = await optical.getAddress();
    const oldHash = await registry.approvedCodeHash(address);
    const code = await ethers.provider.getCode(address);
    await network.provider.send("hardhat_setCode", [address, code + "00"]);
    expect(await registry.isRegistered(address)).to.equal(false);
    await registry.updateMetadata(address, "Editorial", "Changed wording", 3, "Reviewer");
    expect(await registry.approvedCodeHash(address)).to.equal(oldHash);
    expect(await registry.isApproved(address)).to.equal(false);
    await renew();
    expect(await registry.isApproved(address)).to.equal(true);
  });
  it("invalidates changed flags and requires explicit matching review renewal", async function () {
    await register();
    await optical.setFlags(2);
    expect(await registry.isApproved(await optical.getAddress())).to.equal(false);
    await registry.updateMetadata(await optical.getAddress(), "New label", "Text", 2, "Reviewer");
    expect(await registry.isApproved(await optical.getAddress())).to.equal(false);
    await expect(renew(1)).to.be.revertedWithCustomError(registry, "ReviewIdentityMismatch");
    await renew(2);
    expect(await registry.isApproved(await optical.getAddress())).to.equal(true);
    await optical.setFlags(255);
    expect(await registry.isRegistered(await optical.getAddress())).to.equal(false);
    await expect(renew(255)).to.be.revertedWithCustomError(registry, "ReviewIdentityMismatch");
  });
  it("rejects unauthorized, stale, zero-evidence and revoked review renewal", async function () {
    await register();
    const address = await optical.getAddress();
    const hash = await registry.approvedCodeHash(address);
    await expect(registry.connect(outsider).renewReview(address, hash, 1, digest)).to.be.revertedWithCustomError(registry, "MissingRole");
    await expect(renew(1, ethers.ZeroHash)).to.be.revertedWithCustomError(registry, "InvalidOptical");
    await expect(renew(1, digest, ethers.id("stale-code"))).to.be.revertedWithCustomError(registry, "ReviewIdentityMismatch");
    await registry.deregisterOptical(address);
    await expect(renew()).to.be.revertedWithCustomError(registry, "NotRegistered");
    expect(await registry.isApproved(address)).to.equal(false);
  });
  it("bounds reverting, malformed, huge-return and gas-exhausting flags probes", async function () {
    const address = await optical.getAddress();
    for (const code of ["0x60006000fd", "0x600160005360016000f3", "0x600060005260406000f3", "0x62ffffff6000f3", "0x5b600056"]) {
      await network.provider.send("hardhat_setCode", [address, code]);
      await expect(register()).to.be.revertedWithCustomError(registry, "InvalidOptical");
      expect(await registry.isApproved(address)).to.equal(false);
    }
  });
  it("returns false for malformed flags without any runtime identity change", async function () {
    const address = await optical.getAddress();
    // Fixed runtime returns slot 0 using the return length in slot 1.
    await network.provider.send("hardhat_setCode", [address, "0x6000546000526001546000f3"]);
    await network.provider.send("hardhat_setStorageAt", [address, "0x0", ethers.toBeHex(1, 32)]);
    await network.provider.send("hardhat_setStorageAt", [address, "0x1", ethers.toBeHex(32, 32)]);
    await register();
    const reviewedHash = await registry.approvedCodeHash(address);
    expect(await registry.isApproved(address)).to.equal(true);
    for (const length of [0, 1, 64, 16777215]) {
      await network.provider.send("hardhat_setStorageAt", [address, "0x1", ethers.toBeHex(length, 32)]);
      expect(ethers.keccak256(await ethers.provider.getCode(address))).to.equal(reviewedHash);
      expect(await registry.isApproved(address)).to.equal(false);
    }
    await network.provider.send("hardhat_setStorageAt", [address, "0x1", ethers.toBeHex(32, 32)]);
    expect(await registry.isApproved(address)).to.equal(true);
  });
  it("records the exact validated flags when a hostile getter varies across calls in one transaction", async function () {
    const address = await optical.getAddress();
    // Measure the cost of SLOAD: cold first probe returns 1; warm second probe returns 2.
    // Static calls cannot write storage, but EIP-2929 warmth still spans successful calls.
    const code = "0x5a600054505a90036103e890111560010160005260206000f3";
    await network.provider.send("hardhat_setCode", [address, code]);
    const hash = ethers.keccak256(code);
    await register();
    expect((await registry.identityReviews(address)).flags).to.equal(1);
    const tx = await renew(1, digest, hash);
    const receipt = await tx.wait();
    const reviewEvent = receipt.logs.map((log) => {
      try { return registry.interface.parseLog(log); } catch { return null; }
    }).find((log) => log && log.name === "OpticalIdentityReviewed");
    expect(reviewEvent.args.flags).to.equal(1);
    expect((await registry.identityReviews(address)).flags).to.equal(1);
  });
  it("preserves populated original registry state while requiring explicit strengthened review", async function () {
    const artifact = require("./baselines/OpticalRegistry.json");
    registry = await deployRegistry(artifact);
    await register();
    const address = await optical.getAddress();
    const originalMeta = await registry.getOpticalMetadata(address);
    const originalHash = await registry.approvedCodeHash(address);
    const next = await (await ethers.getContractFactory("OpticalRegistry")).deploy();
    await registry.upgradeToAndCall(await next.getAddress(), "0x");
    registry = await ethers.getContractAt("OpticalRegistry", await registry.getAddress());
    expect(await registry.getOpticalMetadata(address)).to.deep.equal(originalMeta);
    expect(await registry.approvedCodeHash(address)).to.equal(originalHash);
    expect(await registry.getAllOpticals(0, 10)).to.deep.equal([address]);
    expect(await registry.hasRole(ethers.ZeroHash, admin.address)).to.equal(true);
    expect(await registry.isApproved(address)).to.equal(false);
    await registry.updateMetadata(address, "Updated", "Editorial only", 2, "Reviewer");
    expect(await registry.isApproved(address)).to.equal(false);
    await renew();
    expect(await registry.isApproved(address)).to.equal(true);
  });
});
