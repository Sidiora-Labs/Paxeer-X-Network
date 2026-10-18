const { expect } = require("chai");
const { ethers } = require("hardhat");
const { mine, time } = require("@nomicfoundation/hardhat-toolbox/network-helpers");

describe("GovernanceModule", function () {
  let gov, timelock, sidToken;
  let deployer, alice, bob, charlie;
  const TWO_DAYS = 2 * 24 * 60 * 60;
  const VOTING_PERIOD = 100; // blocks
  const PROPOSAL_THRESHOLD = ethers.parseEther("1000"); // 1000 SID
  const QUORUM_VOTES = ethers.parseEther("5000"); // 5000 SID

  before(async function () {
    [deployer, alice, bob, charlie] = await ethers.getSigners();
  });

  beforeEach(async function () {
    // Deploy mock SID token
    const MockERC20 = await ethers.getContractFactory("MockERC20Base");
    sidToken = await MockERC20.deploy("Sidiora", "SID", 18);
    await sidToken.waitForDeployment();

    // Deploy Timelock
    const Timelock = await ethers.getContractFactory("Timelock");
    timelock = await Timelock.deploy(TWO_DAYS, deployer.address, deployer.address);
    await timelock.waitForDeployment();

    // Deploy GovernanceModule via UUPS proxy
    const Gov = await ethers.getContractFactory("GovernanceModule");
    const impl = await Gov.deploy();
    await impl.waitForDeployment();

    const initData = Gov.interface.encodeFunctionData("initialize", [
      await sidToken.getAddress(),
      await timelock.getAddress(),
      deployer.address,
      PROPOSAL_THRESHOLD,
      VOTING_PERIOD,
      QUORUM_VOTES,
    ]);
    const Proxy = await ethers.getContractFactory("UUPSProxy");
    const proxy = await Proxy.deploy(await impl.getAddress(), initData);
    await proxy.waitForDeployment();

    gov = Gov.attach(await proxy.getAddress());

    // Transfer Timelock proposer to governance
    await timelock.setProposer(await gov.getAddress());

    // Fund users with SID tokens
    await sidToken.mint(alice.address, ethers.parseEther("10000"));
    await sidToken.mint(bob.address, ethers.parseEther("10000"));
    await sidToken.mint(charlie.address, ethers.parseEther("500")); // below threshold
  });

  describe("initialization", function () {
    it("should set correct parameters", async function () {
      expect(await gov.votingToken()).to.equal(await sidToken.getAddress());
      expect(await gov.timelock()).to.equal(await timelock.getAddress());
      expect(await gov.proposalThreshold()).to.equal(PROPOSAL_THRESHOLD);
      expect(await gov.votingPeriod()).to.equal(VOTING_PERIOD);
      expect(await gov.quorumVotes()).to.equal(QUORUM_VOTES);
      expect(await gov.adminModeActive()).to.be.true;
    });

    it("should grant admin roles to deployer", async function () {
      const ADMIN_ROLE = ethers.keccak256(ethers.toUtf8Bytes("ADMIN_ROLE"));
      expect(await gov.hasRole(ADMIN_ROLE, deployer.address)).to.be.true;
      expect(await gov.hasRole(ethers.ZeroHash, deployer.address)).to.be.true;
    });
  });

  describe("propose", function () {
    it("should create proposal with sufficient SID balance", async function () {
      const target = await sidToken.getAddress();
      const data = sidToken.interface.encodeFunctionData("mint", [alice.address, ethers.parseEther("100")]);

      await expect(
        gov.connect(alice).propose([target], [0], [data], "Mint more SID")
      ).to.emit(gov, "ProposalCreated");

      expect(await gov.proposalCount()).to.equal(1);
    });

    it("should revert if below threshold", async function () {
      const target = await sidToken.getAddress();
      const data = sidToken.interface.encodeFunctionData("mint", [charlie.address, ethers.parseEther("100")]);

      await expect(
        gov.connect(charlie).propose([target], [0], [data], "Should fail")
      ).to.be.revertedWithCustomError(gov, "BelowProposalThreshold");
    });

    it("should revert with empty targets", async function () {
      await expect(
        gov.connect(alice).propose([], [], [], "Empty")
      ).to.be.revertedWithCustomError(gov, "InvalidProposal");
    });

    it("should revert with mismatched array lengths", async function () {
      const target = await sidToken.getAddress();
      await expect(
        gov.connect(alice).propose([target], [0, 0], ["0x"], "Mismatch")
      ).to.be.revertedWithCustomError(gov, "ArrayLengthMismatch");
    });
  });

  describe("castVote", function () {
    let proposalId;

    beforeEach(async function () {
      const target = await sidToken.getAddress();
      const data = sidToken.interface.encodeFunctionData("mint", [alice.address, ethers.parseEther("100")]);
      await gov.connect(alice).propose([target], [0], [data], "Test proposal");
      proposalId = 1;
      // Mine a block so voting starts
      await mine(1);
    });

    it("should allow voting for", async function () {
      await expect(
        gov.connect(alice).castVote(proposalId, true)
      ).to.emit(gov, "VoteCast")
        .withArgs(proposalId, alice.address, true, ethers.parseEther("10000"));
    });

    it("should allow voting against", async function () {
      await expect(
        gov.connect(bob).castVote(proposalId, false)
      ).to.emit(gov, "VoteCast")
        .withArgs(proposalId, bob.address, false, ethers.parseEther("10000"));
    });

    it("should revert double vote", async function () {
      await gov.connect(alice).castVote(proposalId, true);
      await expect(
        gov.connect(alice).castVote(proposalId, true)
      ).to.be.revertedWithCustomError(gov, "AlreadyVoted");
    });

    it("should revert vote after period ends", async function () {
      await mine(VOTING_PERIOD + 2);
      await expect(
        gov.connect(alice).castVote(proposalId, true)
      ).to.be.revertedWithCustomError(gov, "VotingClosed");
    });
  });

  describe("execute", function () {
    let proposalId;

    beforeEach(async function () {
      const target = await sidToken.getAddress();
      const data = sidToken.interface.encodeFunctionData("mint", [charlie.address, ethers.parseEther("100")]);
      await gov.connect(alice).propose([target], [0], [data], "Mint for Charlie");
      proposalId = 1;
      await mine(1);
    });

    it("should execute passed proposal (queues to Timelock)", async function () {
      // Alice and Bob vote for (20000 SID > 5000 quorum)
      await gov.connect(alice).castVote(proposalId, true);
      await gov.connect(bob).castVote(proposalId, true);

      // End voting period
      await mine(VOTING_PERIOD + 1);

      const eta = (await time.latest()) + TWO_DAYS + 100;
      await expect(gov.execute(proposalId, eta)).to.emit(gov, "ProposalQueued");
    });

    it("should revert if quorum not met", async function () {
      // Only Charlie votes (500 SID < 5000 quorum) — but Charlie can't propose, so we need Alice with low balance
      // Actually let's just have alice vote for with 10000 > 5000 quorum, that works
      // Test quorum: nobody votes
      await mine(VOTING_PERIOD + 1);
      const eta = (await time.latest()) + TWO_DAYS + 100;
      await expect(gov.execute(proposalId, eta)).to.be.revertedWithCustomError(gov, "QuorumNotMet");
    });

    it("should revert if more against than for", async function () {
      await gov.connect(alice).castVote(proposalId, true);  // 10000 for
      await gov.connect(bob).castVote(proposalId, false);    // 10000 against
      await mine(VOTING_PERIOD + 1);
      const eta = (await time.latest()) + TWO_DAYS + 100;
      await expect(gov.execute(proposalId, eta)).to.be.revertedWithCustomError(gov, "ProposalNotPassed");
    });

    it("should revert if voting not ended", async function () {
      await gov.connect(alice).castVote(proposalId, true);
      const eta = (await time.latest()) + TWO_DAYS + 100;
      await expect(gov.execute(proposalId, eta)).to.be.revertedWithCustomError(gov, "VotingClosed");
    });
  });

  describe("admin mode", function () {
    it("should allow admin to queue directly via adminExecute", async function () {
      const target = await sidToken.getAddress();
      const data = sidToken.interface.encodeFunctionData("mint", [deployer.address, ethers.parseEther("100")]);
      const eta = (await time.latest()) + TWO_DAYS + 100;

      await gov.adminExecute(target, 0, data, eta);
      // Verify it was queued in Timelock
      const txHash = ethers.keccak256(ethers.AbiCoder.defaultAbiCoder().encode(
        ["address", "uint256", "bytes", "uint256"],
        [target, 0, data, eta]
      ));
      expect(await timelock.queuedTransactions(txHash)).to.be.true;
    });

    it("should revert adminExecute from non-admin", async function () {
      const target = await sidToken.getAddress();
      const data = sidToken.interface.encodeFunctionData("mint", [alice.address, ethers.parseEther("100")]);
      const eta = (await time.latest()) + TWO_DAYS + 100;

      await expect(
        gov.connect(alice).adminExecute(target, 0, data, eta)
      ).to.be.revertedWithCustomError(gov, "MissingRole");
    });

    it("should deactivate admin mode permanently", async function () {
      await expect(gov.deactivateAdminMode()).to.emit(gov, "AdminModeDeactivated");
      expect(await gov.adminModeActive()).to.be.false;
    });

    it("should revert adminExecute after deactivation", async function () {
      await gov.deactivateAdminMode();
      const target = await sidToken.getAddress();
      const data = sidToken.interface.encodeFunctionData("mint", [deployer.address, ethers.parseEther("100")]);
      const eta = (await time.latest()) + TWO_DAYS + 100;

      await expect(
        gov.adminExecute(target, 0, data, eta)
      ).to.be.revertedWithCustomError(gov, "AdminModeNotActive");
    });
  });

  describe("cancel", function () {
    let proposalId;

    beforeEach(async function () {
      const target = await sidToken.getAddress();
      const data = sidToken.interface.encodeFunctionData("mint", [alice.address, ethers.parseEther("100")]);
      await gov.connect(alice).propose([target], [0], [data], "Cancel test");
      proposalId = 1;
    });

    it("should allow proposer to cancel", async function () {
      await expect(gov.connect(alice).cancel(proposalId)).to.emit(gov, "ProposalCancelled");
    });

    it("should allow admin to cancel", async function () {
      await expect(gov.cancel(proposalId)).to.emit(gov, "ProposalCancelled");
    });

    it("should revert cancel from unauthorized", async function () {
      await expect(
        gov.connect(bob).cancel(proposalId)
      ).to.be.revertedWithCustomError(gov, "Unauthorized");
    });
  });

  describe("getProposalState", function () {
    it("should return Pending before start block", async function () {
      const target = await sidToken.getAddress();
      const data = sidToken.interface.encodeFunctionData("mint", [alice.address, ethers.parseEther("100")]);
      await gov.connect(alice).propose([target], [0], [data], "State test");
      expect(await gov.getProposalState(1)).to.equal(0); // Pending
    });

    it("should return Active during voting", async function () {
      const target = await sidToken.getAddress();
      const data = sidToken.interface.encodeFunctionData("mint", [alice.address, ethers.parseEther("100")]);
      await gov.connect(alice).propose([target], [0], [data], "State test");
      await mine(1);
      expect(await gov.getProposalState(1)).to.equal(1); // Active
    });

    it("should return Cancelled after cancellation", async function () {
      const target = await sidToken.getAddress();
      const data = sidToken.interface.encodeFunctionData("mint", [alice.address, ethers.parseEther("100")]);
      await gov.connect(alice).propose([target], [0], [data], "State test");
      await gov.connect(alice).cancel(1);
      expect(await gov.getProposalState(1)).to.equal(6); // Cancelled
    });
  });

  describe("UUPS upgrade", function () {
    it("should upgrade by admin", async function () {
      const V2 = await ethers.getContractFactory("GovernanceModule");
      const implV2 = await V2.deploy();
      await gov.upgradeToAndCall(await implV2.getAddress(), "0x");
    });

    it("should revert upgrade by non-admin", async function () {
      const V2 = await ethers.getContractFactory("GovernanceModule");
      const implV2 = await V2.deploy();
      await expect(
        gov.connect(alice).upgradeToAndCall(await implV2.getAddress(), "0x")
      ).to.be.revertedWithCustomError(gov, "MissingRole");
    });
  });

  describe("audit regressions", function () {
    async function passed(targets, values, data) {
      await gov.connect(alice).propose(targets, values, data, "Audit regression");
      await mine(1);
      await gov.connect(alice).castVote(1, true);
      await mine(VOTING_PERIOD + 1);
    }
    it("does not reuse transferred voting balances", async function () {
      await gov.connect(alice).propose([await sidToken.getAddress()], [0], ["0x"], "Snapshot");
      await mine(1);
      await gov.connect(alice).castVote(1, true);
      await sidToken.connect(alice).transfer(charlie.address, ethers.parseEther("10000"));
      await gov.connect(charlie).castVote(1, true);
      const p = await gov.proposals(1);
      expect(p.forVotes).to.equal(ethers.parseEther("10500"));
    });
    it("normalizes arbitrary ETA and reports actual execution", async function () {
      const data = sidToken.interface.encodeFunctionData("mint", [charlie.address, 1]);
      await passed([await sidToken.getAddress()], [0], [data]);
      await gov.execute(1, ethers.MaxUint256);
      expect(await gov.getProposalState(1)).to.equal(4);
      expect(await gov.proposalEta(1)).to.be.lessThan(BigInt(await time.latest()) + BigInt(TWO_DAYS) + 2n);
      await time.increaseTo(await gov.proposalEta(1));
      await gov.executeQueued(1);
      expect(await gov.getProposalState(1)).to.equal(5);
    });
    it("atomically rolls back earlier actions when a later call fails", async function () {
      const target = await sidToken.getAddress();
      const mintData = sidToken.interface.encodeFunctionData("mint", [charlie.address, 1]);
      const failData = sidToken.interface.encodeFunctionData("burn", [charlie.address, ethers.MaxUint256]);
      await passed([target, target], [0, 0], [mintData, failData]);
      await gov.execute(1, 0);
      await time.increaseTo(await gov.proposalEta(1));
      const before = await sidToken.balanceOf(charlie.address);
      await expect(gov.executeQueued(1)).to.be.revertedWithCustomError(timelock, "ExecutionFailed");
      expect(await sidToken.balanceOf(charlie.address)).to.equal(before);
      expect(await gov.getProposalState(1)).to.equal(4);
    });
    it("allows identical voted actions and tracks guardian cancellation", async function () {
      const target = await sidToken.getAddress();
      const data = sidToken.interface.encodeFunctionData("mint", [charlie.address, 1]);
      await passed([target, target], [0, 0], [data, data]);
      await gov.execute(1, 0);
      await timelock.cancelBatch(await gov.proposalOperation(1));
      expect(await gov.getProposalState(1)).to.equal(6);
    });
    it("rejects nonexistent proposal cancellation and queries", async function () {
      await expect(gov.cancel(1)).to.be.revertedWithCustomError(gov, "InvalidProposal");
      await expect(gov.getProposalState(0)).to.be.revertedWithCustomError(gov, "InvalidProposal");
    });
    it("allows a delayed self-call to rotate proposer after handoff", async function () {
      const data = timelock.interface.encodeFunctionData("setProposer", [bob.address]);
      await passed([await timelock.getAddress()], [0], [data]);
      await gov.execute(1, 0);
      await time.increaseTo(await gov.proposalEta(1));
      await gov.executeQueued(1);
      expect(await timelock.proposer()).to.equal(bob.address);
    });
  });

  // Helper to read proposal struct fields
  async function getProposalData(id) {
    return await gov.proposals(id);
  }
});
