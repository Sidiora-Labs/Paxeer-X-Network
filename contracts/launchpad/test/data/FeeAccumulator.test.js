const { poolSigner: impersonatePool } = require("../opticals/fixture");
const { expect } = require("chai");
const { ethers } = require("hardhat");
const { PROTOCOL_FEE_BPS, DEAD_ADDRESS, BPS_DENOMINATOR } = require("../helpers/constants");

describe("FeeAccumulator", function () {
  let accumulator, accumulatorProxy;
  let configProxy, treasuryProxy, registryProxy, eventEmitter, usdl, token;
  let deployer, alice, bob, poolSigner, feesRouterSigner;

  before(async function () {
    [deployer, alice, bob, poolSigner, feesRouterSigner] = await ethers.getSigners();
  });

  beforeEach(async function () {
    // Deploy mock USDL
    const MockERC20 = await ethers.getContractFactory("MockERC20");
    usdl = await MockERC20.deploy("USD Ledger", "USDL", 6);
    await usdl.waitForDeployment();

    // Deploy a mock token (represents the pool's SidioraERC20)
    token = await (await ethers.getContractFactory("MockERC20Base")).deploy("Test Token", "TEST", 18);
    await token.waitForDeployment();

    // Deploy mock EventEmitter
    const MockEE = await ethers.getContractFactory("MockEventEmitter");
    eventEmitter = await MockEE.deploy();
    await eventEmitter.waitForDeployment();

    // Deploy ProtocolConfig (behind proxy)
    const Config = await ethers.getContractFactory("ProtocolConfig");
    const configImpl = await Config.deploy();
    await configImpl.waitForDeployment();
    const configInitData = Config.interface.encodeFunctionData("initialize", [
      await usdl.getAddress(),
      await eventEmitter.getAddress(),
      deployer.address,
    ]);
    const Proxy = await ethers.getContractFactory("UUPSProxy");
    let proxy = await Proxy.deploy(await configImpl.getAddress(), configInitData);
    await proxy.waitForDeployment();
    configProxy = Config.attach(await proxy.getAddress());

    // Deploy Treasury (behind proxy)
    const Treasury = await ethers.getContractFactory("Treasury");
    const treasuryImpl = await Treasury.deploy();
    await treasuryImpl.waitForDeployment();
    const treasuryInitData = Treasury.interface.encodeFunctionData("initialize", [
      await eventEmitter.getAddress(),
      deployer.address,
    ]);
    proxy = await Proxy.deploy(await treasuryImpl.getAddress(), treasuryInitData);
    await proxy.waitForDeployment();
    treasuryProxy = Treasury.attach(await proxy.getAddress());

    // Deploy PoolRegistry (behind proxy)
    const Registry = await ethers.getContractFactory("PoolRegistry");
    const registryImpl = await Registry.deploy();
    await registryImpl.waitForDeployment();
    const registryInitData = Registry.interface.encodeFunctionData("initialize", [
      await eventEmitter.getAddress(),
      deployer.address,
    ]);
    proxy = await Proxy.deploy(await registryImpl.getAddress(), registryInitData);
    await proxy.waitForDeployment();
    registryProxy = Registry.attach(await proxy.getAddress());

    // Deploy FeeAccumulator (behind proxy)
    const Acc = await ethers.getContractFactory("FeeAccumulator");
    const accImpl = await Acc.deploy();
    await accImpl.waitForDeployment();
    const accInitData = Acc.interface.encodeFunctionData("initialize", [
      await configProxy.getAddress(),
      await treasuryProxy.getAddress(),
      await registryProxy.getAddress(),
      await eventEmitter.getAddress(),
      await usdl.getAddress(),
      deployer.address,
    ]);
    proxy = await Proxy.deploy(await accImpl.getAddress(), accInitData);
    await proxy.waitForDeployment();
    accumulatorProxy = Acc.attach(await proxy.getAddress());

    const pool = await (await ethers.getContractFactory("MockPoolForOptical")).deploy();
    await pool.setTokenAddress(await token.getAddress());
    await pool.setFeeAccumulator(await accumulatorProxy.getAddress());
    poolSigner = await impersonatePool(pool);

    // Grant roles
    const POOL_ROLE = ethers.keccak256(ethers.toUtf8Bytes("POOL_ROLE"));
    const FEES_ROUTER_ROLE = ethers.keccak256(ethers.toUtf8Bytes("FEES_ROUTER_ROLE"));
    const DEPOSITOR_ROLE = ethers.keccak256(ethers.toUtf8Bytes("DEPOSITOR_ROLE"));
    const FACTORY_ROLE = ethers.keccak256(ethers.toUtf8Bytes("FACTORY_ROLE"));

    await accumulatorProxy.grantRole(POOL_ROLE, poolSigner.address);
    await accumulatorProxy.grantRole(FEES_ROUTER_ROLE, feesRouterSigner.address);

    // Grant DEPOSITOR_ROLE to FeeAccumulator on Treasury (so it can deposit protocol fees)
    await treasuryProxy.grantRole(DEPOSITOR_ROLE, await accumulatorProxy.getAddress());

    // Register a pool in the registry for airdrop tests
    await registryProxy.grantRole(FACTORY_ROLE, deployer.address);
    await registryProxy.register(
      poolSigner.address, await token.getAddress(), alice.address, ethers.ZeroAddress, 1
    );

    // Mint USDL to pool signer (simulates USDL being sent to FeeAccumulator for fee recording)
    await usdl.mint(poolSigner.address, ethers.parseUnits("100000", 6));
    // Pool signer approves FeeAccumulator to pull USDL
    await usdl.connect(poolSigner).approve(await accumulatorProxy.getAddress(), ethers.MaxUint256);
  });

  async function recordFeeFromPool(amount) {
    // Transfer USDL to FeeAccumulator first (simulating pool sending fees)
    await usdl.connect(poolSigner).transfer(await accumulatorProxy.getAddress(), amount);
    await accumulatorProxy.connect(poolSigner).recordFee(poolSigner.address, amount);
  }

  describe("initialization", function () {
    it("should set admin role", async function () {
      expect(await accumulatorProxy.hasRole(ethers.ZeroHash, deployer.address)).to.be.true;
    });

    it("should revert on double initialization", async function () {
      await expect(
        accumulatorProxy.initialize(
          await configProxy.getAddress(),
          await treasuryProxy.getAddress(),
          await registryProxy.getAddress(),
          await eventEmitter.getAddress(),
          await usdl.getAddress(),
          deployer.address,
        )
      ).to.be.revertedWithCustomError(accumulatorProxy, "AlreadyInitialized");
    });

    it("should store correct addresses", async function () {
      expect(await accumulatorProxy.protocolConfig()).to.equal(await configProxy.getAddress());
      expect(await accumulatorProxy.treasury()).to.equal(await treasuryProxy.getAddress());
      expect(await accumulatorProxy.poolRegistry()).to.equal(await registryProxy.getAddress());
      expect(await accumulatorProxy.usdlAddress()).to.equal(await usdl.getAddress());
    });
  });

  describe("recordFee", function () {
    it("should record fee and split protocol/pool portions", async function () {
      const feeAmount = ethers.parseUnits("10", 6);
      await recordFeeFromPool(feeAmount);

      // protocolFeeBps = 1000 (10%), so protocolCut = 1e18, poolCut = 9e18
      const expectedProtocolCut = (feeAmount * PROTOCOL_FEE_BPS) / BPS_DENOMINATOR;
      const expectedPoolCut = feeAmount - expectedProtocolCut;

      expect(await accumulatorProxy.getAccumulatedFees(poolSigner.address)).to.equal(expectedPoolCut);
      expect(await accumulatorProxy.getProtocolFeesPending()).to.equal(0);
    });

    it("should deposit protocol cut to treasury", async function () {
      const feeAmount = ethers.parseUnits("10", 6);
      await recordFeeFromPool(feeAmount);

      const expectedProtocolCut = (feeAmount * PROTOCOL_FEE_BPS) / BPS_DENOMINATOR;
      expect(await treasuryProxy.getBalance(await usdl.getAddress())).to.equal(expectedProtocolCut);
    });

    it("should emit FeeRecorded event", async function () {
      const feeAmount = ethers.parseUnits("10", 6);
      // Transfer first
      await usdl.connect(poolSigner).transfer(await accumulatorProxy.getAddress(), feeAmount);

      await expect(
        accumulatorProxy.connect(poolSigner).recordFee(poolSigner.address, feeAmount)
      ).to.emit(accumulatorProxy, "FeeRecorded");
    });

    it("should revert from non-pool caller", async function () {
      await expect(
        accumulatorProxy.connect(alice).recordFee(poolSigner.address, ethers.parseUnits("10", 6))
      ).to.be.revertedWithCustomError(accumulatorProxy, "MissingRole");
    });

    it("should revert with zero fee amount", async function () {
      await expect(
        accumulatorProxy.connect(poolSigner).recordFee(poolSigner.address, 0)
      ).to.be.revertedWithCustomError(accumulatorProxy, "ZeroAmount");
    });

    it("should accumulate fees over multiple recordings", async function () {
      await recordFeeFromPool(ethers.parseUnits("10", 6));
      await recordFeeFromPool(ethers.parseUnits("5", 6));

      const expectedPoolCut1 = ethers.parseUnits("10", 6) - (ethers.parseUnits("10", 6) * PROTOCOL_FEE_BPS) / BPS_DENOMINATOR;
      const expectedPoolCut2 = ethers.parseUnits("5", 6) - (ethers.parseUnits("5", 6) * PROTOCOL_FEE_BPS) / BPS_DENOMINATOR;

      expect(await accumulatorProxy.getAccumulatedFees(poolSigner.address))
        .to.equal(expectedPoolCut1 + expectedPoolCut2);
    });
  });

  describe("claim (CLAIM strategy)", function () {
    it("should transfer accumulated fees to recipient", async function () {
      await recordFeeFromPool(ethers.parseUnits("10", 6));

      const accumulated = await accumulatorProxy.getAccumulatedFees(poolSigner.address);
      const balBefore = await usdl.balanceOf(alice.address);

      await accumulatorProxy.connect(feesRouterSigner).claim(poolSigner.address, alice.address);

      const balAfter = await usdl.balanceOf(alice.address);
      expect(balAfter - balBefore).to.equal(accumulated);
    });

    it("should reset accumulated fees to zero after claim", async function () {
      await recordFeeFromPool(ethers.parseUnits("10", 6));
      await accumulatorProxy.connect(feesRouterSigner).claim(poolSigner.address, alice.address);
      expect(await accumulatorProxy.getAccumulatedFees(poolSigner.address)).to.equal(0);
    });

    it("should emit FeesClaimed event", async function () {
      await recordFeeFromPool(ethers.parseUnits("10", 6));
      await expect(
        accumulatorProxy.connect(feesRouterSigner).claim(poolSigner.address, alice.address)
      ).to.emit(accumulatorProxy, "FeesClaimed");
    });

    it("should revert when no fees accumulated", async function () {
      await expect(
        accumulatorProxy.connect(feesRouterSigner).claim(poolSigner.address, alice.address)
      ).to.be.revertedWithCustomError(accumulatorProxy, "NoFeesAccumulated");
    });

    it("should revert from non-FeesRouter caller", async function () {
      await recordFeeFromPool(ethers.parseUnits("10", 6));
      await expect(
        accumulatorProxy.connect(alice).claim(poolSigner.address, alice.address)
      ).to.be.revertedWithCustomError(accumulatorProxy, "MissingRole");
    });

    it("should revert with zero recipient address", async function () {
      await recordFeeFromPool(ethers.parseUnits("10", 6));
      await expect(
        accumulatorProxy.connect(feesRouterSigner).claim(poolSigner.address, ethers.ZeroAddress)
      ).to.be.revertedWithCustomError(accumulatorProxy, "ZeroAddress");
    });
  });

  describe("burn (BURN strategy)", function () {
    it("should send accumulated fees to DEAD address", async function () {
      await recordFeeFromPool(ethers.parseUnits("10", 6));
      const accumulated = await accumulatorProxy.getAccumulatedFees(poolSigner.address);
      const deadBalBefore = await usdl.balanceOf(DEAD_ADDRESS);

      await accumulatorProxy.connect(feesRouterSigner).burn(poolSigner.address);

      const deadBalAfter = await usdl.balanceOf(DEAD_ADDRESS);
      expect(deadBalAfter - deadBalBefore).to.equal(accumulated);
    });

    it("should reset accumulated fees to zero", async function () {
      await recordFeeFromPool(ethers.parseUnits("10", 6));
      await accumulatorProxy.connect(feesRouterSigner).burn(poolSigner.address);
      expect(await accumulatorProxy.getAccumulatedFees(poolSigner.address)).to.equal(0);
    });

    it("should emit FeesBurned event", async function () {
      await recordFeeFromPool(ethers.parseUnits("10", 6));
      await expect(
        accumulatorProxy.connect(feesRouterSigner).burn(poolSigner.address)
      ).to.emit(accumulatorProxy, "FeesBurned");
    });

    it("should revert when no fees accumulated", async function () {
      await expect(
        accumulatorProxy.connect(feesRouterSigner).burn(poolSigner.address)
      ).to.be.revertedWithCustomError(accumulatorProxy, "NoFeesAccumulated");
    });
  });

  describe("triggerAirdrop (AIRDROP strategy)", function () {
    beforeEach(async function () { await token.mint(alice.address, 1000); });
    it("should snapshot amount and increment epoch", async function () {
      await recordFeeFromPool(ethers.parseUnits("10", 6));
      const accumulated = await accumulatorProxy.getAccumulatedFees(poolSigner.address);

      await accumulatorProxy.connect(feesRouterSigner).triggerAirdrop(poolSigner.address);

      expect(await accumulatorProxy.getAirdropEpoch(poolSigner.address)).to.equal(1);
      expect(await accumulatorProxy.getAirdropBalance(poolSigner.address)).to.equal(accumulated);
      expect(await accumulatorProxy.getAccumulatedFees(poolSigner.address)).to.equal(0);
    });

    it("should emit AirdropTriggered event", async function () {
      await recordFeeFromPool(ethers.parseUnits("10", 6));
      await expect(
        accumulatorProxy.connect(feesRouterSigner).triggerAirdrop(poolSigner.address)
      ).to.emit(accumulatorProxy, "AirdropTriggered");
    });

    it("should revert when no fees accumulated", async function () {
      await expect(
        accumulatorProxy.connect(feesRouterSigner).triggerAirdrop(poolSigner.address)
      ).to.be.revertedWithCustomError(accumulatorProxy, "NoFeesAccumulated");
    });
  });

  describe("claimAirdrop", function () {
    beforeEach(async function () {
      // Mint tokens to holders: Alice 60%, Bob 40%
      const totalSupply = ethers.parseUnits("1000", 6);
      await token.mint(alice.address, ethers.parseUnits("600", 6));
      await token.mint(bob.address, ethers.parseUnits("400", 6));

      // Record and trigger airdrop
      await recordFeeFromPool(ethers.parseUnits("10", 6));
      await accumulatorProxy.connect(feesRouterSigner).triggerAirdrop(poolSigner.address);
    });

    it("should distribute proportional share to holder", async function () {
      const airdropBalance = await accumulatorProxy.getAirdropBalance(poolSigner.address);
      const balBefore = await usdl.balanceOf(alice.address);

      await accumulatorProxy.claimAirdrop(poolSigner.address, alice.address);

      const balAfter = await usdl.balanceOf(alice.address);
      // Alice has 60% of tokens, should get 60% of airdrop
      const expectedShare = (airdropBalance * 600n) / 1000n;
      expect(balAfter - balBefore).to.equal(expectedShare);
    });

    it("should mark holder as claimed for epoch", async function () {
      await accumulatorProxy.claimAirdrop(poolSigner.address, alice.address);
      expect(await accumulatorProxy.hasClaimedAirdrop(poolSigner.address, alice.address, 1)).to.be.true;
    });

    it("should revert on double claim", async function () {
      await accumulatorProxy.claimAirdrop(poolSigner.address, alice.address);
      await expect(
        accumulatorProxy.claimAirdrop(poolSigner.address, alice.address)
      ).to.be.revertedWithCustomError(accumulatorProxy, "AlreadyClaimed");
    });

    it("should allow multiple holders to claim", async function () {
      await accumulatorProxy.claimAirdrop(poolSigner.address, alice.address);
      await accumulatorProxy.claimAirdrop(poolSigner.address, bob.address);

      expect(await accumulatorProxy.hasClaimedAirdrop(poolSigner.address, alice.address, 1)).to.be.true;
      expect(await accumulatorProxy.hasClaimedAirdrop(poolSigner.address, bob.address, 1)).to.be.true;
    });

    it("should emit AirdropClaimed event", async function () {
      await expect(
        accumulatorProxy.claimAirdrop(poolSigner.address, alice.address)
      ).to.emit(accumulatorProxy, "AirdropClaimed");
    });

    it("should revert when no airdrop triggered", async function () {
      // Use bob as a different pool address with no airdrop
      await expect(
        accumulatorProxy.claimAirdrop(bob.address, alice.address)
      ).to.be.revertedWithCustomError(accumulatorProxy, "AirdropNotTriggered");
    });
  });

  describe("sendLpRewards (LP_REWARDS strategy)", function () {
    it("should transfer accumulated fees to pool address", async function () {
      await recordFeeFromPool(ethers.parseUnits("10", 6));
      const accumulated = await accumulatorProxy.getAccumulatedFees(poolSigner.address);
      const poolBalBefore = await usdl.balanceOf(poolSigner.address);

      await accumulatorProxy.connect(feesRouterSigner).sendLpRewards(poolSigner.address);

      const poolBalAfter = await usdl.balanceOf(poolSigner.address);
      expect(poolBalAfter - poolBalBefore).to.equal(accumulated);
    });

    it("should reset accumulated fees to zero", async function () {
      await recordFeeFromPool(ethers.parseUnits("10", 6));
      await accumulatorProxy.connect(feesRouterSigner).sendLpRewards(poolSigner.address);
      expect(await accumulatorProxy.getAccumulatedFees(poolSigner.address)).to.equal(0);
    });

    it("should emit LpRewardsSent event", async function () {
      await recordFeeFromPool(ethers.parseUnits("10", 6));
      await expect(
        accumulatorProxy.connect(feesRouterSigner).sendLpRewards(poolSigner.address)
      ).to.emit(accumulatorProxy, "LpRewardsSent");
    });

    it("should revert when no fees accumulated", async function () {
      await expect(
        accumulatorProxy.connect(feesRouterSigner).sendLpRewards(poolSigner.address)
      ).to.be.revertedWithCustomError(accumulatorProxy, "NoFeesAccumulated");
    });
  });

  describe("UUPS upgrade", function () {
    it("should upgrade by admin", async function () {
      const V2 = await ethers.getContractFactory("FeeAccumulator");
      const implV2 = await V2.deploy();
      await accumulatorProxy.upgradeToAndCall(await implV2.getAddress(), "0x");
      expect(await accumulatorProxy.usdlAddress()).to.equal(await usdl.getAddress());
    });

    it("should revert upgrade by non-admin", async function () {
      const V2 = await ethers.getContractFactory("FeeAccumulator");
      const implV2 = await V2.deploy();
      await expect(
        accumulatorProxy.connect(alice).upgradeToAndCall(await implV2.getAddress(), "0x")
      ).to.be.revertedWithCustomError(accumulatorProxy, "MissingRole");
    });
  });
});
