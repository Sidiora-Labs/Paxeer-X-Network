const { expect } = require("chai");
const { ethers } = require("hardhat");

describe("Proxy + ERC1967Utils", function () {
  let deployer;

  before(async function () {
    [deployer] = await ethers.getSigners();
  });

  describe("ERC1967Utils", function () {
    it("should read/write implementation slot", async function () {
      const Wrapper = await ethers.getContractFactory("ERC1967UtilsWrapper");
      const wrapper = await Wrapper.deploy();

      // Deploy a dummy impl so it has code
      const Impl = await ethers.getContractFactory("MockImplementation");
      const impl = await Impl.deploy();

      await wrapper.setImplementation(await impl.getAddress());
      expect(await wrapper.getImplementation()).to.equal(await impl.getAddress());
    });

    it("should emit Upgraded event on setImplementation", async function () {
      const Wrapper = await ethers.getContractFactory("ERC1967UtilsWrapper");
      const wrapper = await Wrapper.deploy();
      const Impl = await ethers.getContractFactory("MockImplementation");
      const impl = await Impl.deploy();

      await expect(wrapper.setImplementation(await impl.getAddress()))
        .to.emit(wrapper, "Upgraded")
        .withArgs(await impl.getAddress());
    });

    it("should revert setImplementation with non-contract address", async function () {
      const Wrapper = await ethers.getContractFactory("ERC1967UtilsWrapper");
      const wrapper = await Wrapper.deploy();

      await expect(
        wrapper.setImplementation(deployer.address)
      ).to.be.revertedWithCustomError(wrapper, "InvalidImplementation");
    });

    it("should read/write admin slot", async function () {
      const Wrapper = await ethers.getContractFactory("ERC1967UtilsWrapper");
      const wrapper = await Wrapper.deploy();

      await wrapper.setAdmin(deployer.address);
      expect(await wrapper.getAdmin()).to.equal(deployer.address);
    });
  });

  describe("Proxy delegatecall", function () {
    it("should forward calls to implementation", async function () {
      const Impl = await ethers.getContractFactory("MockImplementation");
      const impl = await Impl.deploy();

      const Proxy = await ethers.getContractFactory("MockERC1967Proxy");
      const proxy = await Proxy.deploy(await impl.getAddress(), "0x");

      // Interact with proxy as if it were the implementation
      const proxied = Impl.attach(await proxy.getAddress());
      await proxied.setValue(42);
      expect(await proxied.getValue()).to.equal(42);
    });

    it("should use proxy's storage, not implementation's", async function () {
      const Impl = await ethers.getContractFactory("MockImplementation");
      const impl = await Impl.deploy();

      const Proxy = await ethers.getContractFactory("MockERC1967Proxy");
      const proxy = await Proxy.deploy(await impl.getAddress(), "0x");

      const proxied = Impl.attach(await proxy.getAddress());
      await proxied.setValue(99);

      // Implementation's storage should be untouched
      expect(await impl.getValue()).to.equal(0);
      // Proxy's storage has the value
      expect(await proxied.getValue()).to.equal(99);
    });

    it("should forward initialization data", async function () {
      const Impl = await ethers.getContractFactory("MockImplementation");
      const impl = await Impl.deploy();

      const initData = impl.interface.encodeFunctionData("setValue", [123]);
      const Proxy = await ethers.getContractFactory("MockERC1967Proxy");
      const proxy = await Proxy.deploy(await impl.getAddress(), initData);

      const proxied = Impl.attach(await proxy.getAddress());
      expect(await proxied.getValue()).to.equal(123);
    });

    it("should preserve storage across different interactions", async function () {
      const Impl = await ethers.getContractFactory("MockImplementation");
      const impl = await Impl.deploy();

      const Proxy = await ethers.getContractFactory("MockERC1967Proxy");
      const proxy = await Proxy.deploy(await impl.getAddress(), "0x");
      const proxied = Impl.attach(await proxy.getAddress());

      await proxied.setValue(1);
      expect(await proxied.getValue()).to.equal(1);
      await proxied.setValue(2);
      expect(await proxied.getValue()).to.equal(2);
    });
  });
});
