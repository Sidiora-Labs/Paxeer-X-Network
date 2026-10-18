// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
//
// EventEmitter v2 — UUPS upgrade flow regression test.
//
// Validates the exact code path that the mainnet Timelock-routed upgrade
// will execute against the deployed proxy at
//   0x6679aF411d534de222C32ed0AF94C3BD67090672
//
// Strategy: build a "v1 era" EventEmitter (same contract, but only
// `initialize(admin)` called — `reinitializeV2` deliberately skipped), let
// it accumulate v1 storage (poolRegistry + a few authorized emitters),
// verify the v1 emit selectors work, then perform the atomic
// `upgradeToAndCall(newImpl, encodeCall(reinitializeV2, [adminWithEmitterRole]))`
// and assert:
//
//   1. v1 storage is preserved (poolRegistry + authorized emitters intact).
//   2. reinitializeV2 grants EVENT_EMITTER_ROLE to the supplied address.
//   3. reinitializeV2 cannot be called twice (idempotent migration).
//   4. New v2 admin surface (setOpticalRegistry / setMetaAGRouter /
//      setSidioraFactory) works post-upgrade.
//   5. New v2 auth paths (sidioraFactory==msg.sender, opticalRegistry,
//      registeredTokens) all gate emission correctly.
//   6. v1 emit signatures (emitMarketCreated, emitSwap, etc.) still fire
//      with identical topic0 / argument layout.
//
// Storage layout invariant: this test mirrors S12 (append-only storage).
// If any pre-state value drifts across the impl swap, the test fails.

const { expect } = require("chai");
const { ethers } = require("hardhat");

const DEFAULT_ADMIN_ROLE = ethers.ZeroHash;

