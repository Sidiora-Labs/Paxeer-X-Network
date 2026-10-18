const { expect } = require("chai");
const { ethers } = require("hardhat");
const { time } = require("@nomicfoundation/hardhat-toolbox/network-helpers");

describe("GovernanceModule administrator mode", function () {
  const DELAY = 3600;
  const ADMIN_ROLE = ethers.id("ADMIN_ROLE");
  let admin, nextAdmin, outsider, gov, timelock, target;

  beforeEach(async function () {
    [admin, nextAdmin, outsider] = await ethers.getSigners();
    const Timelock = await ethers.getContractFactory("Timelock");
    timelock = await Timelock.deploy(DELAY, admin.address, admin.address);
    const Governance = await ethers.getContractFactory("GovernanceModule");
    const implementation = await Governance.deploy();
    const Proxy = await ethers.getContractFactory("UUPSProxy");
    const proxy = await Proxy.deploy(await implementation.getAddress(),
      Governance.interface.encodeFunctionData("initializeAdmin", [await timelock.getAddress(), admin.address]));
    gov = Governance.attach(await proxy.getAddress());
    await timelock.setProposer(await gov.getAddress());
    const Target = await ethers.getContractFactory("MockImplementation");
    target = await Target.deploy();
  });

  it("initializes without a voting token and cannot be initialized twice", async function () {
    expect(await gov.adminOnlyMode()).to.equal(true);
    expect(await gov.adminModeActive()).to.equal(true);
    expect(await gov.votingToken()).to.equal(ethers.ZeroAddress);
    expect(await gov.hasRole(ADMIN_ROLE, admin.address)).to.equal(true);
    await expect(gov.initializeAdmin(await timelock.getAddress(), admin.address))
      .to.be.revertedWithCustomError(gov, "AlreadyInitialized");
  });

  it("queues authorized decisions and enforces the Timelock delay", async function () {
    const destination = await target.getAddress();
    const data = target.interface.encodeFunctionData("setValue", [42]);
    const eta = (await time.latest()) + DELAY + 10;
    await expect(gov.connect(outsider).adminExecute(destination, 0, data, eta))
      .to.be.revertedWithCustomError(gov, "MissingRole");
    await gov.adminExecute(destination, 0, data, eta);
    expect(await target.getValue()).to.equal(0);
    await expect(timelock.executeTransaction(destination, 0, data, eta))
      .to.be.revertedWithCustomError(timelock, "DelayNotMet");
    await time.increaseTo(eta);
    await timelock.connect(outsider).executeTransaction(destination, 0, data, eta);
    expect(await target.getValue()).to.equal(42);
  });

  it("rejects token proposals and administrator-mode deactivation", async function () {
    await expect(gov.propose([await target.getAddress()], [0], ["0x"], "Disabled token proposal"))
      .to.be.revertedWithCustomError(gov, "AdminOnlyGovernance");
    await expect(gov.deactivateAdminMode()).to.be.revertedWithCustomError(gov, "AdminOnlyGovernance");
    expect(await gov.adminModeActive()).to.equal(true);
  });

  it("can rotate decision authority through delayed role changes", async function () {
    await gov.grantRole(ethers.ZeroHash, await timelock.getAddress());
    await gov.renounceRole(ethers.ZeroHash, admin.address);
    const destination = await gov.getAddress();
    const grant = gov.interface.encodeFunctionData("grantRole", [ADMIN_ROLE, nextAdmin.address]);
    let eta = (await time.latest()) + DELAY + 10;
    await gov.adminExecute(destination, 0, grant, eta);
    await time.increaseTo(eta);
    await timelock.executeTransaction(destination, 0, grant, eta);
    expect(await gov.hasRole(ADMIN_ROLE, nextAdmin.address)).to.equal(true);
    const revoke = gov.interface.encodeFunctionData("revokeRole", [ADMIN_ROLE, admin.address]);
    eta = (await time.latest()) + DELAY + 10;
    await gov.connect(nextAdmin).adminExecute(destination, 0, revoke, eta);
    await time.increaseTo(eta);
    await timelock.executeTransaction(destination, 0, revoke, eta);
    expect(await gov.hasRole(ADMIN_ROLE, admin.address)).to.equal(false);
  });
});
