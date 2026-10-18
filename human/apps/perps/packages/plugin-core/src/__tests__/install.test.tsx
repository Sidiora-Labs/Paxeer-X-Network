import React from "react";
import { installExtension, setExtensionBuilder } from "../install";
import { PaxeerNetworkPluginRegistry } from "../pluginRegistry";
import { PaxeerNetworkExtensionRegistry } from "../registry";
import { resetExtensionRegistry } from "../setupTests";
import { ExtensionPositionEnum } from "../types";

describe("installExtension", () => {
  beforeEach(() => {
    resetExtensionRegistry();
    PaxeerNetworkPluginRegistry.clear();
  });

  it("registers to PaxeerNetworkPluginRegistry and PaxeerNetworkExtensionRegistry when curried component is called", () => {
    const Component = () => <div>Installed</div>;
    const installer = installExtension({
      name: "TestExt",
      positions: [ExtensionPositionEnum.DepositForm],
    });
    installer(Component);

    const plugins = PaxeerNetworkPluginRegistry.getPlugins();
    expect(plugins).toHaveLength(1);
    expect(plugins[0].id).toBe("TestExt");
    expect(plugins[0].interceptors).toHaveLength(1);
    expect(plugins[0].interceptors![0].target).toBe("Deposit.DepositForm");

    const legacy = PaxeerNetworkExtensionRegistry.getInstance().getPluginsByPosition(
      ExtensionPositionEnum.DepositForm,
    );
    expect(legacy).toBeDefined();
    expect(legacy?.name).toBe("TestExt");
    expect(legacy?.render).toBe(Component);
  });

  it("registers interceptors for all positions", () => {
    const Component = () => null;
    installExtension({
      name: "Multi",
      positions: [
        ExtensionPositionEnum.DepositForm,
        ExtensionPositionEnum.WithdrawForm,
      ],
    })(Component);

    const plugins = PaxeerNetworkPluginRegistry.getPlugins();
    expect(plugins[0].interceptors).toHaveLength(2);
    const targets = plugins[0].interceptors!.map((i) => i.target);
    expect(targets).toContain("Deposit.DepositForm");
    expect(targets).toContain("Deposit.WithdrawForm");
  });
});

describe("setExtensionBuilder", () => {
  beforeEach(() => {
    resetExtensionRegistry();
  });

  it("sets builder on existing extension at position", () => {
    const registry = PaxeerNetworkExtensionRegistry.getInstance();
    registry.register({
      __isInternal: false,
      name: "Ext",
      positions: [ExtensionPositionEnum.AccountMenu],
      render: () => null,
    });
    const builder = jest.fn(() => ({}));
    setExtensionBuilder(ExtensionPositionEnum.AccountMenu, builder);
    const plugin = registry.getPluginsByPosition(
      ExtensionPositionEnum.AccountMenu,
    );
    expect(plugin?.builder).toBe(builder);
  });
});
