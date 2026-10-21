const { expect } = require("chai");
const positiveMetadata = value => value > 0n;
const { ethers } = require("hardhat");
const { FeeStrategy } = require("../helpers/constants");

describe("SidioraNFT", function () {
  let nft, nftProxy, eventEmitter;
  let deployer, alice, bob, factory, feesRouter;

  before(async function () {
    [deployer, alice, bob, factory, feesRouter] = await ethers.getSigners();
  });

  beforeEach(async function () {
    // Deploy mock EventEmitter
    const MockEE = await ethers.getContractFactory("MockEventEmitter");
    eventEmitter = await MockEE.deploy();
    await eventEmitter.waitForDeployment();

    // Deploy SidioraNFT implementation
    const NFT = await ethers.getContractFactory("SidioraNFT");
    const impl = await NFT.deploy();
    await impl.waitForDeployment();

    // Deploy proxy
    const initData = NFT.interface.encodeFunctionData("initialize", [
      "Sidiora Pool NFT",
      "SIDNFT",
      await eventEmitter.getAddress(),
      deployer.address,
    ]);
    const Proxy = await ethers.getContractFactory("UUPSProxy");
    const proxy = await Proxy.deploy(await impl.getAddress(), initData);
    await proxy.waitForDeployment();

    nftProxy = NFT.attach(await proxy.getAddress());

    // Grant roles
    const MINTER_ROLE = ethers.keccak256(ethers.toUtf8Bytes("MINTER_ROLE"));
    const STRATEGY_SETTER_ROLE = ethers.keccak256(ethers.toUtf8Bytes("STRATEGY_SETTER_ROLE"));
    await nftProxy.grantRole(MINTER_ROLE, factory.address);
    await nftProxy.grantRole(STRATEGY_SETTER_ROLE, feesRouter.address);
  });

  describe("initialization", function () {
    it("should set correct name and symbol", async function () {
      expect(await nftProxy.name()).to.equal("Sidiora Pool NFT");
      expect(await nftProxy.symbol()).to.equal("SIDNFT");
    });

    it("should start nextTokenId at 1", async function () {
      expect(await nftProxy.nextTokenId()).to.equal(1);
    });

    it("should grant DEFAULT_ADMIN_ROLE to admin", async function () {
      expect(await nftProxy.hasRole(ethers.ZeroHash, deployer.address)).to.be.true;
    });

    it("should revert on double initialization", async function () {
      await expect(
        nftProxy.initialize("X", "Y", ethers.ZeroAddress, deployer.address)
      ).to.be.revertedWithCustomError(nftProxy, "AlreadyInitialized");
    });
  });

  describe("mint", function () {
    it("should mint NFT from factory", async function () {
      const pool = alice.address; // mock pool address
      await nftProxy.connect(factory).mint(alice.address, pool, Number(FeeStrategy.CLAIM));

      expect(await nftProxy.ownerOf(1)).to.equal(alice.address);
      expect(await nftProxy.totalSupply()).to.equal(1);
      expect(await nftProxy.nextTokenId()).to.equal(2);
    });

    it("should store pool address and fee strategy", async function () {
      const pool = bob.address;
      await nftProxy.connect(factory).mint(alice.address, pool, Number(FeeStrategy.BURN));

      expect(await nftProxy.getPoolAddress(1)).to.equal(pool);
      expect(await nftProxy.getFeeStrategy(1)).to.equal(Number(FeeStrategy.BURN));
    });

    it("should emit PoolNFTMinted event", async function () {
      const pool = bob.address;
      await expect(
        nftProxy.connect(factory).mint(alice.address, pool, Number(FeeStrategy.CLAIM))
      ).to.emit(nftProxy, "PoolNFTMinted")
        .withArgs(1, alice.address, pool);
    });

    it("should revert from non-factory (missing MINTER_ROLE)", async function () {
      await expect(
        nftProxy.connect(alice).mint(alice.address, bob.address, 0)
      ).to.be.revertedWithCustomError(nftProxy, "MissingRole");
    });

    it("should revert with zero recipient", async function () {
      await expect(
        nftProxy.connect(factory).mint(ethers.ZeroAddress, bob.address, 0)
      ).to.be.revertedWithCustomError(nftProxy, "ZeroAddress");
    });

    it("should revert with zero pool address", async function () {
      await expect(
        nftProxy.connect(factory).mint(alice.address, ethers.ZeroAddress, 0)
      ).to.be.revertedWithCustomError(nftProxy, "ZeroAddress");
    });

    it("should revert with invalid strategy (>3)", async function () {
      await expect(
        nftProxy.connect(factory).mint(alice.address, bob.address, 4)
      ).to.be.revertedWithCustomError(nftProxy, "InvalidStrategy");
    });

    it("should mint multiple NFTs with incrementing IDs", async function () {
      await nftProxy.connect(factory).mint(alice.address, alice.address, 0);
      await nftProxy.connect(factory).mint(bob.address, bob.address, 1);

      expect(await nftProxy.ownerOf(1)).to.equal(alice.address);
      expect(await nftProxy.ownerOf(2)).to.equal(bob.address);
      expect(await nftProxy.totalSupply()).to.equal(2);
    });
  });

  describe("fee strategy", function () {
    beforeEach(async function () {
      await nftProxy.connect(factory).mint(alice.address, bob.address, Number(FeeStrategy.CLAIM));
    });

    it("should allow token owner to change strategy", async function () {
      await nftProxy.connect(alice).setFeeStrategy(1, Number(FeeStrategy.BURN));
      expect(await nftProxy.getFeeStrategy(1)).to.equal(Number(FeeStrategy.BURN));
    });

    it("should allow STRATEGY_SETTER_ROLE to change strategy", async function () {
      await nftProxy.connect(feesRouter).setFeeStrategy(1, Number(FeeStrategy.LP_REWARDS));
      expect(await nftProxy.getFeeStrategy(1)).to.equal(Number(FeeStrategy.LP_REWARDS));
    });

    it("should emit FeeStrategyChanged event", async function () {
      await expect(
        nftProxy.connect(alice).setFeeStrategy(1, Number(FeeStrategy.LP_REWARDS))
      ).to.emit(nftProxy, "FeeStrategyChanged")
        .withArgs(1, Number(FeeStrategy.CLAIM), Number(FeeStrategy.LP_REWARDS));
    });

    it("should revert from unauthorized caller", async function () {
      await expect(
        nftProxy.connect(bob).setFeeStrategy(1, Number(FeeStrategy.BURN))
      ).to.be.revertedWithCustomError(nftProxy, "NotApproved");
    });

    it("should revert with invalid strategy", async function () {
      await expect(
        nftProxy.connect(alice).setFeeStrategy(1, 5)
      ).to.be.revertedWithCustomError(nftProxy, "InvalidStrategy");
    });
  });

  describe("transfers", function () {
    beforeEach(async function () {
      await nftProxy.connect(factory).mint(alice.address, bob.address, Number(FeeStrategy.CLAIM));
    });

    it("should transfer NFT and fee rights", async function () {
      await nftProxy.connect(alice).transferFrom(alice.address, bob.address, 1);
      expect(await nftProxy.ownerOf(1)).to.equal(bob.address);
      // New owner can change strategy
      await nftProxy.connect(bob).setFeeStrategy(1, Number(FeeStrategy.BURN));
      expect(await nftProxy.getFeeStrategy(1)).to.equal(Number(FeeStrategy.BURN));
    });
  });

  describe("enumeration", function () {
    it("should enumerate tokens correctly", async function () {
      await nftProxy.connect(factory).mint(alice.address, alice.address, 0);
      await nftProxy.connect(factory).mint(alice.address, bob.address, 1);

      expect(await nftProxy.totalSupply()).to.equal(2);
      expect(await nftProxy.tokenOfOwnerByIndex(alice.address, 0)).to.equal(1);
      expect(await nftProxy.tokenOfOwnerByIndex(alice.address, 1)).to.equal(2);
    });
  });

  describe("ERC165", function () {
    it("should support ERC721 interface", async function () {
      expect(await nftProxy.supportsInterface("0x80ac58cd")).to.be.true;
    });

    it("should support ERC721Enumerable interface", async function () {
      expect(await nftProxy.supportsInterface("0x780e9d63")).to.be.true;
    });

    it("should support ERC165 interface", async function () {
      expect(await nftProxy.supportsInterface("0x01ffc9a7")).to.be.true;
    });
  });

  describe("UUPS upgrade", function () {
    it("should upgrade by admin", async function () {
      const V2 = await ethers.getContractFactory("SidioraNFT");
      const implV2 = await V2.deploy();
      await nftProxy.upgradeToAndCall(await implV2.getAddress(), "0x");
      expect(await nftProxy.name()).to.equal("Sidiora Pool NFT");
    });

    it("should revert upgrade by non-admin", async function () {
      const V2 = await ethers.getContractFactory("SidioraNFT");
      const implV2 = await V2.deploy();
      await expect(
        nftProxy.connect(alice).upgradeToAndCall(await implV2.getAddress(), "0x")
      ).to.be.revertedWithCustomError(nftProxy, "MissingRole");
    });
  });
  describe("audit remediation: pool fee-right uniqueness and safe receipt", function () {
    it("rejects a second fee-right NFT for the same pool", async function () {
      await nftProxy.connect(factory).mint(alice.address, alice.address, 0);
      await expect(nftProxy.connect(factory).mint(bob.address, alice.address, 1))
        .to.be.revertedWithCustomError(nftProxy, "DuplicatePool");
      expect(await nftProxy.poolTokenId(alice.address)).to.equal(1);
    });
    it("rolls back mint and metadata for a contract lacking ERC721 receipt support", async function () {
      const receiver = await (await ethers.getContractFactory("MockERC20")).deploy("BadReceiver", "BAD", 6);
      await expect(nftProxy.connect(factory).mint(await receiver.getAddress(), alice.address, 0))
        .to.be.revertedWithCustomError(nftProxy, "NonERC721Receiver");
      expect(await nftProxy.nextTokenId()).to.equal(1);
      expect(await nftProxy.poolTokenId(alice.address)).to.equal(0);
    });
  });

  describe("audit remediation: populated NFT uniqueness migration", function () {
    it("indexes legacy rights before allowing additional mints", async function () {
      const baseline = require("../../../audit/2026-10-09/remediation/evidence/original-artifacts/SidioraNFT.json");
      const Legacy = new ethers.ContractFactory(baseline.abi, baseline.bytecode, deployer);
      const legacy = await Legacy.deploy();
      const Proxy = await ethers.getContractFactory("UUPSProxy");
      const proxy = await Proxy.deploy(await legacy.getAddress(), Legacy.interface.encodeFunctionData("initialize", [
        "Legacy", "LEG", ethers.ZeroAddress, deployer.address]));
      const prior = Legacy.attach(await proxy.getAddress());
      const minter = ethers.keccak256(ethers.toUtf8Bytes("MINTER_ROLE"));
      await prior.grantRole(minter, factory.address);
      await prior.connect(factory).mint(alice.address, alice.address, 0);
      await prior.connect(factory).mint(bob.address, bob.address, 1);
      const Next = await ethers.getContractFactory("SidioraNFT");
      const next = await Next.deploy();
      await prior.upgradeToAndCall(await next.getAddress(), "0x");
      const upgraded = Next.attach(await proxy.getAddress());
      expect(await upgraded.ownerOf(1)).to.equal(alice.address);
      await expect(upgraded.connect(factory).mint(alice.address, deployer.address, 0))
        .to.be.revertedWithCustomError(upgraded, "PoolIndexMigrationRequired");
      await expect(upgraded.connect(alice).migratePoolIndex(1)).to.be.reverted;
      await upgraded.migratePoolIndex(1);
      expect(await upgraded.poolIndexReady()).to.equal(false);
      await upgraded.migratePoolIndex(1);
      expect(await upgraded.poolIndexReady()).to.equal(true);
      expect(await upgraded.poolTokenId(alice.address)).to.equal(1);
      expect(await upgraded.poolTokenId(bob.address)).to.equal(2);
      await expect(upgraded.connect(factory).mint(alice.address, alice.address, 0))
        .to.be.revertedWithCustomError(upgraded, "DuplicatePool");
      await upgraded.connect(factory).mint(alice.address, deployer.address, 0);
      expect(await upgraded.totalSupply()).to.equal(3);
    });
  });

  describe("audit remediation: canonical NFT hub mirrors", function () {
    it("mirrors mint, transfer and strategy payloads", async function () {
      await expect(nftProxy.connect(factory).mint(alice.address, bob.address, 0))
        .to.emit(eventEmitter, "NftMint").withArgs(1, alice.address, bob.address, 0, positiveMetadata, positiveMetadata);
      await expect(nftProxy.connect(alice).transferFrom(alice.address, bob.address, 1))
        .to.emit(eventEmitter, "NftTransfer").withArgs(await nftProxy.getAddress(), alice.address, bob.address, 1, positiveMetadata, positiveMetadata);
      await expect(nftProxy.connect(bob).setFeeStrategy(1, 3)).to.emit(eventEmitter, "FeeStrategyChanged")
        .withArgs(ethers.zeroPadValue(bob.address, 32), 1, 0, 3, positiveMetadata, positiveMetadata);
    });
  });

  describe("audit remediation: unsupported snapshot-token strategy fails closed", function () {
    it("rejects direct owner/strategy-role changes and mint without authenticated snapshot support", async function () {
      await nftProxy.connect(factory).mint(alice.address, bob.address, 0);
      await expect(nftProxy.connect(alice).setFeeStrategy(1, 2)).to.be.revertedWithCustomError(nftProxy, "UnsupportedSnapshotToken");
      await expect(nftProxy.connect(feesRouter).setFeeStrategy(1, 2)).to.be.revertedWithCustomError(nftProxy, "UnsupportedSnapshotToken");
      await expect(nftProxy.connect(factory).mint(alice.address, deployer.address, 2)).to.be.revertedWithCustomError(nftProxy, "UnsupportedSnapshotToken");
      expect(await nftProxy.getFeeStrategy(1)).to.equal(0);
    });
  });

});
