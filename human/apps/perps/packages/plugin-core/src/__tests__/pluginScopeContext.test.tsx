import React from "react";
import { render, screen } from "@testing-library/react";
import { PluginScopeProvider, usePluginScope } from "../pluginScopeContext";

const ScopeConsumer = () => {
  const scope = usePluginScope();
  return (
    <span data-testid="scope">
      {scope
        ? `${scope.pluginId}|${scope.pluginName ?? ""}|${scope.pluginVersion ?? ""}`
        : "null"}
    </span>
  );
};

describe("PluginScopeProvider", () => {
  it("provides pluginId, pluginName, pluginVersion to children", () => {
    render(
      <PluginScopeProvider
        pluginId="my-plugin"
        pluginName="My Plugin"
        pluginVersion="1.0.0"
      >
        <ScopeConsumer />
      </PluginScopeProvider>,
    );
    expect(screen.getByTestId("scope").textContent).toBe(
      "my-plugin|My Plugin|1.0.0",
    );
  });

  it("works with only pluginId", () => {
    render(
      <PluginScopeProvider pluginId="minimal">
        <ScopeConsumer />
      </PluginScopeProvider>,
    );
    expect(screen.getByTestId("scope").textContent).toBe("minimal||");
  });
});

describe("usePluginScope", () => {
  it("returns null when outside PluginScopeProvider", () => {
    render(<ScopeConsumer />);
    expect(screen.getByTestId("scope").textContent).toBe("null");
  });
});
