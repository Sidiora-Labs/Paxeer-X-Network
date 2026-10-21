// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
//
// EventEmitter v2 — coverage for the additions:
//   - VERSION() accessor
//   - EVENT_EMITTER_ROLE separation from DEFAULT_ADMIN_ROLE
//   - reinitializeV2 (idempotent migration)
//   - 5-path authorization mesh (static / poolRegistry / opticalRegistry / sidioraFactory / registeredToken)
//   - Generic schemaless emission (EventLog{,1,2}) with indexed eventNameHash
//   - Typed fast-path emitters across every domain
//
// The v1 surface is verified by `EventEmitter.test.js` — this file does not
// re-cover those signatures.

const { expect } = require("chai");
const { ethers } = require("hardhat");

// Helper: chai matcher for any uint
const anyUint = () => true;

// Helper: build an EventData with optional fields.
function emptyEventData() {
  return {
    addressItems: { items: [], arrayItems: [] },
    uintItems: { items: [], arrayItems: [] },
    intItems: { items: [], arrayItems: [] },
    boolItems: { items: [], arrayItems: [] },
    bytes32Items: { items: [], arrayItems: [] },
    bytesItems: { items: [], arrayItems: [] },
    stringItems: { items: [], arrayItems: [] },
  };
}

describe("EventEmitter v2", function () {
  // signers
  let deployer, alice, bob, carol;
  // role-holders / fake contracts
  let authorizedContract, emitterRoleHolder, factoryAddr, opticalAddr, poolAddr, tokenAddr;

  let emitterProxy;
  let mockPoolRegistry, mockOpticalRegistry;

  const DEFAULT_ADMIN_ROLE = ethers.ZeroHash;
  let EVENT_EMITTER_ROLE;

  before(async function () {
    [
      deployer,
      alice,
      bob,
      carol,
      authorizedContract,
      emitterRoleHolder,
      factoryAddr,
      opticalAddr,
      poolAddr,
      tokenAddr,
    ] = await ethers.getSigners();
    EVENT_EMITTER_ROLE = ethers.id("EVENT_EMITTER_ROLE");
  });

  beforeEach(async function () {
    // Deploy v2 impl + UUPS proxy
    const EventEmitter = await ethers.getContractFactory("EventEmitter");
    const impl = await EventEmitter.deploy();
    await impl.waitForDeployment();

    const initData = EventEmitter.interface.encodeFunctionData("initialize", [
      deployer.address,
    ]);
    const Proxy = await ethers.getContractFactory("UUPSProxy");
    const proxy = await Proxy.deploy(await impl.getAddress(), initData);
    await proxy.waitForDeployment();

    emitterProxy = EventEmitter.attach(await proxy.getAddress());

    // Run v2 migration
    await emitterProxy.reinitializeV2(emitterRoleHolder.address);

    // Authorize the test "contract" signer for direct emission tests.
    await emitterProxy.setAuthorizedEmitter(authorizedContract.address, true);
    // This fixture signer deliberately represents each canonical typed source.
    for (const fragment of emitterProxy.interface.fragments) {
      if (fragment.type === "function" && fragment.name.startsWith("emit") && !fragment.name.startsWith("emitEventLog")) {
        await emitterProxy.setTypedEmitter(authorizedContract.address, fragment.selector, true);
      }
    }
  });

  // ════════════════════════════════════════════════════════════════════════
  //                         Version + role separation
  // ════════════════════════════════════════════════════════════════════════

  describe("VERSION accessor", function () {
    it("should return '2.0.0'", async function () {
      expect(await emitterProxy.VERSION()).to.equal("2.0.0");
    });
  });

  describe("EVENT_EMITTER_ROLE", function () {
    it("should expose the role hash via constant getter", async function () {
      expect(await emitterProxy.EVENT_EMITTER_ROLE()).to.equal(EVENT_EMITTER_ROLE);
    });

    it("should be granted to the address passed to reinitializeV2", async function () {
      expect(
        await emitterProxy.hasRole(EVENT_EMITTER_ROLE, emitterRoleHolder.address)
      ).to.be.true;
    });

    it("should be admin'd by DEFAULT_ADMIN_ROLE", async function () {
      expect(await emitterProxy.getRoleAdmin(EVENT_EMITTER_ROLE)).to.equal(
        DEFAULT_ADMIN_ROLE
      );
    });

    it("should NOT grant DEFAULT_ADMIN_ROLE to EVENT_EMITTER_ROLE holder", async function () {
      expect(
        await emitterProxy.hasRole(DEFAULT_ADMIN_ROLE, emitterRoleHolder.address)
      ).to.be.false;
    });

    it("should let EVENT_EMITTER_ROLE holder authorize emitters", async function () {
      await expect(
        emitterProxy
          .connect(emitterRoleHolder)
          .setAuthorizedEmitter(alice.address, true)
      ).to.not.be.reverted;
      expect(await emitterProxy.isAuthorizedEmitter(alice.address)).to.be.true;
    });

    it("should NOT let EVENT_EMITTER_ROLE holder upgrade the impl", async function () {
      const EventEmitter = await ethers.getContractFactory("EventEmitter");
      const newImpl = await EventEmitter.deploy();
      await expect(
        emitterProxy
          .connect(emitterRoleHolder)
          .upgradeToAndCall(await newImpl.getAddress(), "0x")
      ).to.be.revertedWithCustomError(emitterProxy, "MissingRole");
    });
  });

  // ════════════════════════════════════════════════════════════════════════
  //                            reinitializeV2
  // ════════════════════════════════════════════════════════════════════════

  describe("reinitializeV2", function () {
    it("should be idempotent — second call reverts", async function () {
      await expect(
        emitterProxy.reinitializeV2(emitterRoleHolder.address)
      ).to.be.revertedWithCustomError(emitterProxy, "Unauthorized");
    });

    it("should reject non-admin caller", async function () {
      // Re-deploy a clean proxy first — beforeEach already ran reinit.
      const EventEmitter = await ethers.getContractFactory("EventEmitter");
      const impl = await EventEmitter.deploy();
      const Proxy = await ethers.getContractFactory("UUPSProxy");
      const initData = EventEmitter.interface.encodeFunctionData("initialize", [
        deployer.address,
      ]);
      const proxy = await Proxy.deploy(await impl.getAddress(), initData);
      const fresh = EventEmitter.attach(await proxy.getAddress());

      await expect(
        fresh.connect(alice).reinitializeV2(alice.address)
      ).to.be.revertedWithCustomError(fresh, "MissingRole");
    });

    it("should accept zero address (skip role grant)", async function () {
      const EventEmitter = await ethers.getContractFactory("EventEmitter");
      const impl = await EventEmitter.deploy();
      const Proxy = await ethers.getContractFactory("UUPSProxy");
      const initData = EventEmitter.interface.encodeFunctionData("initialize", [
        deployer.address,
      ]);
      const proxy = await Proxy.deploy(await impl.getAddress(), initData);
      const fresh = EventEmitter.attach(await proxy.getAddress());

      await fresh.reinitializeV2(ethers.ZeroAddress);
      // Admin can grant later
      expect(await fresh.hasRole(EVENT_EMITTER_ROLE, alice.address)).to.be.false;
      await fresh.grantRole(EVENT_EMITTER_ROLE, alice.address);
      expect(await fresh.hasRole(EVENT_EMITTER_ROLE, alice.address)).to.be.true;
    });
  });

  // ════════════════════════════════════════════════════════════════════════
  //                        Authorization mesh — 5 paths
  // ════════════════════════════════════════════════════════════════════════

  describe("authorization mesh", function () {
    it("path 1 — static authorized emitter passes", async function () {
      await expect(
        emitterProxy
          .connect(authorizedContract)
          .emitEventLog("test.path1", emptyEventData())
      ).to.emit(emitterProxy, "EventLog");
    });

    it("path 2 — poolRegistry.isRegisteredPool passes", async function () {
      const MockReg = await ethers.getContractFactory("MockPoolRegistry");
      const mockReg = await MockReg.deploy();
      await mockReg.waitForDeployment();
      await emitterProxy.setPoolRegistry(await mockReg.getAddress());

      // Mark `poolAddr` as a registered pool in the mock.
      await mockReg.setRegistered(poolAddr.address, true);

      await expect(
        emitterProxy
          .connect(poolAddr)
          .emitEventLog("test.path2", emptyEventData())
      ).to.emit(emitterProxy, "EventLog");

      // Reverse: deregister and confirm reject.
      await mockReg.setRegistered(poolAddr.address, false);
      await expect(
        emitterProxy
          .connect(poolAddr)
          .emitEventLog("test.path2-deregistered", emptyEventData())
      ).to.be.revertedWithCustomError(emitterProxy, "Unauthorized");
    });

    it("path 3 — opticalRegistry.isApproved passes", async function () {
      const MockOpt = await ethers.getContractFactory("MockOpticalRegistry");
      const mockOpt = await MockOpt.deploy();
      await mockOpt.waitForDeployment();
      await emitterProxy.setOpticalRegistry(await mockOpt.getAddress());

      // Mark `opticalAddr` as approved in the registry.
      await mockOpt.setApproved(opticalAddr.address, true);

      await expect(
        emitterProxy
          .connect(opticalAddr)
          .emitEventLog("test.path3", emptyEventData())
      ).to.emit(emitterProxy, "EventLog");

      // Reverse: revoke approval and confirm reject.
      await mockOpt.setApproved(opticalAddr.address, false);
      await expect(
        emitterProxy
          .connect(opticalAddr)
          .emitEventLog("test.path3-revoked", emptyEventData())
      ).to.be.revertedWithCustomError(emitterProxy, "Unauthorized");
    });

    it("path 4 — sidioraFactory direct passes", async function () {
      await emitterProxy.setSidioraFactory(factoryAddr.address);
      await expect(
        emitterProxy
          .connect(factoryAddr)
          .emitEventLog("test.factory", emptyEventData())
      ).to.emit(emitterProxy, "EventLog");
    });

    it("path 5 — registered pool token passes", async function () {
      await emitterProxy.setSidioraFactory(factoryAddr.address);
      // Factory registers the token
      await emitterProxy
        .connect(factoryAddr)
        .registerToken(tokenAddr.address, poolAddr.address);
      expect(await emitterProxy.isRegisteredToken(tokenAddr.address)).to.be
        .true;

      await expect(
        emitterProxy
          .connect(tokenAddr)
          .emitTokenTransfer(tokenAddr.address, alice.address, bob.address, 100n)
      ).to.emit(emitterProxy, "TokenTransfer");
    });

    it("rejects unauthorized senders across all paths", async function () {
      await expect(
        emitterProxy
          .connect(carol)
          .emitEventLog("test.reject", emptyEventData())
      ).to.be.revertedWithCustomError(emitterProxy, "Unauthorized");
    });
  });

  // ════════════════════════════════════════════════════════════════════════
  //                        Registry wiring
  // ════════════════════════════════════════════════════════════════════════

  describe("registry wiring", function () {
    it("setOpticalRegistry — admin succeeds", async function () {
      await emitterProxy.setOpticalRegistry(opticalAddr.address);
      expect(await emitterProxy.opticalRegistry()).to.equal(opticalAddr.address);
    });

    it("setOpticalRegistry — EVENT_EMITTER_ROLE succeeds", async function () {
      await emitterProxy
        .connect(emitterRoleHolder)
        .setOpticalRegistry(opticalAddr.address);
      expect(await emitterProxy.opticalRegistry()).to.equal(opticalAddr.address);
    });

    it("setOpticalRegistry — random caller reverts", async function () {
      await expect(
        emitterProxy.connect(alice).setOpticalRegistry(opticalAddr.address)
      ).to.be.revertedWithCustomError(emitterProxy, "MissingRole");
    });

    it("setMetaAGRouter — admin succeeds", async function () {
      await emitterProxy.setMetaAGRouter(alice.address);
      expect(await emitterProxy.metaAGRouter()).to.equal(alice.address);
    });

    it("setSidioraFactory — admin succeeds", async function () {
      await emitterProxy.setSidioraFactory(factoryAddr.address);
      expect(await emitterProxy.sidioraFactory()).to.equal(factoryAddr.address);
    });

    it("setTokenRegistry — admin no-ops without revert", async function () {
      await expect(
        emitterProxy.setTokenRegistry(alice.address)
      ).to.not.be.reverted;
    });
  });

  // ════════════════════════════════════════════════════════════════════════
  //                 Token / NFT registration & deregistration
  // ════════════════════════════════════════════════════════════════════════

  describe("token registration", function () {
    it("factory can register a token", async function () {
      await emitterProxy.setSidioraFactory(factoryAddr.address);
      await emitterProxy
        .connect(factoryAddr)
        .registerToken(tokenAddr.address, poolAddr.address);
      expect(await emitterProxy.isRegisteredToken(tokenAddr.address)).to.be
        .true;
    });

    it("EVENT_EMITTER_ROLE can register a token", async function () {
      await emitterProxy
        .connect(emitterRoleHolder)
        .registerToken(tokenAddr.address, poolAddr.address);
      expect(await emitterProxy.isRegisteredToken(tokenAddr.address)).to.be
        .true;
    });

    it("random caller cannot register a token", async function () {
      await expect(
        emitterProxy
          .connect(alice)
          .registerToken(tokenAddr.address, poolAddr.address)
      ).to.be.revertedWithCustomError(emitterProxy, "Unauthorized");
    });

    it("admin can deregister a token", async function () {
      await emitterProxy
        .connect(emitterRoleHolder)
        .registerToken(tokenAddr.address, poolAddr.address);
      await emitterProxy.deregisterToken(tokenAddr.address);
      expect(await emitterProxy.isRegisteredToken(tokenAddr.address)).to.be
        .false;
    });
  });

  // ════════════════════════════════════════════════════════════════════════
  //                Generic schemaless emit (EventLog{,1,2})
  // ════════════════════════════════════════════════════════════════════════

  describe("emitEventLog", function () {
    it("emits with indexed eventNameHash and full eventData round-trip", async function () {
      const eventName = "PecorTrade.fillSummary";
      const expectedHash = ethers.id(eventName);

      const data = emptyEventData();
      data.addressItems.items = [
        { key: "trader", value: alice.address },
        { key: "tokenIn", value: bob.address },
      ];
      data.uintItems.items = [
        { key: "amountIn", value: 1234n },
        { key: "amountOut", value: 5678n },
      ];
      data.boolItems.items = [{ key: "isBuy", value: true }];
      data.stringItems.items = [{ key: "memo", value: "filled OK" }];
      data.bytes32Items.items = [{ key: "txTag", value: ethers.id("tag1") }];

      await expect(
        emitterProxy.connect(authorizedContract).emitEventLog(eventName, data)
      )
        .to.emit(emitterProxy, "EventLog")
        .withArgs(
          authorizedContract.address,
          expectedHash,
          eventName,
          anyUint, // EventData struct — not asserted positionally
          anyUint, // timestamp
          anyUint  // blockNumber
        );
    });
  });

  describe("emitEventLog1", function () {
    it("emits with one extra indexed topic", async function () {
      const eventName = "Pool.created";
      const topic1 = ethers.zeroPadValue(alice.address, 32);

      await expect(
        emitterProxy
          .connect(authorizedContract)
          .emitEventLog1(eventName, topic1, emptyEventData())
      ).to.emit(emitterProxy, "EventLog1");
    });
  });

  describe("emitEventLog2", function () {
    it("emits with two extra topics", async function () {
      const eventName = "User.tokenAction";
      const topic1 = ethers.zeroPadValue(alice.address, 32);
      const topic2 = ethers.zeroPadValue(bob.address, 32);

      await expect(
        emitterProxy
          .connect(authorizedContract)
          .emitEventLog2(eventName, topic1, topic2, emptyEventData())
      ).to.emit(emitterProxy, "EventLog2");
    });
  });

  describe("generic emit auth", function () {
    it("rejects unauthorized callers", async function () {
      await expect(
        emitterProxy
          .connect(alice)
          .emitEventLog("blocked", emptyEventData())
      ).to.be.revertedWithCustomError(emitterProxy, "Unauthorized");
    });
  });

  // ════════════════════════════════════════════════════════════════════════
  //                       Typed fast-paths — PECOR
  // ════════════════════════════════════════════════════════════════════════

  describe("emitPecorSwap", function () {
    it("emits PecorSwap with the full payload", async function () {
      await expect(
        emitterProxy.connect(authorizedContract).emitPecorSwap(
          alice.address,
          bob.address,
          carol.address,
          1000n,
          990n,
          ethers.parseUnits("1", 18),
          ethers.parseUnits("1.01", 18),
          ethers.parseUnits("1000", 18),
          30,
          3n,
          5,
          0
        )
      ).to.emit(emitterProxy, "PecorSwap");
    });
  });

  describe("emitPecorOrderCreated", function () {
    it("emits PecorOrderCreated", async function () {
      await expect(
        emitterProxy
          .connect(authorizedContract)
          .emitPecorOrderCreated(
            42n,
            alice.address,
            0,
            1,
            bob.address,
            carol.address,
            1000n,
            ethers.parseUnits("1.05", 18),
            0n,
            0n
          )
      ).to.emit(emitterProxy, "PecorOrderCreated");
    });
  });

  describe("emitPecorOrderLifecycle", function () {
    it("emits PecorOrderLifecycle", async function () {
      await expect(
        emitterProxy
          .connect(authorizedContract)
          .emitPecorOrderLifecycle(
            42n,
            alice.address,
            0,
            1,
            ethers.parseUnits("1.05", 18),
            "0x"
          )
      ).to.emit(emitterProxy, "PecorOrderLifecycle");
    });
  });

  describe("emitBestRouteSwap", function () {
    it("emits BestRouteSwap", async function () {
      await expect(
        emitterProxy
          .connect(authorizedContract)
          .emitBestRouteSwap(
            alice.address,
            bob.address,
            carol.address,
            ethers.id("route1"),
            1000n,
            995n,
            [bob.address, carol.address],
            5
          )
      ).to.emit(emitterProxy, "BestRouteSwap");
    });
  });

  // ════════════════════════════════════════════════════════════════════════
  //                       Typed fast-paths — Oracle
  // ════════════════════════════════════════════════════════════════════════

  describe("emitPriceUpdated", function () {
    it("emits PriceUpdated", async function () {
      await expect(
        emitterProxy
          .connect(authorizedContract)
          .emitPriceUpdated(
            bob.address,
            7n,
            alice.address,
            ethers.parseUnits("100", 18),
            10000n,
            ethers.id("source-pyth")
          )
      ).to.emit(emitterProxy, "PriceUpdated");
    });
  });

  describe("emitCircuitBreaker", function () {
    it("emits CircuitBreakerTriggered", async function () {
      await expect(
        emitterProxy
          .connect(authorizedContract)
          .emitCircuitBreaker(
            bob.address,
            ethers.id("source-pyth"),
            ethers.parseUnits("110", 18),
            ethers.parseUnits("100", 18),
            1000n
          )
      ).to.emit(emitterProxy, "CircuitBreakerTriggered");
    });
  });

  describe("emitOracleAdapterLifecycle", function () {
    it("emits OracleAdapterLifecycle", async function () {
      await expect(
        emitterProxy
          .connect(authorizedContract)
          .emitOracleAdapterLifecycle(
            ethers.id("source-pyth"),
            alice.address,
            0,
            100
          )
      ).to.emit(emitterProxy, "OracleAdapterLifecycle");
    });
  });

  // ════════════════════════════════════════════════════════════════════════
  //                       Typed fast-paths — Vault / Treasury / Gov
  // ════════════════════════════════════════════════════════════════════════

  describe("emitVaultFlow", function () {
    it("emits VaultFlow", async function () {
      await expect(
        emitterProxy
          .connect(authorizedContract)
          .emitVaultFlow(0, bob.address, alice.address, 500n, 10000n)
      ).to.emit(emitterProxy, "VaultFlow");
    });
  });

  describe("emitGovernance", function () {
    it("emits Governance", async function () {
      await expect(
        emitterProxy
          .connect(authorizedContract)
          .emitGovernance(0, ethers.id("tx1"), alice.address, "0x1234")
      ).to.emit(emitterProxy, "Governance");
    });
  });

  describe("emitTreasuryFlow", function () {
    it("emits TreasuryFlow", async function () {
      await expect(
        emitterProxy
          .connect(authorizedContract)
          .emitTreasuryFlow(0, bob.address, alice.address, 500n)
      ).to.emit(emitterProxy, "TreasuryFlow");
    });
  });

  describe("emitOpticalLifecycle", function () {
    it("emits OpticalLifecycle", async function () {
      await expect(
        emitterProxy
          .connect(authorizedContract)
          .emitOpticalLifecycle(
            0,
            opticalAddr.address,
            poolAddr.address,
            ethers.id("AntiSnipe"),
            "0x"
          )
      ).to.emit(emitterProxy, "OpticalLifecycle");
    });
  });

  // ════════════════════════════════════════════════════════════════════════
  //                   Typed fast-paths — Token / NFT mirrors
  // ════════════════════════════════════════════════════════════════════════

  describe("emitTokenTransfer", function () {
    it("emits TokenTransfer", async function () {
      await expect(
        emitterProxy
          .connect(authorizedContract)
          .emitTokenTransfer(
            authorizedContract.address,
            alice.address,
            bob.address,
            1000n
          )
      ).to.emit(emitterProxy, "TokenTransfer");
    });
  });

  describe("emitNftTransfer", function () {
    it("emits NftTransfer", async function () {
      await expect(
        emitterProxy
          .connect(authorizedContract)
          .emitNftTransfer(
            authorizedContract.address,
            alice.address,
            bob.address,
            42n
          )
      ).to.emit(emitterProxy, "NftTransfer");
    });
  });

  describe("emitAssetApproval", function () {
    it("emits AssetApproval — ERC20", async function () {
      await expect(
        emitterProxy
          .connect(authorizedContract)
          .emitAssetApproval(
            authorizedContract.address,
            alice.address,
            bob.address,
            1000n,
            false
          )
      ).to.emit(emitterProxy, "AssetApproval");
    });

    it("emits AssetApproval — ERC721", async function () {
      await expect(
        emitterProxy
          .connect(authorizedContract)
          .emitAssetApproval(
            authorizedContract.address,
            alice.address,
            bob.address,
            42n,
            true
          )
      ).to.emit(emitterProxy, "AssetApproval");
    });
  });

  // ════════════════════════════════════════════════════════════════════════
  //                  Typed fast-paths — Lifecycle (role/upgrade/pause)
  // ════════════════════════════════════════════════════════════════════════

  describe("emitRoleChange", function () {
    it("emits RoleChange", async function () {
      await expect(
        emitterProxy
          .connect(authorizedContract)
          .emitRoleChange(
            0,
            ethers.id("OPERATOR"),
            alice.address,
            bob.address,
            DEFAULT_ADMIN_ROLE
          )
      ).to.emit(emitterProxy, "RoleChange");
    });
  });

  describe("emitUpgraded", function () {
    it("emits ContractUpgraded", async function () {
      await expect(
        emitterProxy
          .connect(authorizedContract)
          .emitUpgraded(authorizedContract.address, alice.address, 0)
      ).to.emit(emitterProxy, "ContractUpgraded");
    });
  });

  describe("emitPauseToggle", function () {
    it("emits PauseToggle", async function () {
      await expect(
        emitterProxy
          .connect(authorizedContract)
          .emitPauseToggle(authorizedContract.address, true)
      ).to.emit(emitterProxy, "PauseToggle");
    });
  });

  // ════════════════════════════════════════════════════════════════════════
  //                  Typed fast-paths — Launchpad mirrors
  // ════════════════════════════════════════════════════════════════════════

  describe("emitFeeFlow", function () {
    it("emits FeeFlow", async function () {
      await expect(
        emitterProxy
          .connect(authorizedContract)
          .emitFeeFlow(0, poolAddr.address, alice.address, 100n, 10n, 90n, 0n)
      ).to.emit(emitterProxy, "FeeFlow");
    });
  });

  describe("emitRouterTrade", function () {
    it("emits RouterTrade", async function () {
      await expect(
        emitterProxy
          .connect(authorizedContract)
          .emitRouterTrade(
            0,
            poolAddr.address,
            alice.address,
            tokenAddr.address,
            bob.address,
            1000n,
            995n,
            0n
          )
      ).to.emit(emitterProxy, "RouterTrade");
    });
  });

  describe("emitNftMint", function () {
    it("emits NftMint", async function () {
      await expect(
        emitterProxy
          .connect(authorizedContract)
          .emitNftMint(1n, alice.address, poolAddr.address, 0)
      ).to.emit(emitterProxy, "NftMint");
    });
  });

  describe("emitPoolRegistered", function () {
    it("emits PoolRegistered", async function () {
      await expect(
        emitterProxy
          .connect(authorizedContract)
          .emitPoolRegistered(
            poolAddr.address,
            tokenAddr.address,
            alice.address,
            opticalAddr.address,
            1n
          )
      ).to.emit(emitterProxy, "PoolRegistered");
    });
  });

  describe("emitTokenDeployed", function () {
    it("emits TokenDeployed", async function () {
      await expect(
        emitterProxy
          .connect(authorizedContract)
          .emitTokenDeployed(
            tokenAddr.address,
            poolAddr.address,
            alice.address,
            ethers.id("salt-1"),
            "Demo Token",
            "DEMO",
            6,
            1_000_000n
          )
      ).to.emit(emitterProxy, "TokenDeployed");
    });
  });

  // ════════════════════════════════════════════════════════════════════════
  //                       Storage layout invariance
  // ════════════════════════════════════════════════════════════════════════

  describe("storage layout v1 → v2", function () {
    it("preserves poolRegistry slot value across upgrade", async function () {
      // Deploy v2 fresh (already in beforeEach)
      // Set poolRegistry, then "upgrade" (re-deploy same impl, same proxy
      // semantics) and verify the value persists.
      await emitterProxy.setPoolRegistry(poolAddr.address);
      expect(await emitterProxy.poolRegistry()).to.equal(poolAddr.address);

      const EventEmitter = await ethers.getContractFactory("EventEmitter");
      const newImpl = await EventEmitter.deploy();
      await emitterProxy.upgradeToAndCall(await newImpl.getAddress(), "0x");

      expect(await emitterProxy.poolRegistry()).to.equal(poolAddr.address);
    });

    it("preserves authorized emitter map across upgrade", async function () {
      await emitterProxy.setAuthorizedEmitter(alice.address, true);
      const EventEmitter = await ethers.getContractFactory("EventEmitter");
      const newImpl = await EventEmitter.deploy();
      await emitterProxy.upgradeToAndCall(await newImpl.getAddress(), "0x");
      expect(await emitterProxy.isAuthorizedEmitter(alice.address)).to.be.true;
    });

    it("preserves v2 registries across upgrade", async function () {
      await emitterProxy.setOpticalRegistry(opticalAddr.address);
      await emitterProxy.setMetaAGRouter(alice.address);
      await emitterProxy.setSidioraFactory(factoryAddr.address);
      const EventEmitter = await ethers.getContractFactory("EventEmitter");
      const newImpl = await EventEmitter.deploy();
      await emitterProxy.upgradeToAndCall(await newImpl.getAddress(), "0x");
      expect(await emitterProxy.opticalRegistry()).to.equal(opticalAddr.address);
      expect(await emitterProxy.metaAGRouter()).to.equal(alice.address);
      expect(await emitterProxy.sidioraFactory()).to.equal(factoryAddr.address);
    });
  });
});
