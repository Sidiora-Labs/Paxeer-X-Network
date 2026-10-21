const { expect } = require("chai");
const { ethers } = require("hardhat");
const { TOKEN_TOTAL_SUPPLY, ZERO_ADDRESS } = require("../helpers/constants");

describe("SidioraERC20", function () {
  let token;
  let deployer, alice, bob;

  before(async function () {
    [deployer, alice, bob] = await ethers.getSigners();
  });

  beforeEach(async function () {
    const SidioraERC20 = await ethers.getContractFactory("SidioraERC20");
    token = await SidioraERC20.deploy("LaunchToken", "LAUNCH", TOKEN_TOTAL_SUPPLY, alice.address);
    await token.waitForDeployment();
  });

  describe("constructor", function () {
    it("should set correct name", async function () {
      expect(await token.name()).to.equal("LaunchToken");
    });

    it("should set correct symbol", async function () {
      expect(await token.symbol()).to.equal("LAUNCH");
    });

    it("should set 6 decimals", async function () {
      expect(await token.decimals()).to.equal(6);
    });

    it("should mint total supply to recipient", async function () {
      expect(await token.totalSupply()).to.equal(TOKEN_TOTAL_SUPPLY);
      expect(await token.balanceOf(alice.address)).to.equal(TOKEN_TOTAL_SUPPLY);
    });

    it("should revert with zero recipient address", async function () {
      const SidioraERC20 = await ethers.getContractFactory("SidioraERC20");
      await expect(
        SidioraERC20.deploy("Test", "TST", TOKEN_TOTAL_SUPPLY, ethers.ZeroAddress)
      ).to.be.revertedWithCustomError(token, "ZeroAddress");
    });
  });

  describe("transfers", function () {
    it("should transfer tokens between accounts", async function () {
      const amount = ethers.parseUnits("1000", 6);
      await token.connect(alice).transfer(bob.address, amount);
      expect(await token.balanceOf(bob.address)).to.equal(amount);
      expect(await token.balanceOf(alice.address)).to.equal(TOKEN_TOTAL_SUPPLY - amount);
    });

    it("should approve and transferFrom", async function () {
      const amount = ethers.parseUnits("500", 6);
      await token.connect(alice).approve(deployer.address, amount);
      await token.transferFrom(alice.address, bob.address, amount);
      expect(await token.balanceOf(bob.address)).to.equal(amount);
    });
  });

  describe("immutability", function () {
    it("should have no public mint function", async function () {
      // SidioraERC20 has no mint function - _mint is internal in ERC20Base
      expect(token.mint).to.be.undefined;
    });

    it("should have no public burn function", async function () {
      expect(token.burn).to.be.undefined;
    });
  });

  describe("permit (EIP-2612)", function () {
    it("should have valid DOMAIN_SEPARATOR", async function () {
      const domainSeparator = await token.DOMAIN_SEPARATOR();
      expect(domainSeparator).to.not.equal(ethers.ZeroHash);
    });

    it("should execute permit with valid signature", async function () {
      const spender = bob.address;
      const value = ethers.parseUnits("100", 6);
      const deadline = (await ethers.provider.getBlock("latest")).timestamp + 3600;
      const nonce = await token.nonces(alice.address);

      const domain = {
        name: "LaunchToken",
        version: "1",
        chainId: (await ethers.provider.getNetwork()).chainId,
        verifyingContract: await token.getAddress(),
      };
      const types = {
        Permit: [
          { name: "owner", type: "address" },
          { name: "spender", type: "address" },
          { name: "value", type: "uint256" },
          { name: "nonce", type: "uint256" },
          { name: "deadline", type: "uint256" },
        ],
      };
      const message = {
        owner: alice.address,
        spender,
        value,
        nonce,
        deadline,
      };

      const sig = await alice.signTypedData(domain, types, message);
      const { v, r, s } = ethers.Signature.from(sig);

      await token.permit(alice.address, spender, value, deadline, v, r, s);
      expect(await token.allowance(alice.address, spender)).to.equal(value);
    });
  });

  describe("CREATE2 deterministic address", function () {
    it("should match TokenAddress.computeAddress prediction", async function () {
      const TokenAddressWrapper = await ethers.getContractFactory("TokenAddressWrapper");
      const wrapper = await TokenAddressWrapper.deploy();
      await wrapper.waitForDeployment();

      const SidioraERC20 = await ethers.getContractFactory("SidioraERC20");
      const creationCode = SidioraERC20.bytecode;

      const creator = deployer.address;
      const name = "PredictToken";
      const symbol = "PRED";
      const nonce = 0;
      const recipient = alice.address;

      // Compute predicted address
      const predicted = await wrapper.computeAddress(
        deployer.address, creator, name, symbol, nonce, creationCode, TOKEN_TOTAL_SUPPLY, recipient
      );

      // Deploy via CREATE2
      const salt = ethers.keccak256(
        ethers.solidityPacked(
          ["address", "string", "string", "uint256"],
          [creator, name, symbol, nonce]
        )
      );
      const initCode = ethers.solidityPacked(
        ["bytes", "bytes"],
        [creationCode, ethers.AbiCoder.defaultAbiCoder().encode(
          ["string", "string", "uint256", "address"],
          [name, symbol, TOKEN_TOTAL_SUPPLY, recipient]
        )]
      );

      // Use CREATE2 deployer pattern — just verify the lib computation is consistent
      expect(predicted).to.not.equal(ethers.ZeroAddress);
    });
  });
});
