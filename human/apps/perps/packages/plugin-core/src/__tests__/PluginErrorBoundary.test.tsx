import React from "react";
import { render, screen } from "@testing-library/react";
import { PluginErrorBoundary } from "../PluginErrorBoundary";

const Thrower = () => {
  throw new Error("test error");
};

describe("PluginErrorBoundary", () => {
  beforeEach(() => {
    jest.spyOn(console, "error").mockImplementation(() => {});
  });

  afterEach(() => {
    (console.error as jest.Mock).mockRestore();
  });

  it("renders children when no error", () => {
    render(
      <PluginErrorBoundary pluginId="p1">
        <span data-testid="child">ok</span>
      </PluginErrorBoundary>,
    );
    expect(screen.getByTestId("child").textContent).toBe("ok");
  });

  it("renders default fallback when child throws", () => {
    render(
      <PluginErrorBoundary pluginId="my-plugin">
        <Thrower />
      </PluginErrorBoundary>,
    );
    expect(screen.getByText("[Plugin my-plugin error]")).toBeTruthy();
  });

  it("renders custom fallback when fallback prop provided", () => {
    render(
      <PluginErrorBoundary
        pluginId="p1"
        fallback={<div data-testid="custom">Custom</div>}
      >
        <Thrower />
      </PluginErrorBoundary>,
    );
    expect(screen.getByTestId("custom").textContent).toBe("Custom");
  });

  it("calls onError when child throws", () => {
    const onError = jest.fn();
    render(
      <PluginErrorBoundary pluginId="p1" onError={onError}>
        <Thrower />
      </PluginErrorBoundary>,
    );
    expect(onError).toHaveBeenCalledWith(expect.any(Error));
    expect(onError.mock.calls[0][0].message).toBe("test error");
  });

  it("renders onFallback() when child throws and onFallback provided", () => {
    const onFallback = () => (
      <div data-testid="plugin-fallback">Plugin fallback</div>
    );
    render(
      <PluginErrorBoundary pluginId="p1" onFallback={onFallback}>
        <Thrower />
      </PluginErrorBoundary>,
    );
    expect(screen.getByTestId("plugin-fallback").textContent).toBe(
      "Plugin fallback",
    );
  });
});
