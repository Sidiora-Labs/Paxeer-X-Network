const { expect } = require("chai");
const { ethers } = require("hardhat");

describe("ERC20Base", function () {
  let token, deployer, alice, bob;

  before(async function () {
    [deployer, alice, bob] = await ethers.getSigners();
  });

  beforeEach(async function () {
    const Mock = await ethers.getContractFactory("MockERC20Base");
    token = await Mock.deploy("Test Token", "TEST", 18);
    await token.waitForDeployment();
  });

  describe("metadata", function () {
    it("should have correct name, symbol, decimals", async function () {
      expect(await token.name()).to.equal("Test Token");
      expect(await token.symbol()).to.equal("TEST");
      expect(await token.decimals()).to.equal(18);
    });

    it("should start with zero total supply", async function () {
      expect(await token.totalSupply()).to.equal(0);
    });
  });

  describe("mint", function () {
    it("should mint tokens", async function () {
      await token.mint(alice.address, ethers.parseEther("1000"));
      expect(await token.balanceOf(alice.address)).to.equal(ethers.parseEther("1000"));
      expect(await token.totalSupply()).to.equal(ethers.parseEther("1000"));
    });

    it("should emit Transfer event from zero address", async function () {
      await expect(token.mint(alice.address, ethers.parseEther("100")))
        .to.emit(token, "Transfer")
        .withArgs(ethers.ZeroAddress, alice.address, ethers.parseEther("100"));
    });

    it("should revert mint to zero address", async function () {
      await expect(
        token.mint(ethers.ZeroAddress, ethers.parseEther("100"))
      ).to.be.revertedWithCustomError(token, "ZeroAddress");
    });
  });

  describe("burn", function () {
    it("should burn tokens", async function () {
      await token.mint(alice.address, ethers.parseEther("1000"));
      await token.burn(alice.address, ethers.parseEther("300"));
      expect(await token.balanceOf(alice.address)).to.equal(ethers.parseEther("700"));
      expect(await token.totalSupply()).to.equal(ethers.parseEther("700"));
    });

    it("should revert burn exceeding balance", async function () {
      await token.mint(alice.address, ethers.parseEther("100"));
      await expect(
        token.burn(alice.address, ethers.parseEther("200"))
      ).to.be.revertedWithCustomError(token, "InsufficientBalance");
    });
  });

  describe("transfer", function () {
    beforeEach(async function () {
      await token.mint(deployer.address, ethers.parseEther("1000"));
    });

    it("should transfer tokens", async function () {
      await token.transfer(alice.address, ethers.parseEther("100"));
      expect(await token.balanceOf(alice.address)).to.equal(ethers.parseEther("100"));
      expect(await token.balanceOf(deployer.address)).to.equal(ethers.parseEther("900"));
    });

    it("should emit Transfer event", async function () {
      await expect(token.transfer(alice.address, ethers.parseEther("100")))
        .to.emit(token, "Transfer")
        .withArgs(deployer.address, alice.address, ethers.parseEther("100"));
    });

    it("should revert transfer exceeding balance", async function () {
      await expect(
        token.transfer(alice.address, ethers.parseEther("2000"))
      ).to.be.revertedWithCustomError(token, "InsufficientBalance");
    });

    it("should revert transfer to zero address", async function () {
      await expect(
        token.transfer(ethers.ZeroAddress, ethers.parseEther("100"))
      ).to.be.revertedWithCustomError(token, "ZeroAddress");
    });
  });

  describe("approve and transferFrom", function () {
    beforeEach(async function () {
      await token.mint(deployer.address, ethers.parseEther("1000"));
    });

    it("should approve spender", async function () {
      await token.approve(alice.address, ethers.parseEther("500"));
      expect(await token.allowance(deployer.address, alice.address)).to.equal(
        ethers.parseEther("500")
      );
    });

    it("should emit Approval event", async function () {
      await expect(token.approve(alice.address, ethers.parseEther("500")))
        .to.emit(token, "Approval")
        .withArgs(deployer.address, alice.address, ethers.parseEther("500"));
    });

    it("should transferFrom with allowance", async function () {
      await token.approve(alice.address, ethers.parseEther("500"));
      await token.connect(alice).transferFrom(deployer.address, bob.address, ethers.parseEther("200"));
      expect(await token.balanceOf(bob.address)).to.equal(ethers.parseEther("200"));
      expect(await token.allowance(deployer.address, alice.address)).to.equal(
        ethers.parseEther("300")
      );
    });

    it("should revert transferFrom exceeding allowance", async function () {
      await token.approve(alice.address, ethers.parseEther("100"));
      await expect(
        token.connect(alice).transferFrom(deployer.address, bob.address, ethers.parseEther("200"))
      ).to.be.revertedWithCustomError(token, "InsufficientAllowance");
    });

    it("should not decrease max uint256 allowance", async function () {
      await token.approve(alice.address, ethers.MaxUint256);
      await token.connect(alice).transferFrom(deployer.address, bob.address, ethers.parseEther("100"));
      expect(await token.allowance(deployer.address, alice.address)).to.equal(ethers.MaxUint256);
    });
  });

  describe("permit (EIP-2612)", function () {
    it("should have correct DOMAIN_SEPARATOR", async function () {
      const domain = await token.DOMAIN_SEPARATOR();
      expect(domain).to.not.equal(ethers.ZeroHash);
    });

    it("should permit via valid signature", async function () {
      const owner = deployer;
      const spender = alice.address;
      const value = ethers.parseEther("100");
      const nonce = await token.nonces(owner.address);
      const deadline = ethers.MaxUint256;

      const domain = {
        name: "Test Token",
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

      const message = { owner: owner.address, spender, value, nonce, deadline };
      const sig = await owner.signTypedData(domain, types, message);
      const { v, r, s } = ethers.Signature.from(sig);

      await token.permit(owner.address, spender, value, deadline, v, r, s);
      expect(await token.allowance(owner.address, spender)).to.equal(value);
      expect(await token.nonces(owner.address)).to.equal(1);
    });

    it("should revert on expired deadline", async function () {
      const owner = deployer;
      const spender = alice.address;
      const value = ethers.parseEther("100");
      const nonce = await token.nonces(owner.address);
      const deadline = 0n; // expired

      const domain = {
        name: "Test Token",
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

      const message = { owner: owner.address, spender, value, nonce, deadline };
      const sig = await owner.signTypedData(domain, types, message);
      const { v, r, s } = ethers.Signature.from(sig);

      await expect(
        token.permit(owner.address, spender, value, deadline, v, r, s)
      ).to.be.revertedWithCustomError(token, "PermitExpired");
    });

    it("should revert on invalid signature", async function () {
      await expect(
        token.permit(
          deployer.address, alice.address, ethers.parseEther("100"),
          ethers.MaxUint256, 27,
          ethers.ZeroHash, ethers.ZeroHash
        )
      ).to.be.revertedWithCustomError(token, "InvalidPermit");
    });
  });
});
