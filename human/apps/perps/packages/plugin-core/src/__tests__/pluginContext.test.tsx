import React from "react";
import { render, screen } from "@testing-library/react";
import {
  PaxeerNetworkPluginProvider,
  usePaxeerNetworkPluginContext,
  ExtensionProvider,
  useExtensionContext,
} from "../pluginContext";
import { PaxeerNetworkPluginRegistry } from "../pluginRegistry";
import type { PaxeerNetworkPlugin, PluginRegistrationFn } from "../types";

/** Consumer to read context in tests */
const ContextConsumer = () => {
  const { plugins, apiFacade } = usePaxeerNetworkPluginContext();
  return (
    <div>
      <span data-testid="plugin-count">{plugins.length}</span>
      <span data-testid="plugin-ids">{plugins.map((p) => p.id).join(",")}</span>
      <span data-testid="has-api">{String(!!apiFacade)}</span>
    </div>
  );
};

describe("PaxeerNetworkPluginProvider", () => {
  afterEach(() => {
    PaxeerNetworkPluginRegistry.clear();
  });

  it("provides empty plugins and apiFacade when plugins=[]", () => {
    render(
      <PaxeerNetworkPluginProvider plugins={[]}>
        <ContextConsumer />
      </PaxeerNetworkPluginProvider>,
    );
    expect(screen.getByTestId("plugin-count").textContent).toBe("0");
    expect(screen.getByTestId("plugin-ids").textContent).toBe("");
    expect(screen.getByTestId("has-api").textContent).toBe("true");
  });

  it("merges descriptor plugins from props", () => {
    const plugins: PaxeerNetworkPlugin[] = [
      { id: "p1", name: "P1" },
      { id: "p2", name: "P2" },
    ];
    render(
      <PaxeerNetworkPluginProvider plugins={plugins}>
        <ContextConsumer />
      </PaxeerNetworkPluginProvider>,
    );
    expect(screen.getByTestId("plugin-count").textContent).toBe("2");
    expect(screen.getByTestId("plugin-ids").textContent).toBe("p1,p2");
  });

  it("invokes registration fn with SDK and pluginState and merges registered plugins", () => {
    const registerFn: PluginRegistrationFn<{ foo: string }> = (sdk, state) => {
      expect(state).toEqual({ foo: "bar" });
      sdk.registerPlugin({ id: "from-fn", name: "FromFn" });
    };
    render(
      <PaxeerNetworkPluginProvider
        plugins={[registerFn]}
        pluginState={{ foo: "bar" }}
      >
        <ContextConsumer />
      </PaxeerNetworkPluginProvider>,
    );
    expect(screen.getByTestId("plugin-ids").textContent).toBe("from-fn");
  });

  it("calls setup on each plugin that has setup", () => {
    const setup = jest.fn();
    const plugins: PaxeerNetworkPlugin[] = [
      { id: "with-setup", name: "WithSetup", setup },
    ];
    render(
      <PaxeerNetworkPluginProvider plugins={plugins}>
        <ContextConsumer />
      </PaxeerNetworkPluginProvider>,
    );
    expect(setup).toHaveBeenCalledTimes(1);
    expect(setup).toHaveBeenCalledWith(expect.any(Object));
  });

  it("merges plugins from PaxeerNetworkPluginRegistry with props plugins", () => {
    PaxeerNetworkPluginRegistry.register({
      id: "from-registry",
      name: "FromRegistry",
    });
    render(
      <PaxeerNetworkPluginProvider
        plugins={[{ id: "from-props", name: "FromProps" }]}
      >
        <ContextConsumer />
      </PaxeerNetworkPluginProvider>,
    );
    expect(screen.getByTestId("plugin-count").textContent).toBe("2");
    expect(screen.getByTestId("plugin-ids").textContent).toContain(
      "from-props",
    );
    expect(screen.getByTestId("plugin-ids").textContent).toContain(
      "from-registry",
    );
  });
});

describe("usePaxeerNetworkPluginContext", () => {
  it("returns default (empty plugins, empty api) when outside provider", () => {
    render(<ContextConsumer />);
    expect(screen.getByTestId("plugin-count").textContent).toBe("0");
    expect(screen.getByTestId("has-api").textContent).toBe("true");
  });
});

describe("ExtensionProvider and useExtensionContext", () => {
  const ExtensionConsumer = () => {
    const ctx = useExtensionContext();
    return <span data-testid="ext-ctx">{JSON.stringify(ctx)}</span>;
  };

  it("provides empty object and child can read it", () => {
    render(
      <ExtensionProvider>
        <ExtensionConsumer />
      </ExtensionProvider>,
    );
    expect(screen.getByTestId("ext-ctx").textContent).toBe("{}");
  });
});