describe("EventEmitter v2 — UUPS upgrade flow", function () {
  let admin, alice, bob, carol;
  let authorizedEmitter, emitterRoleHolder, factoryAddr, opticalAddr, poolAddr;
  let mockPoolRegistry, mockOpticalRegistry;
  let EventEmitter; // factory
  let proxy;        // EventEmitter attached to proxy address
  let v1Impl;       // first impl deployment (the "v1 era" pretend)
  let EVENT_EMITTER_ROLE;

  before(async function () {
    [
      admin,
      alice,
      bob,
      carol,
      authorizedEmitter,
      emitterRoleHolder,
      factoryAddr,
      opticalAddr,
      poolAddr,
    ] = await ethers.getSigners();
    EVENT_EMITTER_ROLE = ethers.id("EVENT_EMITTER_ROLE");
  });

  beforeEach(async function () {
    EventEmitter = await ethers.getContractFactory("EventEmitter");

    // ─── "v1 era" deploy: impl + proxy + initialize(admin) ──────────
    v1Impl = await EventEmitter.deploy();
    await v1Impl.waitForDeployment();

    const initData = EventEmitter.interface.encodeFunctionData("initialize", [
      admin.address,
    ]);
    const Proxy = await ethers.getContractFactory("UUPSProxy");
    const proxyContract = await Proxy.deploy(await v1Impl.getAddress(), initData);
    await proxyContract.waitForDeployment();

    proxy = EventEmitter.attach(await proxyContract.getAddress());

    // Spin up the registry mocks (auth fixtures used in tests/v2 already).
    const MockPoolRegistry = await ethers.getContractFactory("MockPoolRegistry");
    mockPoolRegistry = await MockPoolRegistry.deploy();
    await mockPoolRegistry.waitForDeployment();

    const MockOpticalRegistry = await ethers.getContractFactory("MockOpticalRegistry");
    mockOpticalRegistry = await MockOpticalRegistry.deploy();
    await mockOpticalRegistry.waitForDeployment();

    // ─── v1 storage population (slots 0,1,2) ─────────────────────────
    await proxy.connect(admin).setPoolRegistry(await mockPoolRegistry.getAddress());
    await proxy.connect(admin).setAuthorizedEmitter(authorizedEmitter.address, true);
    // This fixture signer deliberately represents each canonical typed source.
    for (const fragment of proxy.interface.fragments) {
      if (fragment.type === "function" && fragment.name.startsWith("emit") && !fragment.name.startsWith("emitEventLog")) {
        await proxy.connect(admin).setTypedEmitter(authorizedEmitter.address, fragment.selector, true);
      }
    }
    await mockPoolRegistry.setRegistered(poolAddr.address, true);
  });

  // ════════════════════════════════════════════════════════════════════
  //                           Pre-upgrade baseline
  // ════════════════════════════════════════════════════════════════════

  describe("pre-upgrade (v1 era)", function () {
    it("VERSION still returns 2.0.0 (impl is the same code; v1/v2 distinction is the migration call)", async function () {
      // Sanity: in this test the impl IS v2 contract code. The "v1 era"
      // purely means reinitializeV2 hasn't been called yet — slot 7
      // (_v2Initialized) is false and EVENT_EMITTER_ROLE has not been
      // granted to anyone.
      expect(await proxy.VERSION()).to.equal("2.0.0");
    });

    it("v1 storage holds the registered pool registry + authorized emitter", async function () {
      expect(await proxy.poolRegistry()).to.equal(await mockPoolRegistry.getAddress());
      expect(await proxy.isAuthorizedEmitter(authorizedEmitter.address)).to.equal(true);
    });

    it("EVENT_EMITTER_ROLE is unassigned in the v1 era", async function () {
      expect(await proxy.hasRole(EVENT_EMITTER_ROLE, admin.address)).to.equal(false);
      expect(await proxy.hasRole(EVENT_EMITTER_ROLE, emitterRoleHolder.address)).to.equal(false);
    });

    it("v1 emit selectors are callable by the authorized emitter", async function () {
      const poolId = ethers.id("pool/1");
      await expect(
        proxy
          .connect(authorizedEmitter)
          .emitMarketCreated(poolId, alice.address, bob.address, poolAddr.address, opticalAddr.address)
      ).to.emit(proxy, "MarketCreated");
    });
  });

  // ════════════════════════════════════════════════════════════════════
  //                     Upgrade flow (impl swap + reinit)
  // ════════════════════════════════════════════════════════════════════

  describe("upgradeToAndCall(newImpl, reinitializeV2(adminWithEmitterRole))", function () {
    let newImpl;
    let upgradeCalldata;

    beforeEach(async function () {
      newImpl = await EventEmitter.deploy();
      await newImpl.waitForDeployment();

      // Identical to what the Timelock-routed mainnet upgrade does.
      const initCalldata = EventEmitter.interface.encodeFunctionData(
        "reinitializeV2",
        [emitterRoleHolder.address]
      );
      upgradeCalldata = { newImpl: await newImpl.getAddress(), initCalldata };
    });

    it("reverts when called by a non-admin", async function () {
      await expect(
        proxy.connect(alice).upgradeToAndCall(upgradeCalldata.newImpl, upgradeCalldata.initCalldata)
      ).to.be.reverted;
    });

    it("succeeds when called by DEFAULT_ADMIN_ROLE holder", async function () {
      await expect(
        proxy
          .connect(admin)
          .upgradeToAndCall(upgradeCalldata.newImpl, upgradeCalldata.initCalldata)
      ).to.not.be.reverted;
    });

    describe("post-upgrade state", function () {
      beforeEach(async function () {
        await proxy
          .connect(admin)
          .upgradeToAndCall(upgradeCalldata.newImpl, upgradeCalldata.initCalldata);
      });

      // ── Storage preservation ──────────────────────────────────────
      it("preserves v1 poolRegistry across the impl swap", async function () {
        expect(await proxy.poolRegistry()).to.equal(await mockPoolRegistry.getAddress());
      });

      it("preserves v1 authorized-emitter mapping across the impl swap", async function () {
        expect(await proxy.isAuthorizedEmitter(authorizedEmitter.address)).to.equal(true);
      });

      it("preserves DEFAULT_ADMIN_ROLE for the original admin", async function () {
        expect(await proxy.hasRole(DEFAULT_ADMIN_ROLE, admin.address)).to.equal(true);
      });

      // ── reinitializeV2 effects ────────────────────────────────────
      it("grants EVENT_EMITTER_ROLE to adminWithEmitterRole", async function () {
        expect(
          await proxy.hasRole(EVENT_EMITTER_ROLE, emitterRoleHolder.address)
        ).to.equal(true);
      });

      it("does NOT grant DEFAULT_ADMIN_ROLE to the EVENT_EMITTER_ROLE holder", async function () {
        expect(
          await proxy.hasRole(DEFAULT_ADMIN_ROLE, emitterRoleHolder.address)
        ).to.equal(false);
      });

      it("VERSION reports 2.0.0", async function () {
        expect(await proxy.VERSION()).to.equal("2.0.0");
      });

      it("EVENT_EMITTER_ROLE constant getter resolves to keccak256('EVENT_EMITTER_ROLE')", async function () {
        expect(await proxy.EVENT_EMITTER_ROLE()).to.equal(EVENT_EMITTER_ROLE);
      });

      it("EVENT_EMITTER_ROLE is admin'd by DEFAULT_ADMIN_ROLE", async function () {
        expect(await proxy.getRoleAdmin(EVENT_EMITTER_ROLE)).to.equal(DEFAULT_ADMIN_ROLE);
      });

      // ── Idempotency ───────────────────────────────────────────────
      it("reinitializeV2 reverts on second call", async function () {
        await expect(
          proxy.connect(admin).reinitializeV2(emitterRoleHolder.address)
        ).to.be.reverted;
      });

      // ── New v2 admin surface ──────────────────────────────────────
      it("admin can set opticalRegistry post-upgrade", async function () {
        const target = await mockOpticalRegistry.getAddress();
        await proxy.connect(admin).setOpticalRegistry(target);
        expect(await proxy.opticalRegistry()).to.equal(target);
      });

      it("EVENT_EMITTER_ROLE holder can set sidioraFactory post-upgrade", async function () {
        await proxy.connect(emitterRoleHolder).setSidioraFactory(factoryAddr.address);
        expect(await proxy.sidioraFactory()).to.equal(factoryAddr.address);
      });

      it("EVENT_EMITTER_ROLE holder can set metaAGRouter post-upgrade", async function () {
        const router = ethers.Wallet.createRandom().address;
        await proxy.connect(emitterRoleHolder).setMetaAGRouter(router);
        expect(await proxy.metaAGRouter()).to.equal(router);
      });

      it("non-role caller cannot set sidioraFactory", async function () {
        await expect(
          proxy.connect(alice).setSidioraFactory(factoryAddr.address)
        ).to.be.reverted;
      });

      // ── New v2 auth paths actually gate emission ─────────────────
      it("registered EOA cannot forge a pool swap without matching pool identity", async function () {
        // poolAddr was marked registered in the beforeEach setup.
        const poolId = ethers.id("pool/dyn-pool");
        await expect(
          proxy
            .connect(poolAddr)
            .emitSwap(poolId, alice.address, true, 100, 200, 1, 2_000_000n)
        ).to.be.revertedWithCustomError(proxy, "Unauthorized");
      });

      it("dynamic auth path #3 (opticalRegistry.isApproved) gates emission for opticals", async function () {
        await proxy.connect(admin).setOpticalRegistry(await mockOpticalRegistry.getAddress());
        await mockOpticalRegistry.setApproved(opticalAddr.address, true);

        await expect(
          proxy
            .connect(opticalAddr)
            .emitOpticalLifecycle(5, opticalAddr.address, poolAddr.address, ethers.id("Tax"), "0x1234")
        ).to.emit(proxy, "OpticalLifecycle");
      });

      it("dynamic auth path #4 (sender == sidioraFactory) gates emission for the factory", async function () {
        await proxy.connect(admin).setSidioraFactory(factoryAddr.address);
        await expect(
          proxy
            .connect(factoryAddr)
            .emitTokenDeployed(
              alice.address,
              poolAddr.address,
              bob.address,
              ethers.id("salt/1"),
              "TestTok",
              "TST",
              18,
              1_000_000n
            )
        ).to.emit(proxy, "TokenDeployed");
      });

      it("dynamic auth path #5 (registeredTokens) gates emission for pool tokens", async function () {
        await proxy.connect(admin).setSidioraFactory(factoryAddr.address);
        // Factory registers a token (alice acting as a deployed pool token).
        await proxy.connect(factoryAddr).registerToken(alice.address, poolAddr.address);
        expect(await proxy.isRegisteredToken(alice.address)).to.equal(true);

        await expect(
          proxy
            .connect(alice)
            .emitTokenTransfer(alice.address, bob.address, carol.address, 42n)
        ).to.emit(proxy, "TokenTransfer");
      });

      it("unauthorized callers are still rejected post-upgrade", async function () {
        await expect(
          proxy
            .connect(carol)
            .emitSwap(ethers.id("pool/x"), alice.address, true, 1, 1, 0, 0)
        ).to.be.reverted;
      });

      // ── v1 selector stability ─────────────────────────────────────
      it("v1 emit selectors retain their signatures (topic0 stability)", async function () {
        const poolId = ethers.id("pool/topic0-stability");

        await expect(
          proxy
            .connect(authorizedEmitter)
            .emitMarketCreated(poolId, alice.address, bob.address, poolAddr.address, opticalAddr.address)
        )
          .to.emit(proxy, "MarketCreated")
          .withArgs(
            poolId,
            alice.address,
            bob.address,
            poolAddr.address,
            opticalAddr.address,
            (ts) => typeof ts === "bigint",
            (bn) => typeof bn === "bigint"
          );

        await expect(
          proxy
            .connect(authorizedEmitter)
            .emitSwap(poolId, alice.address, true, 100, 200, 1, 2_000_000n)
        ).to.emit(proxy, "Swap");

        await expect(
          proxy
            .connect(authorizedEmitter)
            .emitConfigUpdated(ethers.id("config/key"), 1, 2)
        ).to.emit(proxy, "ConfigUpdated");
      });
    });
  });

  // ════════════════════════════════════════════════════════════════════
  //               Edge: reinitializeV2 with adminWithEmitterRole=0
  // ════════════════════════════════════════════════════════════════════

  describe("reinitializeV2(address(0))", function () {
    it("succeeds without granting any EVENT_EMITTER_ROLE (slot still set to v2)", async function () {
      const newImpl = await EventEmitter.deploy();
      await newImpl.waitForDeployment();

      const initCalldata = EventEmitter.interface.encodeFunctionData("reinitializeV2", [
        ethers.ZeroAddress,
      ]);
      await proxy
        .connect(admin)
        .upgradeToAndCall(await newImpl.getAddress(), initCalldata);

      expect(await proxy.VERSION()).to.equal("2.0.0");
      expect(
        await proxy.hasRole(EVENT_EMITTER_ROLE, ethers.ZeroAddress)
      ).to.equal(false);

      // A fresh second call still reverts (slot 7 was set on first run).
      await expect(
        proxy.connect(admin).reinitializeV2(emitterRoleHolder.address)
      ).to.be.reverted;
    });
  });
});
