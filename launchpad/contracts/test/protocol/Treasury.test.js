const { expect } = require("chai");
const { ethers } = require("hardhat");

describe("Treasury", function () {
  let treasury, usdl, eventEmitter;
  let deployer, alice, depositor;
  const DEPOSITOR_ROLE = ethers.keccak256(ethers.toUtf8Bytes("DEPOSITOR_ROLE"));

  before(async function () {
    [deployer, alice, depositor] = await ethers.getSigners();
  });

  beforeEach(async function () {
    // Deploy mock USDL
    const MockERC20 = await ethers.getContractFactory("MockERC20");
    usdl = await MockERC20.deploy("USD Ledger", "USDL", 6);
    await usdl.waitForDeployment();

    // Deploy mock EventEmitter
    const MockEE = await ethers.getContractFactory("MockEventEmitter");
    eventEmitter = await MockEE.deploy();
    await eventEmitter.waitForDeployment();

    // Deploy Treasury via UUPS proxy
    const Treasury = await ethers.getContractFactory("Treasury");
    const impl = await Treasury.deploy();
    await impl.waitForDeployment();

    const initData = Treasury.interface.encodeFunctionData("initialize", [
      await eventEmitter.getAddress(),
      deployer.address,
    ]);
    const Proxy = await ethers.getContractFactory("UUPSProxy");
    const proxy = await Proxy.deploy(await impl.getAddress(), initData);
    await proxy.waitForDeployment();

    treasury = Treasury.attach(await proxy.getAddress());

    // Grant depositor role
    await treasury.grantRole(DEPOSITOR_ROLE, depositor.address);

    // Fund depositor with USDL
    await usdl.mint(depositor.address, ethers.parseUnits("10000", 6));
    await usdl.connect(depositor).approve(await treasury.getAddress(), ethers.MaxUint256);
  });

  describe("initialization", function () {
    it("should set admin role to deployer", async function () {
      expect(await treasury.hasRole(ethers.ZeroHash, deployer.address)).to.be.true;
    });

    it("should revert on double init", async function () {
      await expect(
        treasury.initialize(await eventEmitter.getAddress(), deployer.address)
      ).to.be.revertedWithCustomError(treasury, "AlreadyInitialized");
    });
  });

  describe("deposit", function () {
    it("should accept deposit from authorized depositor", async function () {
      await treasury.connect(depositor).deposit(await usdl.getAddress(), ethers.parseUnits("500", 6));
      expect(await treasury.getBalance(await usdl.getAddress())).to.equal(ethers.parseUnits("500", 6));
    });

    it("should emit Deposited event", async function () {
      await expect(
        treasury.connect(depositor).deposit(await usdl.getAddress(), ethers.parseUnits("500", 6))
      ).to.emit(treasury, "Deposited")
        .withArgs(await usdl.getAddress(), depositor.address, ethers.parseUnits("500", 6));
    });

    it("should revert deposit from unauthorized address", async function () {
      await usdl.mint(alice.address, ethers.parseUnits("1000", 6));
      await usdl.connect(alice).approve(await treasury.getAddress(), ethers.MaxUint256);
      await expect(
        treasury.connect(alice).deposit(await usdl.getAddress(), ethers.parseUnits("100", 6))
      ).to.be.revertedWithCustomError(treasury, "MissingRole");
    });

    it("should revert deposit of zero amount", async function () {
      await expect(
        treasury.connect(depositor).deposit(await usdl.getAddress(), 0)
      ).to.be.revertedWithCustomError(treasury, "ZeroAmount");
    });

    it("should track multiple deposits", async function () {
      await treasury.connect(depositor).deposit(await usdl.getAddress(), ethers.parseUnits("300", 6));
      await treasury.connect(depositor).deposit(await usdl.getAddress(), ethers.parseUnits("200", 6));
      expect(await treasury.getBalance(await usdl.getAddress())).to.equal(ethers.parseUnits("500", 6));
    });
  });

  describe("withdraw", function () {
    beforeEach(async function () {
      await treasury.connect(depositor).deposit(await usdl.getAddress(), ethers.parseUnits("1000", 6));
    });

    it("should allow admin to withdraw", async function () {
      await treasury.withdraw(await usdl.getAddress(), alice.address, ethers.parseUnits("400", 6));
      expect(await usdl.balanceOf(alice.address)).to.equal(ethers.parseUnits("400", 6));
      expect(await treasury.getBalance(await usdl.getAddress())).to.equal(ethers.parseUnits("600", 6));
    });

    it("should emit Withdrawn event", async function () {
      await expect(
        treasury.withdraw(await usdl.getAddress(), alice.address, ethers.parseUnits("400", 6))
      ).to.emit(treasury, "Withdrawn")
        .withArgs(await usdl.getAddress(), alice.address, ethers.parseUnits("400", 6));
    });

    it("should revert withdraw from non-admin", async function () {
      await expect(
        treasury.connect(alice).withdraw(await usdl.getAddress(), alice.address, ethers.parseUnits("100", 6))
      ).to.be.revertedWithCustomError(treasury, "MissingRole");
    });

    it("should revert withdraw exceeding balance", async function () {
      await expect(
        treasury.withdraw(await usdl.getAddress(), alice.address, ethers.parseUnits("2000", 6))
      ).to.be.revertedWithCustomError(treasury, "InsufficientBalance");
    });

    it("should revert withdraw to zero address", async function () {
      await expect(
        treasury.withdraw(await usdl.getAddress(), ethers.ZeroAddress, ethers.parseUnits("100", 6))
      ).to.be.revertedWithCustomError(treasury, "ZeroAddress");
    });

    it("should revert withdraw of zero amount", async function () {
      await expect(
        treasury.withdraw(await usdl.getAddress(), alice.address, 0)
      ).to.be.revertedWithCustomError(treasury, "ZeroAmount");
    });
  });

  describe("audit accounting regressions", function () {
    it("credits actual received amount for taxed deposits", async function () {
      const TaxToken = await ethers.getContractFactory("TreasuryTaxToken");
      const taxed = await TaxToken.deploy();
      await taxed.mint(depositor.address, 100);
      await taxed.connect(depositor).approve(await treasury.getAddress(), 100);
      await treasury.connect(depositor).deposit(await taxed.getAddress(), 100);
      expect(await treasury.getBalance(await taxed.getAddress())).to.equal(90);
      expect(await taxed.balanceOf(await treasury.getAddress())).to.equal(90);
    });
    it("records transferred creation fees only up to actual backing", async function () {
      const token = await usdl.getAddress();
      await usdl.connect(depositor).transfer(await treasury.getAddress(), 100);
      await treasury.connect(depositor).recordDeposit(token, 100);
      expect(await treasury.getBalance(token)).to.equal(100);
      await expect(treasury.connect(depositor).recordDeposit(token, 1)).to.be.revertedWithCustomError(treasury, "InsufficientBalance");
      await treasury.withdraw(token, alice.address, 100);
      expect(await usdl.balanceOf(alice.address)).to.equal(100);
    });
    it("recovers only unaccounted surplus", async function () {
      const token = await usdl.getAddress();
      await treasury.connect(depositor).deposit(token, 100);
      await usdl.connect(depositor).transfer(await treasury.getAddress(), 20);
      await expect(treasury.recoverSurplus(token, alice.address, 21)).to.be.revertedWithCustomError(treasury, "InsufficientBalance");
      await treasury.recoverSurplus(token, alice.address, 20);
      expect(await treasury.getBalance(token)).to.equal(100);
      expect(await usdl.balanceOf(await treasury.getAddress())).to.equal(100);
    });
    it("rejects nonexistent token contracts and unauthorized accounting", async function () {
      await expect(treasury.connect(depositor).deposit(alice.address, 100)).to.be.revertedWithCustomError(treasury, "InvalidToken");
      await expect(treasury.connect(alice).recordDeposit(await usdl.getAddress(), 100)).to.be.revertedWithCustomError(treasury, "MissingRole");
    });
  });

  describe("UUPS upgrade", function () {
    it("should upgrade by admin", async function () {
      const V2 = await ethers.getContractFactory("Treasury");
      const implV2 = await V2.deploy();
      await treasury.upgradeToAndCall(await implV2.getAddress(), "0x");
    });

    it("should revert upgrade by non-admin", async function () {
      const V2 = await ethers.getContractFactory("Treasury");
      const implV2 = await V2.deploy();
      await expect(
        treasury.connect(alice).upgradeToAndCall(await implV2.getAddress(), "0x")
      ).to.be.revertedWithCustomError(treasury, "MissingRole");
    });
  });
});
