import React, { useEffect, useState } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { PaxeerNetworkPluginProvider } from "../pluginContext";
import { PaxeerNetworkPluginRegistry } from "../pluginRegistry";
import { usePluginScope } from "../pluginScopeContext";
import type { PluginRegistrationFn } from "../types";
import { useInjectedComponent } from "../useInjectedComponent";

const DefaultWidget = (props: { label?: string }) => (
  <div data-testid="default">{props.label ?? "Default"}</div>
);

/** Wrapper that uses useInjectedComponent and renders the result */
const SlotRenderer = ({
  path,
  defaultComponent,
}: {
  path: string;
  defaultComponent: React.ComponentType<{ label?: string }>;
}) => {
  const Injected = useInjectedComponent(path, defaultComponent);
  return <Injected label="slot" />;
};

describe("useInjectedComponent", () => {
  afterEach(() => {
    PaxeerNetworkPluginRegistry.clear();
  });

  it("returns DefaultComponent when no interceptors for path", () => {
    render(
      <PaxeerNetworkPluginProvider plugins={[]}>
        <SlotRenderer
          path="Deposit.DepositForm"
          defaultComponent={DefaultWidget}
        />
      </PaxeerNetworkPluginProvider>,
    );
    expect(screen.getByTestId("default")).toBeTruthy();
    expect(screen.getByTestId("default").textContent).toBe("slot");
  });

  it("renders interceptor output when plugin registers for path", () => {
    const InterceptorComponent = (
      Original: React.ComponentType<any>,
      props: any,
    ) => (
      <div data-testid="interceptor">
        <span>intercepted</span>
        <Original {...props} />
      </div>
    );
    const plugins = [
      {
        id: "test-plugin",
        name: "Test",
        interceptors: [
          {
            target: "Deposit.DepositForm",
            component: InterceptorComponent,
          },
        ],
      },
    ];
    render(
      <PaxeerNetworkPluginProvider plugins={plugins}>
        <SlotRenderer
          path="Deposit.DepositForm"
          defaultComponent={DefaultWidget}
        />
      </PaxeerNetworkPluginProvider>,
    );
    expect(screen.getByTestId("interceptor")).toBeTruthy();
    expect(screen.getByText("intercepted")).toBeTruthy();
    expect(screen.getByTestId("default")).toBeTruthy();
  });

  it("ignores interceptors for other paths", () => {
    const plugins = [
      {
        id: "other",
        name: "Other",
        interceptors: [
          {
            target: "Other.Path",
            component: () => <div data-testid="other">other</div>,
          },
        ],
      },
    ];
    render(
      <PaxeerNetworkPluginProvider plugins={plugins}>
        <SlotRenderer
          path="Deposit.DepositForm"
          defaultComponent={DefaultWidget}
        />
      </PaxeerNetworkPluginProvider>,
    );
    expect(screen.getByTestId("default")).toBeTruthy();
    expect(screen.queryByTestId("other")).toBeNull();
  });

  it("keeps the default component mounted when pluginState changes", () => {
    const mounted = jest.fn();
    const unmounted = jest.fn();
    const StatefulDefault = (props: { label?: string }) => {
      useEffect(() => {
        mounted();
        return unmounted;
      }, []);

      return <div data-testid="stateful-default">{props.label}</div>;
    };
    const registerPlugin: PluginRegistrationFn = (sdk, state) => {
      const networkId = state?.networkId;
      sdk.registerPlugin({
        id: "state-aware-plugin",
        interceptors: [
          {
            target: "TradingView.Desktop",
            component: (Original, props) => (
              <div data-testid="plugin-state">
                <span>{networkId}</span>
                <Original {...props} />
              </div>
            ),
          },
        ],
      });
    };
    const pluginState = (networkId: "mainnet" | "testnet") =>
      ({
        config: { brokerName: "paxeer-network" },
        networkId,
      }) as const;

    const { rerender, unmount } = render(
      <PaxeerNetworkPluginProvider
        plugins={[registerPlugin]}
        pluginState={pluginState("testnet")}
      >
        <SlotRenderer
          path="TradingView.Desktop"
          defaultComponent={StatefulDefault}
        />
      </PaxeerNetworkPluginProvider>,
    );

    expect(screen.getByTestId("plugin-state").textContent).toContain("testnet");
    expect(mounted).toHaveBeenCalledTimes(1);
    expect(unmounted).not.toHaveBeenCalled();

    rerender(
      <PaxeerNetworkPluginProvider
        plugins={[registerPlugin]}
        pluginState={pluginState("mainnet")}
      >
        <SlotRenderer
          path="TradingView.Desktop"
          defaultComponent={StatefulDefault}
        />
      </PaxeerNetworkPluginProvider>,
    );

    expect(screen.getByTestId("plugin-state").textContent).toContain("mainnet");
    expect(mounted).toHaveBeenCalledTimes(1);
    expect(unmounted).not.toHaveBeenCalled();

    unmount();
    expect(unmounted).toHaveBeenCalledTimes(1);
  });

  it("preserves interceptor hook state across plugin context updates", () => {
    const registerPlugin: PluginRegistrationFn = (sdk, state) => {
      const networkId = state?.networkId;
      sdk.registerPlugin({
        id: "hook-plugin",
        interceptors: [
          {
            target: "TradingView.DisplayControl.DesktopMenuList",
            component: function HookInterceptor(Original, props) {
              const [count, setCount] = useState(0);
              return (
                <div>
                  <button onClick={() => setCount((value) => value + 1)}>
                    count:{count}
                  </button>
                  <span data-testid="hook-plugin-network">{networkId}</span>
                  <Original {...props} />
                </div>
              );
            },
          },
        ],
      });
    };
    const pluginState = (networkId: "mainnet" | "testnet") =>
      ({
        config: { brokerName: "paxeer-network" },
        networkId,
      }) as const;

    const { rerender } = render(
      <PaxeerNetworkPluginProvider
        plugins={[registerPlugin]}
        pluginState={pluginState("testnet")}
      >
        <SlotRenderer
          path="TradingView.DisplayControl.DesktopMenuList"
          defaultComponent={DefaultWidget}
        />
      </PaxeerNetworkPluginProvider>,
    );

    fireEvent.click(screen.getByRole("button", { name: "count:0" }));
    expect(screen.getByRole("button", { name: "count:1" })).toBeTruthy();

    rerender(
      <PaxeerNetworkPluginProvider
        plugins={[registerPlugin]}
        pluginState={pluginState("mainnet")}
      >
        <SlotRenderer
          path="TradingView.DisplayControl.DesktopMenuList"
          defaultComponent={DefaultWidget}
        />
      </PaxeerNetworkPluginProvider>,
    );

    expect(screen.getByRole("button", { name: "count:1" })).toBeTruthy();
    expect(screen.getByTestId("hook-plugin-network").textContent).toBe(
      "mainnet",
    );
  });

  it("keeps onion order and plugin scope for multiple interceptors", () => {
    const createScopedInterceptor = (testId: string) =>
      function ScopedInterceptor(
        Original: React.ComponentType<any>,
        props: any,
      ) {
        const scope = usePluginScope();
        return (
          <div data-testid={testId} data-plugin-id={scope?.pluginId}>
            <Original {...props} />
          </div>
        );
      };
    const plugins = [
      {
        id: "inner-plugin",
        interceptors: [
          {
            target: "Deposit.DepositForm",
            component: createScopedInterceptor("inner-interceptor"),
          },
        ],
      },
      {
        id: "outer-plugin",
        interceptors: [
          {
            target: "Deposit.DepositForm",
            component: createScopedInterceptor("outer-interceptor"),
          },
        ],
      },
    ];

    render(
      <PaxeerNetworkPluginProvider plugins={plugins}>
        <SlotRenderer
          path="Deposit.DepositForm"
          defaultComponent={DefaultWidget}
        />
      </PaxeerNetworkPluginProvider>,
    );

    const outer = screen.getByTestId("outer-interceptor");
    const inner = screen.getByTestId("inner-interceptor");
    expect(outer.getAttribute("data-plugin-id")).toBe("outer-plugin");
    expect(inner.getAttribute("data-plugin-id")).toBe("inner-plugin");
    expect(outer.contains(inner)).toBe(true);
    expect(inner.contains(screen.getByTestId("default"))).toBe(true);
  });
});
