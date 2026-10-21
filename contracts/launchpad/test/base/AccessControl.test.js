const { expect } = require("chai");
const { ethers } = require("hardhat");

describe("AccessControl", function () {
  let ac, deployer, alice, bob;
  const DEFAULT_ADMIN_ROLE = ethers.ZeroHash;
  let MINTER_ROLE, BURNER_ROLE;

  before(async function () {
    [deployer, alice, bob] = await ethers.getSigners();
    MINTER_ROLE = ethers.keccak256(ethers.toUtf8Bytes("MINTER_ROLE"));
    BURNER_ROLE = ethers.keccak256(ethers.toUtf8Bytes("BURNER_ROLE"));
  });

  beforeEach(async function () {
    const Mock = await ethers.getContractFactory("MockAccessControl");
    ac = await Mock.deploy();
    await ac.waitForDeployment();
  });

  describe("role management", function () {
    it("should grant DEFAULT_ADMIN_ROLE to deployer", async function () {
      expect(await ac.hasRole(DEFAULT_ADMIN_ROLE, deployer.address)).to.be.true;
    });

    it("should grant role by admin", async function () {
      await ac.grantRole(MINTER_ROLE, alice.address);
      expect(await ac.hasRole(MINTER_ROLE, alice.address)).to.be.true;
    });

    it("should emit RoleGranted event", async function () {
      await expect(ac.grantRole(MINTER_ROLE, alice.address))
        .to.emit(ac, "RoleGranted")
        .withArgs(MINTER_ROLE, alice.address, deployer.address);
    });

    it("should revert grant from non-admin", async function () {
      await expect(
        ac.connect(alice).grantRole(MINTER_ROLE, bob.address)
      ).to.be.revertedWithCustomError(ac, "MissingRole");
    });

    it("should revoke role by admin", async function () {
      await ac.grantRole(MINTER_ROLE, alice.address);
      await ac.revokeRole(MINTER_ROLE, alice.address);
      expect(await ac.hasRole(MINTER_ROLE, alice.address)).to.be.false;
    });

    it("should emit RoleRevoked event", async function () {
      await ac.grantRole(MINTER_ROLE, alice.address);
      await expect(ac.revokeRole(MINTER_ROLE, alice.address))
        .to.emit(ac, "RoleRevoked")
        .withArgs(MINTER_ROLE, alice.address, deployer.address);
    });

    it("should revert revoke from non-admin", async function () {
      await ac.grantRole(MINTER_ROLE, alice.address);
      await expect(
        ac.connect(alice).revokeRole(MINTER_ROLE, alice.address)
      ).to.be.revertedWithCustomError(ac, "MissingRole");
    });
  });

  describe("renounceRole", function () {
    it("should allow account to renounce its own role", async function () {
      await ac.grantRole(MINTER_ROLE, alice.address);
      await ac.connect(alice).renounceRole(MINTER_ROLE, alice.address);
      expect(await ac.hasRole(MINTER_ROLE, alice.address)).to.be.false;
    });

    it("should revert if confirmation address does not match caller", async function () {
      await ac.grantRole(MINTER_ROLE, alice.address);
      await expect(
        ac.connect(alice).renounceRole(MINTER_ROLE, bob.address)
      ).to.be.revertedWithCustomError(ac, "MissingRole");
    });
  });

  describe("role admin hierarchy", function () {
    it("should default admin role to DEFAULT_ADMIN_ROLE", async function () {
      expect(await ac.getRoleAdmin(MINTER_ROLE)).to.equal(DEFAULT_ADMIN_ROLE);
    });

    it("should allow setting custom role admin", async function () {
      await ac.setupMinterAdmin(BURNER_ROLE);
      expect(await ac.getRoleAdmin(MINTER_ROLE)).to.equal(BURNER_ROLE);
    });
  });

  describe("onlyRole modifier", function () {
    it("should allow access for role holder", async function () {
      await ac.grantRole(MINTER_ROLE, alice.address);
      expect(await ac.connect(alice).protectedFunction()).to.be.true;
    });

    it("should revert for non-role holder", async function () {
      await expect(
        ac.connect(alice).protectedFunction()
      ).to.be.revertedWithCustomError(ac, "MissingRole");
    });
  });
});
